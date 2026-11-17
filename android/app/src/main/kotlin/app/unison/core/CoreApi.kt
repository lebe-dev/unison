package app.unison.core

import android.content.Context
import app.unison.policy.PolicyConfig
import unison.local.v1.Local

/** Local API calls the app screens make. */
interface CoreApi {
    /** Version and runtime state of the core. */
    suspend fun status(): Local.GetStatusResponse

    /** Stores the name this device announces to the peers. */
    suspend fun setDeviceName(name: String): Local.Settings

    /** Current settings of the core. */
    suspend fun settings(): Local.Settings

    /** Mirrors the consent to report errors to the developer. */
    suspend fun setSentryEnabled(enabled: Boolean): Local.Settings

    /** Turns the application log on or off. */
    suspend fun setJournalEnabled(enabled: Boolean): Local.Settings

    /** Removes the tracking parameters from the links of the clipboard text. */
    suspend fun setStripTracking(enabled: Boolean): Local.Settings

    /** Relay access of the group and the state of its client. */
    suspend fun relay(): Local.Relay

    /**
     * Stores the relay address and token. A null [token] keeps the stored one and
     * a null [filesOnRequest] keeps the stored rule.
     */
    suspend fun setRelay(url: String, token: String?, filesOnRequest: Boolean?): Local.Relay

    /** Turns the relay off for the whole group. */
    suspend fun relayOff(): Local.Relay

    /**
     * Makes this device ask before it takes the clipboard items or the files of
     * any device.
     */
    suspend fun setSelfConfirmation(scope: Local.ConfirmationScope, require: Boolean): Local.Settings

    /**
     * Changes one confirmation flag of a paired device: whether this device asks
     * before it takes what the device sends, or whether the device asks before it
     * takes what this device sends. The second one needs the device online.
     */
    suspend fun setDeviceConfirmation(
        deviceId: String,
        scope: Local.ConfirmationScope,
        side: Local.ConfirmationSide,
        require: Boolean,
    )

    /** Removes a device from the group; the ID of this device leaves the group. */
    suspend fun revokeDevice(deviceId: String)

    /** Changes the local name of a device, or the name this device announces. */
    suspend fun renameDevice(deviceId: String, name: String): Local.Device

    /** Changes the pause of the clipboard synchronization. */
    suspend fun setPause(mode: Local.PauseMode, untilUnixMs: Long = 0): Local.Pause

    /** Live history entries, newest first. */
    suspend fun listHistory(
        kind: Local.Kind = Local.Kind.KIND_UNSPECIFIED,
        deviceId: String = "",
        limit: Int = 0,
    ): Local.ListHistoryResponse

    /** Writes one history entry back to the clipboard. */
    suspend fun copyHistoryItem(id: String)

    /** Writes an entry that waited for a confirmation to the clipboard. */
    suspend fun applyHistoryItem(id: String)

    /** Deletes one history entry on every device. */
    suspend fun deleteHistoryItem(id: String)

    /** Downloads the payloads of one entry into [path]. */
    suspend fun fetchHistoryItem(id: String, path: String): Local.FetchHistoryItemResponse

    /** Every file transfer of this device, newest first. */
    suspend fun listTransfers(): Local.ListTransfersResponse

    /** Stops one file transfer. */
    suspend fun cancelTransfer(id: String)

    /** File offers of Friends that wait for an answer, newest first. */
    suspend fun requests(): Local.ListRequestsResponse

    /** Accepts or declines an incoming file offer. */
    suspend fun decideRequest(id: String, accept: Boolean)

    /** Opens a pairing window that proposes [role] to the devices it pairs with. */
    suspend fun openPairingWindow(role: Local.Role): Local.PairingWindow

    /** Restarts the auto-close timer of the pairing window. */
    suspend fun extendPairingWindow(): Local.PairingWindow

    /** Closes the pairing window. */
    suspend fun closePairingWindow()

    /** Code of the open pairing window, with [addrs] instead of the local interfaces. */
    suspend fun pairingQr(addrs: List<String> = emptyList()): Local.PairingQr

    /** Pairs with the device of a pairing code. */
    suspend fun scanPairingQr(uri: String)

    /** Devices that are open for pairing. */
    suspend fun pairingCandidates(): List<Local.PairingCandidate>

    /** Starts a pairing exchange with the candidate address. */
    suspend fun startPairing(addr: String)

    /** Accepts the pairing request of the device. */
    suspend fun confirmPairing(deviceId: String)

    /** Refuses the pairing request of the device. */
    suspend fun rejectPairing(deviceId: String)
}

/** Core that starts the service when it does not answer yet. */
class RunningCore(
    private val context: Context,
    private val client: CoreClient = CoreClient(),
) : CoreApi {

    override suspend fun status(): Local.GetStatusResponse {
        ensureRunning()
        return client.status()
    }

    override suspend fun setDeviceName(name: String): Local.Settings {
        ensureRunning()
        return client.setDeviceName(name)
    }

    override suspend fun settings(): Local.Settings {
        ensureRunning()
        return client.settings()
    }

    override suspend fun setSentryEnabled(enabled: Boolean): Local.Settings {
        ensureRunning()
        return client.setSentryEnabled(enabled)
    }

    override suspend fun setJournalEnabled(enabled: Boolean): Local.Settings {
        ensureRunning()
        return client.setJournalEnabled(enabled)
    }

    override suspend fun setStripTracking(enabled: Boolean): Local.Settings {
        ensureRunning()
        return client.setStripTracking(enabled)
    }

    override suspend fun relay(): Local.Relay {
        ensureRunning()
        return client.relay()
    }

    override suspend fun setRelay(url: String, token: String?, filesOnRequest: Boolean?): Local.Relay {
        ensureRunning()
        return client.setRelay(url, token, filesOnRequest)
    }

    override suspend fun relayOff(): Local.Relay {
        ensureRunning()
        return client.relayOff()
    }

    override suspend fun setSelfConfirmation(scope: Local.ConfirmationScope, require: Boolean): Local.Settings {
        ensureRunning()
        return client.setSelfConfirmation(scope, require)
    }

    override suspend fun setDeviceConfirmation(
        deviceId: String,
        scope: Local.ConfirmationScope,
        side: Local.ConfirmationSide,
        require: Boolean,
    ) {
        ensureRunning()
        client.setDeviceConfirmation(deviceId, scope, side, require)
    }

    override suspend fun revokeDevice(deviceId: String) {
        ensureRunning()
        client.revokeDevice(deviceId)
    }

    override suspend fun renameDevice(deviceId: String, name: String): Local.Device {
        ensureRunning()
        return client.renameDevice(deviceId, name)
    }

    override suspend fun setPause(mode: Local.PauseMode, untilUnixMs: Long): Local.Pause {
        ensureRunning()
        return client.setPause(mode, untilUnixMs)
    }

    override suspend fun listHistory(kind: Local.Kind, deviceId: String, limit: Int): Local.ListHistoryResponse {
        ensureRunning()
        return client.listHistory(kind, deviceId, limit)
    }

    override suspend fun copyHistoryItem(id: String) {
        ensureRunning()
        client.copyHistoryItem(id)
    }

    override suspend fun applyHistoryItem(id: String) {
        ensureRunning()
        client.applyHistoryItem(id)
    }

    override suspend fun deleteHistoryItem(id: String) {
        ensureRunning()
        client.deleteHistoryItem(id)
    }

    override suspend fun fetchHistoryItem(id: String, path: String): Local.FetchHistoryItemResponse {
        ensureRunning()
        return client.fetchHistoryItem(id, path)
    }

    override suspend fun listTransfers(): Local.ListTransfersResponse {
        ensureRunning()
        return client.listTransfers()
    }

    override suspend fun cancelTransfer(id: String) {
        ensureRunning()
        client.cancelTransfer(id)
    }

    override suspend fun requests(): Local.ListRequestsResponse {
        ensureRunning()
        return client.requests()
    }

    override suspend fun decideRequest(id: String, accept: Boolean) {
        ensureRunning()
        client.decideRequest(id, accept)
    }

    override suspend fun openPairingWindow(role: Local.Role): Local.PairingWindow {
        ensureRunning()
        return client.openPairingWindow(role)
    }

    override suspend fun extendPairingWindow(): Local.PairingWindow {
        ensureRunning()
        return client.extendPairingWindow()
    }

    override suspend fun closePairingWindow() {
        ensureRunning()
        client.closePairingWindow()
    }

    override suspend fun pairingQr(addrs: List<String>): Local.PairingQr {
        ensureRunning()
        return client.pairingQr(addrs)
    }

    override suspend fun scanPairingQr(uri: String) {
        ensureRunning()
        client.scanPairingQr(uri)
    }

    override suspend fun pairingCandidates(): List<Local.PairingCandidate> {
        ensureRunning()
        return client.pairingCandidates()
    }

    override suspend fun startPairing(addr: String) {
        ensureRunning()
        client.startPairing(addr)
    }

    override suspend fun confirmPairing(deviceId: String) {
        ensureRunning()
        client.confirmPairing(deviceId)
    }

    override suspend fun rejectPairing(deviceId: String) {
        ensureRunning()
        client.rejectPairing(deviceId)
    }

    /** Stores the whole network policy and returns the resulting settings. */
    suspend fun updatePolicy(table: PolicyConfig): Local.Settings {
        ensureRunning()
        return client.updatePolicy(table)
    }

    /** Drops the stored policy overrides and returns the defaults. */
    suspend fun resetPolicy(): Local.Settings {
        ensureRunning()
        return client.resetPolicy()
    }

    /** Changes the lifetime of the history items of one kind. */
    suspend fun setHistoryTtl(kind: Local.Kind, seconds: Long): Local.Settings {
        ensureRunning()
        return client.setHistoryTtl(kind, seconds)
    }

    /** Drops the stored history lifetimes and returns the defaults. */
    suspend fun resetHistoryTtl(): Local.Settings {
        ensureRunning()
        return client.resetHistoryTtl()
    }

    private suspend fun ensureRunning() {
        if (CoreStarter.await(context)) {
            return
        }
        throw CoreException(CoreException.CODE_FAILED_PRECONDITION, "core is not running")
    }
}
