package app.unison.core

import android.content.Context
import unison.local.v1.Local

/** Local API calls the app screens make. */
interface CoreApi {
    /** Version and runtime state of the core. */
    suspend fun status(): Local.GetStatusResponse

    /** Stores the device name used from the next start. */
    suspend fun setDeviceName(name: String): Local.Settings

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

    /** Deletes one history entry on every device. */
    suspend fun deleteHistoryItem(id: String)

    /** Downloads the payloads of one entry into [path]. */
    suspend fun fetchHistoryItem(id: String, path: String): Local.FetchHistoryItemResponse

    /** Every file transfer of this device, newest first. */
    suspend fun listTransfers(): Local.ListTransfersResponse

    /** Stops one file transfer. */
    suspend fun cancelTransfer(id: String)

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

    private suspend fun ensureRunning() {
        if (CoreStarter.await(context)) {
            return
        }
        throw CoreException(CoreException.CODE_FAILED_PRECONDITION, "core is not running")
    }
}
