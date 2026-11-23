package app.unison.ui.state

import androidx.annotation.StringRes
import app.unison.R
import app.unison.clipboard.HeldItems
import app.unison.clipboard.PauseRequest
import app.unison.core.CoreApi
import app.unison.core.CoreException
import app.unison.core.qrCodeAddresses
import app.unison.core.withoutOwnService
import app.unison.history.HistorySaver
import app.unison.pairing.PairingLink
import app.unison.service.IncomingFiles
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.channels.BufferOverflow
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import unison.local.v1.Local
import unison.mobile.v1.Mobile
import kotlin.coroutines.cancellation.CancellationException

/** State of the screens over the core and the changes they request. */
class AppState(
    private val core: CoreApi,
    events: Flow<Mobile.Event>,
    private val saver: HistorySaver,
    private val scope: CoroutineScope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate),
    private val localAddresses: () -> List<String> = { emptyList() },
    private val held: HeldItems = HeldItems.shared,
) {
    private val mutableState = MutableStateFlow(CoreState())

    /** What the devices screen renders. */
    val state: StateFlow<CoreState> = mutableState.asStateFlow()

    private val mutableHistory = MutableStateFlow(HistoryState())

    /** What the history screen renders. */
    val history: StateFlow<HistoryState> = mutableHistory.asStateFlow()

    private val mutablePairing = MutableStateFlow(PairingState())

    /** What the add screen renders. */
    val pairing: StateFlow<PairingState> = mutablePairing.asStateFlow()

    private val mutableRequests = MutableStateFlow(RequestsState())

    /** What the requests screen renders. */
    val requests: StateFlow<RequestsState> = mutableRequests.asStateFlow()

    private val mutableNotices =
        MutableSharedFlow<Int>(
            extraBufferCapacity = NOTICE_BUFFER,
            onBufferOverflow = BufferOverflow.DROP_OLDEST,
        )

    /** One-off messages for the snackbar, as string resources. */
    val notices: SharedFlow<Int> = mutableNotices.asSharedFlow()

    /** Held clipboard items by ID, in the order they arrived. */
    private val heldPrompts = MutableStateFlow<Map<String, PendingPrompt>>(emptyMap())

    /**
     * Questions that wait for the user, oldest first: the held clipboard items
     * and then the file offers. An item applied or an offer answered anywhere
     * leaves the list.
     */
    val prompts: StateFlow<List<PendingPrompt>> =
        combine(heldPrompts, held.ids, mutableRequests) { clips, ids, offers ->
            clips.values.filter { it.id in ids } +
                offers.requests.asReversed().map(PendingPrompt::files)
        }.stateIn(scope, SharingStarted.Eagerly, emptyList())

    private var load: Job? = null
    private var loadHistory: Job? = null
    private var loadRequests: Job? = null
    private var pollCandidates: Job? = null
    private var closeWindow: Job? = null
    private var loadingCandidates = false

    /** False while no screen shows the state; the owner reloads it when one does again. */
    private var visible = true

    init {
        scope.launch {
            events.collect { event ->
                // A hidden screen reads the core again when it comes back, so reloading
                // here would only spend the battery on content nobody sees.
                if (visible && event.changesDevices()) {
                    refresh()
                }
                if (visible && event.changesHistory()) {
                    refreshHistory()
                }
                if (visible && event.changesRequests()) {
                    refreshRequests()
                }
                if (event.isHeld()) {
                    rememberHeld(event.event)
                }
                onPeerCompatibilityEvent(event)
                onPairingEvent(event)
            }
        }
    }

    /**
     * Tells whether a screen shows the state. While hidden, events do not reload
     * the devices, the history and the offers; the owner reloads them on show.
     */
    fun setVisible(visible: Boolean) {
        this.visible = visible
    }

    /** Reloads the status and the settings of the core. */
    fun refresh() {
        load?.cancel()
        load =
            scope.launch {
                mutableState.update { it.copy(loading = it.status == null, failure = null) }
                try {
                    val status = core.status()
                    val settings = loadSettings()
                    val relay = loadRelay()
                    mutableState.update {
                        it.copy(
                            loading = false,
                            status = status,
                            settings = settings ?: it.settings,
                            relay = relay ?: it.relay,
                            failure = null,
                        )
                    }
                } catch (e: CancellationException) {
                    throw e
                } catch (e: Exception) {
                    mutableState.update { it.copy(loading = false, failure = failureOf(e)) }
                }
            }
    }

    /** Reloads the history with the current filters. */
    fun refreshHistory() {
        loadHistory?.cancel()
        loadHistory =
            scope.launch {
                mutableHistory.update { it.copy(loading = true, failure = null) }
                try {
                    val filter = mutableHistory.value
                    val response = core.listHistory(filter.kind, filter.deviceId)
                    mutableHistory.update {
                        it.copy(loading = false, loaded = true, items = response.itemsList, failure = null)
                    }
                } catch (e: CancellationException) {
                    throw e
                } catch (e: Exception) {
                    mutableHistory.update { it.copy(loading = false, failure = failureOf(e)) }
                }
            }
    }

    /** Shows entries of one kind only, KIND_UNSPECIFIED shows every kind. */
    fun setHistoryKind(kind: Local.Kind) {
        if (mutableHistory.value.kind == kind) {
            return
        }
        mutableHistory.update { it.copy(kind = kind) }
        refreshHistory()
    }

    /** Shows entries of one device only, an empty ID shows every device. */
    fun setHistoryDevice(deviceId: String) {
        if (mutableHistory.value.deviceId == deviceId) {
            return
        }
        mutableHistory.update { it.copy(deviceId = deviceId) }
        refreshHistory()
    }

    /** Writes an entry back to the clipboard. */
    fun copyHistoryItem(itemId: String) =
        runHistoryAction(
            itemId = itemId,
            failure = R.string.history_copy_failed,
        ) {
            core.copyHistoryItem(itemId)
            mutableNotices.tryEmit(R.string.history_copied)
            refreshHistory()
        }

    /** Saves the payloads of an entry into the Downloads collection. */
    fun downloadHistoryItem(itemId: String) =
        runHistoryAction(
            itemId = itemId,
            failure = R.string.history_download_failed,
        ) {
            if (saver.save(itemId) <= 0) {
                throw IllegalStateException("nothing was saved")
            }
            mutableNotices.tryEmit(R.string.history_downloaded)
            refreshHistory()
        }

    /** Deletes an entry on every device. */
    fun deleteHistoryItem(itemId: String) =
        runHistoryAction(
            itemId = itemId,
            failure = R.string.history_delete_failed,
        ) {
            core.deleteHistoryItem(itemId)
            mutableNotices.tryEmit(R.string.history_deleted)
            refreshHistory()
        }

    /** Deletes every entry on every device. */
    fun clearHistory() {
        if (mutableHistory.value.clearing) {
            return
        }
        mutableHistory.update { it.copy(clearing = true) }
        scope.launch {
            try {
                core.clearHistory()
                held.ids.value.forEach(held::remove)
                heldPrompts.update { emptyMap() }
                mutableNotices.tryEmit(R.string.history_cleared)
                refreshHistory()
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                mutableNotices.tryEmit(R.string.history_clear_failed)
            } finally {
                mutableHistory.update { it.copy(clearing = false) }
            }
        }
    }

    /** Writes an entry that waited for a confirmation to the clipboard. */
    fun applyHistoryItem(itemId: String) =
        runHistoryAction(
            itemId = itemId,
            failure = R.string.history_apply_failed,
        ) {
            core.applyHistoryItem(itemId)
            held.remove(itemId)
            mutableNotices.tryEmit(R.string.history_applied)
            refreshHistory()
        }

    /** Changes the pause of the clipboard synchronization. */
    fun setPause(request: PauseRequest) {
        scope.launch {
            try {
                core.setPause(request.mode, request.untilUnixMs)
                refresh()
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                mutableNotices.tryEmit(R.string.pause_failed)
            }
        }
    }

    /** Reloads the file offers that wait for an answer. */
    fun refreshRequests() {
        loadRequests?.cancel()
        loadRequests = scope.launch { loadRequestsBlocking() }
    }

    /** Accepts or declines one offer of a Friends device. */
    fun decideRequest(
        id: String,
        accept: Boolean,
    ) {
        if (id.isEmpty() || id in mutableRequests.value.answering) {
            return
        }
        mutableRequests.update { it.copy(answering = it.answering + id) }
        scope.launch {
            try {
                core.decideRequest(id, accept)
                mutableRequests.update {
                    it.copy(
                        requests = it.requests.filter { request -> request.id != id },
                        loaded = true,
                    )
                }
                mutableNotices.tryEmit(
                    if (accept) R.string.requests_accepted else R.string.requests_declined,
                )
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                mutableNotices.tryEmit(R.string.requests_answer_failed)
                loadRequestsBlocking()
            } finally {
                mutableRequests.update { it.copy(answering = it.answering - id) }
            }
        }
    }

    /**
     * Changes one confirmation flag of a paired device: whether this device asks
     * before it takes what the device sends ([Local.ConfirmationSide.CONFIRMATION_SIDE_LOCAL]),
     * or whether the device asks before it takes what this device sends.
     */
    fun setDeviceConfirmation(
        deviceId: String,
        scope: Local.ConfirmationScope,
        side: Local.ConfirmationSide,
        require: Boolean,
    ) {
        if (deviceId.isEmpty()) {
            return
        }
        this.scope.launch {
            try {
                core.setDeviceConfirmation(deviceId, scope, side, require)
                mutableNotices.tryEmit(R.string.devices_confirmation_saved)
                refresh()
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                val offline =
                    side == Local.ConfirmationSide.CONFIRMATION_SIDE_PEER &&
                        (e as? CoreException)?.isUnavailable == true
                mutableNotices.tryEmit(
                    if (offline) R.string.devices_confirmation_offline else R.string.devices_confirmation_failed,
                )
            }
        }
    }

    /** Allows a paired device to change the confirmation flags of this device remotely. */
    fun setDeviceRemoteSettings(
        deviceId: String,
        allow: Boolean,
    ) {
        if (deviceId.isEmpty()) {
            return
        }
        scope.launch {
            try {
                core.setDeviceRemoteSettings(deviceId, allow)
                mutableNotices.tryEmit(R.string.devices_remote_settings_saved)
                refresh()
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                mutableNotices.tryEmit(R.string.devices_remote_settings_failed)
            }
        }
    }

    /** Allows every paired device to change the confirmation flags of this device remotely. */
    fun setAllowRemoteSettings(allow: Boolean) {
        scope.launch {
            try {
                val settings = core.setAllowRemoteSettings(allow)
                mutableState.update { it.copy(settings = settings) }
                mutableNotices.tryEmit(R.string.settings_remote_settings_saved)
                refresh()
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                mutableNotices.tryEmit(R.string.settings_remote_settings_failed)
            }
        }
    }

    /** Lets a held clipboard item go without applying it; it stays in the history. */
    fun skipHeldItem(itemId: String) {
        held.remove(itemId)
        heldPrompts.update { it - itemId }
    }

    /** Renames a paired device; the name stays on this device. */
    fun renameDevice(
        deviceId: String,
        name: String,
    ) {
        val trimmed = name.trim()
        if (deviceId.isEmpty() || trimmed.isEmpty()) {
            return
        }
        scope.launch {
            try {
                core.renameDevice(deviceId, trimmed)
                mutableNotices.tryEmit(R.string.devices_renamed)
                refresh()
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                mutableNotices.tryEmit(R.string.devices_rename_failed)
            }
        }
    }

    /** Removes a paired device from the group for good. */
    fun revokeDevice(deviceId: String) {
        if (deviceId.isEmpty()) {
            return
        }
        scope.launch {
            try {
                core.revokeDevice(deviceId)
                mutableNotices.tryEmit(R.string.devices_removed)
                refresh()
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                mutableNotices.tryEmit(R.string.devices_remove_failed)
            }
        }
    }

    /** Leaves the group: the other devices stop syncing with this one. */
    fun leaveGroup() {
        val own =
            mutableState.value.status
                ?.deviceId
                .orEmpty()
        if (own.isEmpty()) {
            return
        }
        scope.launch {
            try {
                core.revokeDevice(own)
                mutableNotices.tryEmit(R.string.settings_left_group)
                refresh()
                refreshHistory()
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                mutableNotices.tryEmit(R.string.settings_leave_group_failed)
            }
        }
    }

    /** Reloads the relay access and the state of its client. */
    fun refreshRelay() {
        scope.launch {
            try {
                val relay = core.relay()
                mutableState.update { it.copy(relay = relay) }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                // The status line keeps the last known state.
            }
        }
    }

    /** Stores the relay address and token of the group. */
    fun setRelay(
        url: String,
        token: String?,
        filesOnRequest: Boolean?,
    ) {
        scope.launch {
            try {
                val relay = core.setRelay(url.trim(), token?.trim()?.takeIf { it.isNotEmpty() }, filesOnRequest)
                mutableState.update { it.copy(relay = relay) }
                mutableNotices.tryEmit(R.string.relay_saved)
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                mutableNotices.tryEmit(R.string.relay_save_failed)
            }
        }
    }

    /** Turns the relay off for the whole group. */
    fun relayOff() {
        scope.launch {
            try {
                val relay = core.relayOff()
                mutableState.update { it.copy(relay = relay) }
                mutableNotices.tryEmit(R.string.relay_off_done)
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                mutableNotices.tryEmit(R.string.relay_off_failed)
            }
        }
    }

    /** Checks that the relay accepts this device, without storing the settings. */
    fun testRelay(
        url: String,
        token: String?,
    ) {
        mutableState.update { it.copy(relayChecking = true, relayCheck = null) }
        scope.launch {
            try {
                val check = core.testRelay(url.trim(), token?.trim()?.takeIf { it.isNotEmpty() })
                mutableState.update { it.copy(relayChecking = false, relayCheck = check) }
            } catch (e: CancellationException) {
                mutableState.update { it.copy(relayChecking = false) }
                throw e
            } catch (e: Exception) {
                mutableState.update { it.copy(relayChecking = false) }
                mutableNotices.tryEmit(R.string.relay_check_failed)
            }
        }
    }

    /** Makes this device ask before it takes the content of [scope] from any device. */
    fun setSelfConfirmation(
        scope: Local.ConfirmationScope,
        require: Boolean,
    ) {
        this.scope.launch {
            try {
                val settings = core.setSelfConfirmation(scope, require)
                mutableState.update { it.copy(settings = settings) }
                mutableNotices.tryEmit(R.string.settings_confirmation_saved)
                refresh()
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                mutableNotices.tryEmit(R.string.settings_confirmation_failed)
            }
        }
    }

    /** Stores the name this device announces to the peers. */
    fun setDeviceName(name: String) {
        scope.launch {
            try {
                core.setDeviceName(name.trim())
                mutableNotices.tryEmit(R.string.settings_name_saved)
                refresh()
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                mutableNotices.tryEmit(R.string.settings_name_failed)
            }
        }
    }

    /** Mirrors the consent to report errors to the developer into the core. */
    fun setSentryEnabled(enabled: Boolean) {
        scope.launch {
            try {
                val settings = core.setSentryEnabled(enabled)
                mutableState.update { it.copy(settings = settings) }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                mutableNotices.tryEmit(R.string.settings_diagnostics_failed)
            }
        }
    }

    /** Turns the application log of the app and the core on or off. */
    fun setJournalEnabled(enabled: Boolean) {
        scope.launch {
            try {
                val settings = core.setJournalEnabled(enabled)
                mutableState.update { it.copy(settings = settings) }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                mutableNotices.tryEmit(R.string.settings_journal_failed)
            }
        }
    }

    /** Mirrors the removal of the tracking parameters from the links into the core. */
    fun setStripTracking(enabled: Boolean) {
        scope.launch {
            try {
                val settings = core.setStripTracking(enabled)
                mutableState.update { it.copy(settings = settings) }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                mutableNotices.tryEmit(R.string.settings_strip_tracking_failed)
            }
        }
    }

    /** Stops the state updates. */
    fun close() {
        scope.cancel()
    }

    /** Selects the role the next pairing window proposes. */
    fun setPairingRole(role: Local.Role) {
        mutablePairing.update { it.copy(role = role) }
    }

    /** Opens a pairing window and loads its code and the nearby devices. */
    fun openPairingWindow() {
        if (mutablePairing.value.busy) {
            return
        }
        mutablePairing.update { it.copy(busy = true, failure = null, result = null) }
        scope.launch {
            try {
                val role = mutablePairing.value.role
                applyWindow(core.openPairingWindow(role), role)
                loadPairingQr()
                loadPairingCandidates()
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                if (!replaceOpenWindow()) {
                    mutablePairing.update { it.copy(failure = R.string.pairing_window_failed) }
                }
            } finally {
                mutablePairing.update { it.copy(busy = false) }
            }
        }
    }

    /** Restarts the auto-close timer of the pairing window. */
    fun extendPairingWindow() {
        if (mutablePairing.value.window == null || mutablePairing.value.busy) {
            return
        }
        mutablePairing.update { it.copy(busy = true, failure = null) }
        scope.launch {
            try {
                applyWindow(core.extendPairingWindow(), mutablePairing.value.role)
                loadPairingQr()
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                mutablePairing.update { it.copy(failure = R.string.pairing_extend_failed) }
                closePairingState()
            } finally {
                mutablePairing.update { it.copy(busy = false) }
            }
        }
    }

    /** Closes the pairing window and the exchanges in it. */
    fun closePairingWindow() {
        if (mutablePairing.value.busy) {
            return
        }
        mutablePairing.update { it.copy(busy = true) }
        scope.launch {
            try {
                core.closePairingWindow()
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                mutablePairing.update { it.copy(failure = R.string.pairing_close_failed) }
            } finally {
                closePairingState()
                mutablePairing.update { it.copy(busy = false) }
            }
        }
    }

    /** Reloads the devices that are open for pairing. */
    fun refreshPairingCandidates() {
        scope.launch { loadPairingCandidates() }
    }

    /** Pairs with a discovered candidate, both devices compare a code. */
    fun startPairing(addr: String) {
        if (addr.isEmpty() || mutablePairing.value.window == null) {
            return
        }
        mutablePairing.update { it.copy(dialing = addr, failure = null, result = null) }
        scope.launch {
            try {
                core.startPairing(addr)
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                mutablePairing.update { it.copy(dialing = "", failure = R.string.pairing_start_failed) }
            }
        }
    }

    /** Accepts the pairing request that waits for the answer. */
    fun confirmPairing() {
        val request = mutablePairing.value.request ?: return
        if (mutablePairing.value.busy) {
            return
        }
        mutablePairing.update { it.copy(busy = true, failure = null) }
        scope.launch {
            try {
                core.confirmPairing(request.deviceId)
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                mutablePairing.update { it.copy(request = null, failure = R.string.pairing_confirm_failed) }
            } finally {
                mutablePairing.update { it.copy(busy = false) }
            }
        }
    }

    /** Refuses the pairing request that waits for the answer. */
    fun rejectPairing() {
        val request = mutablePairing.value.request ?: return
        if (mutablePairing.value.busy) {
            return
        }
        mutablePairing.update { it.copy(busy = true, failure = null) }
        scope.launch {
            try {
                core.rejectPairing(request.deviceId)
                mutablePairing.update {
                    it.copy(
                        request = null,
                        result = PairingResult(paired = false, rejected = true),
                    )
                }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                mutablePairing.update { it.copy(request = null, failure = R.string.pairing_reject_failed) }
            } finally {
                mutablePairing.update { it.copy(busy = false) }
            }
        }
    }

    /** Pairs with the device of a scanned code. */
    fun scanPairingCode(uri: String) {
        val text = uri.trim()
        if (text.isEmpty() || mutablePairing.value.scanning) {
            return
        }
        mutablePairing.update { it.copy(failure = null, result = null) }
        scope.launch {
            try {
                core.scanPairingQr(text)
                mutablePairing.update { it.copy(scanning = true) }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                val failure =
                    if (e is CoreException && e.isInvalidArgument) {
                        R.string.pairing_code_invalid
                    } else {
                        R.string.pairing_scan_failed
                    }
                mutablePairing.update { it.copy(scanning = false, failure = failure) }
            }
        }
    }

    /**
     * Keeps the code of a link that another app opened. Any app or web page can
     * open such a link, so the code waits until the user confirms it.
     */
    fun offerPairingLink(uri: String) {
        val text = uri.trim()
        if (text.isEmpty()) {
            return
        }
        mutablePairing.update { it.copy(link = PairingLinkOffer(uri = text, role = PairingLink.role(text))) }
    }

    /** Pairs with the device of the link the user confirmed. */
    fun acceptPairingLink() {
        val link = mutablePairing.value.link ?: return
        mutablePairing.update { it.copy(link = null) }
        scanPairingCode(link.uri)
    }

    /** Drops the code of a link the user refused. */
    fun dismissPairingLink() {
        mutablePairing.update { it.copy(link = null) }
    }

    /** Stops waiting for the device of a scanned code. */
    fun cancelScan() {
        mutablePairing.update { it.copy(scanning = false) }
    }

    /** Drops the outcome and the failure of the last exchange. */
    fun dismissPairingResult() {
        mutablePairing.update { it.copy(result = null, failure = null) }
    }

    /** Runs one action on an entry and marks it as busy meanwhile. */
    private fun runHistoryAction(
        itemId: String,
        @StringRes failure: Int,
        action: suspend () -> Unit,
    ) {
        if (itemId.isEmpty() || itemId in mutableHistory.value.busy) {
            return
        }
        mutableHistory.update { it.copy(busy = it.busy + itemId) }
        scope.launch {
            try {
                action()
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                mutableNotices.tryEmit(failure)
            } finally {
                mutableHistory.update { it.copy(busy = it.busy - itemId) }
            }
        }
    }

    /** Reads the offers of Friends, keeping the list on screen meanwhile. */
    private suspend fun loadRequestsBlocking() {
        try {
            val response = core.requests()
            mutableRequests.update {
                it.copy(loading = false, loaded = true, requests = response.requestsList, failure = null)
            }
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            mutableRequests.update { it.copy(loading = false, failure = failureOf(e)) }
        }
    }

    /** Reads the settings of the core, null when they are not available. */
    private suspend fun loadSettings(): Local.Settings? =
        try {
            core.settings()
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            null
        }

    private suspend fun loadRelay(): Local.Relay? =
        try {
            core.relay()
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            null
        }

    private fun failureOf(e: Exception): CoreFailure =
        if (e is CoreException && e.isNotRunning) CoreFailure.STOPPED else CoreFailure.UNAVAILABLE

    /** Stores an open window and follows its deadline. */
    private fun applyWindow(
        window: Local.PairingWindow,
        role: Local.Role,
    ) {
        if (!window.open) {
            closePairingState()
            return
        }
        mutablePairing.update { it.copy(window = window, role = role, qr = null, failure = null) }
        startPairingPoll()
        scheduleWindowClose(window.expiresAtUnixMs)
    }

    /** Replaces a window that was left open by an earlier session of the app. */
    private suspend fun replaceOpenWindow(): Boolean =
        try {
            core.closePairingWindow()
            val role = mutablePairing.value.role
            applyWindow(core.openPairingWindow(role), role)
            loadPairingQr()
            loadPairingCandidates()
            true
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            false
        }

    /** Reloads the code of the open window. */
    private suspend fun loadPairingQr() {
        val window = mutablePairing.value.window ?: return
        try {
            val qr = core.pairingQr(qrCodeAddresses(window.addr, localAddresses()))
            mutablePairing.update { it.copy(qr = qr) }
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            // A window that closed in the core reports itself, its code is not
            // a failure of the screen.
            if (e is CoreException && e.isNotRunning) {
                return
            }
            mutablePairing.update { it.copy(failure = R.string.pairing_code_failed) }
        }
    }

    /** Reloads the devices that are open for pairing. */
    private suspend fun loadPairingCandidates() {
        val window = mutablePairing.value.window ?: return
        if (loadingCandidates) {
            return
        }
        loadingCandidates = true
        try {
            val candidates =
                withoutOwnService(
                    candidates = core.pairingCandidates(),
                    windowAddr = window.addr,
                    ipv4 = localAddresses(),
                )
            mutablePairing.update { it.copy(candidates = candidates) }
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            // The window may be closing, the next poll loads the list again.
        } finally {
            loadingCandidates = false
        }
    }

    /** Polls the candidates while the window is open. */
    private fun startPairingPoll() {
        if (pollCandidates != null) {
            return
        }
        pollCandidates =
            scope.launch {
                while (isActive) {
                    delay(CANDIDATE_POLL_MS)
                    loadPairingCandidates()
                }
            }
    }

    /** Stops the candidate poll. */
    private fun stopPairingPoll() {
        pollCandidates?.cancel()
        pollCandidates = null
    }

    /** Drops the window state when its deadline passes. */
    private fun scheduleWindowClose(expiresAtUnixMs: Long) {
        closeWindow?.cancel()
        closeWindow = null
        if (expiresAtUnixMs <= 0) {
            return
        }
        closeWindow =
            scope.launch {
                delay((expiresAtUnixMs - System.currentTimeMillis()).coerceAtLeast(0) + CLOSE_MARGIN_MS)
                val window = mutablePairing.value.window ?: return@launch
                if (window.expiresAtUnixMs <= System.currentTimeMillis()) {
                    closePairingState()
                }
            }
    }

    /** Drops the window, its code and the exchanges in it. */
    private fun closePairingState() {
        stopPairingPoll()
        closeWindow?.cancel()
        closeWindow = null
        mutablePairing.update {
            it.copy(
                window = null,
                qr = null,
                candidates = emptyList(),
                request = null,
                dialing = "",
                scanning = false,
            )
        }
    }

    /** Tells the user about a peer that speaks another protocol version. */
    private fun onPeerCompatibilityEvent(event: Mobile.Event) {
        if (!event.hasEvent() || event.event.kind != Local.EventKind.EVENT_KIND_PEER_INCOMPATIBLE) {
            return
        }
        mutableNotices.tryEmit(
            if (event.event.protocolNewer) {
                R.string.protocol_newer_notice
            } else {
                R.string.protocol_older_notice
            },
        )
    }

    /** Follows the pairing events of the core. */
    private fun onPairingEvent(event: Mobile.Event) {
        if (!event.hasEvent()) {
            return
        }
        val payload = event.event
        when (payload.kind) {
            Local.EventKind.EVENT_KIND_PAIRING_CANDIDATE -> {
                if (mutablePairing.value.window != null) {
                    refreshPairingCandidates()
                }
            }
            Local.EventKind.EVENT_KIND_PAIRING_REQUEST ->
                mutablePairing.update {
                    it.copy(
                        request =
                            PairingRequest(
                                deviceId = payload.deviceId,
                                name = payload.name,
                                role = payload.role,
                                platform = payload.platform,
                                sas = if (payload.hasSas()) payload.sas else null,
                                method = payload.pairingMethod,
                            ),
                        dialing = "",
                        scanning = false,
                        result = null,
                        failure = null,
                    )
                }
            Local.EventKind.EVENT_KIND_PAIRING_PAIRED -> {
                mutablePairing.update {
                    it.copy(
                        request = null,
                        dialing = "",
                        scanning = false,
                        result = PairingResult(paired = true, name = payload.name),
                        failure = null,
                    )
                }
                mutableNotices.tryEmit(R.string.pairing_paired_notice)
                scope.launch {
                    loadPairingQr()
                    loadPairingCandidates()
                }
            }
            Local.EventKind.EVENT_KIND_PAIRING_FAILED ->
                mutablePairing.update {
                    it.copy(
                        request = null,
                        dialing = "",
                        scanning = false,
                        result =
                            PairingResult(
                                paired = false,
                                error = payload.error,
                                unreachable = payload.failureReason == Local.FailureReason.FAILURE_REASON_UNREACHABLE,
                            ),
                    )
                }
            Local.EventKind.EVENT_KIND_PAIRING_CLOSED -> closePairingState()
            else -> Unit
        }
    }

    /** Remembers a held clipboard item, so a dialog can ask about it. */
    private fun rememberHeld(event: Local.Event) {
        val item = event.item
        if (item.id.isEmpty()) {
            return
        }
        val device =
            event.name.ifEmpty {
                mutableState.value.status
                    ?.devicesList
                    ?.firstOrNull { it.deviceId == event.deviceId }
                    ?.name
                    .orEmpty()
            }
        heldPrompts.update { it + (item.id to PendingPrompt.clipboard(item, device)) }
        held.add(item.id)
    }

    /** Events that change the list of devices in the status. */
    private fun Mobile.Event.changesDevices(): Boolean =
        when (type) {
            Mobile.EventType.EVENT_TYPE_PEER_ONLINE,
            Mobile.EventType.EVENT_TYPE_PEER_OFFLINE,
            Mobile.EventType.EVENT_TYPE_PAIRING_PAIRED,
            -> true
            else ->
                hasEvent() &&
                    (
                        event.kind == Local.EventKind.EVENT_KIND_DEVICE_UPDATED ||
                            event.kind == Local.EventKind.EVENT_KIND_PEER_UNREACHABLE ||
                            event.kind == Local.EventKind.EVENT_KIND_PEER_INCOMPATIBLE
                    )
        }

    /** Events that change the entries of the history. */
    private fun Mobile.Event.changesHistory(): Boolean {
        if (!hasEvent()) {
            return false
        }
        return when (event.kind) {
            Local.EventKind.EVENT_KIND_CLIPBOARD,
            Local.EventKind.EVENT_KIND_CLIP_DOWNLOAD,
            Local.EventKind.EVENT_KIND_CLIP_APPLY,
            Local.EventKind.EVENT_KIND_HISTORY,
            -> true
            else -> false
        }
    }

    /** A new offer or a change of a listed one; the progress of a transfer is not. */
    private fun Mobile.Event.changesRequests(): Boolean =
        hasEvent() &&
            IncomingFiles.changesRequests(event, mutableRequests.value.requests.mapTo(HashSet()) { it.id })

    /** Events of an item that waits for the user to apply it. */
    private fun Mobile.Event.isHeld(): Boolean = hasEvent() && event.kind == Local.EventKind.EVENT_KIND_CLIP_APPLY

    private companion object {
        const val NOTICE_BUFFER = 4

        /** Interval of the candidate list while the window is open. */
        const val CANDIDATE_POLL_MS = 2_000L

        /** Time after the deadline before the window state is dropped. */
        const val CLOSE_MARGIN_MS = 250L
    }
}
