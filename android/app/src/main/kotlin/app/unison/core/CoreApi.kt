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

    private suspend fun ensureRunning() {
        if (CoreStarter.await(context)) {
            return
        }
        throw CoreException(CoreException.CODE_FAILED_PRECONDITION, "core is not running")
    }
}
