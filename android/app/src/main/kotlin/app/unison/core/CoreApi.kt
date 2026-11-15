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

    private suspend fun ensureRunning() {
        if (CoreStarter.await(context)) {
            return
        }
        throw CoreException(CoreException.CODE_FAILED_PRECONDITION, "core is not running")
    }
}
