package app.unison.core

import android.content.Context
import android.os.SystemClock
import android.util.Log
import app.unison.service.UnisonService
import kotlin.coroutines.cancellation.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.withContext

/** Starts the core service and waits until the local API answers. */
object CoreStarter {
    private const val TAG = "CoreStarter"

    /** Time to wait for a cold start of the core. */
    const val DEFAULT_TIMEOUT_MS = 3000L

    private const val POLL_MS = 200L

    /** Starts the service when needed and reports whether the core answers. */
    suspend fun await(context: Context, timeoutMs: Long = DEFAULT_TIMEOUT_MS): Boolean {
        if (answers()) {
            return true
        }
        try {
            UnisonService.start(context)
        } catch (e: IllegalStateException) {
            // A foreground service cannot start while the app is in the background.
            Log.w(TAG, "core service cannot start now: ${e.message}")
            return false
        }
        val deadline = SystemClock.elapsedRealtime() + timeoutMs
        while (SystemClock.elapsedRealtime() < deadline) {
            delay(POLL_MS)
            if (answers()) {
                return true
            }
        }
        Log.w(TAG, "core did not answer in $timeoutMs ms")
        return false
    }

    private suspend fun answers(): Boolean = try {
        withContext(Dispatchers.IO) { CoreClient().status() }
        true
    } catch (e: CancellationException) {
        throw e
    } catch (e: Exception) {
        false
    }
}
