package app.unison.ui.state

import androidx.annotation.StringRes
import app.unison.R
import app.unison.clipboard.PauseRequest
import app.unison.core.CoreApi
import app.unison.core.CoreException
import app.unison.history.HistorySaver
import kotlin.coroutines.cancellation.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.channels.BufferOverflow
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import unison.local.v1.Local
import unison.mobile.v1.Mobile

/** State of the screens over the core and the changes they request. */
class AppState(
    private val core: CoreApi,
    events: Flow<Mobile.Event>,
    private val saver: HistorySaver,
    private val scope: CoroutineScope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate),
) {
    private val mutableState = MutableStateFlow(CoreState())

    /** What the devices screen renders. */
    val state: StateFlow<CoreState> = mutableState.asStateFlow()

    private val mutableHistory = MutableStateFlow(HistoryState())

    /** What the history screen renders. */
    val history: StateFlow<HistoryState> = mutableHistory.asStateFlow()

    private val mutableNotices = MutableSharedFlow<Int>(
        extraBufferCapacity = NOTICE_BUFFER,
        onBufferOverflow = BufferOverflow.DROP_OLDEST,
    )

    /** One-off messages for the snackbar, as string resources. */
    val notices: SharedFlow<Int> = mutableNotices.asSharedFlow()

    private var load: Job? = null
    private var loadHistory: Job? = null

    init {
        scope.launch {
            events.collect { event ->
                if (event.changesDevices()) {
                    refresh()
                }
                if (event.changesHistory()) {
                    refreshHistory()
                }
            }
        }
    }

    /** Reloads the status of the core. */
    fun refresh() {
        load?.cancel()
        load = scope.launch {
            mutableState.update { it.copy(loading = it.status == null, failure = null) }
            try {
                mutableState.value = CoreState(loading = false, status = core.status())
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
        loadHistory = scope.launch {
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
    fun copyHistoryItem(itemId: String) = runHistoryAction(
        itemId = itemId,
        failure = R.string.history_copy_failed,
    ) {
        core.copyHistoryItem(itemId)
        mutableNotices.tryEmit(R.string.history_copied)
        refreshHistory()
    }

    /** Saves the payloads of an entry into the Downloads collection. */
    fun downloadHistoryItem(itemId: String) = runHistoryAction(
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
    fun deleteHistoryItem(itemId: String) = runHistoryAction(
        itemId = itemId,
        failure = R.string.history_delete_failed,
    ) {
        core.deleteHistoryItem(itemId)
        mutableNotices.tryEmit(R.string.history_deleted)
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

    /** Stores the device name used from the next start. */
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

    /** Stops the state updates. */
    fun close() {
        scope.cancel()
    }

    /** Runs one action on an entry and marks it as busy meanwhile. */
    private fun runHistoryAction(itemId: String, @StringRes failure: Int, action: suspend () -> Unit) {
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

    private fun failureOf(e: Exception): CoreFailure =
        if (e is CoreException && e.isNotRunning) CoreFailure.STOPPED else CoreFailure.UNAVAILABLE

    /** Events that change the list of devices in the status. */
    private fun Mobile.Event.changesDevices(): Boolean = when (type) {
        Mobile.EventType.EVENT_TYPE_PEER_ONLINE,
        Mobile.EventType.EVENT_TYPE_PEER_OFFLINE,
        Mobile.EventType.EVENT_TYPE_PAIRING_PAIRED,
        -> true
        else -> false
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
            -> true
            else -> false
        }
    }

    private companion object {
        const val NOTICE_BUFFER = 4
    }
}
