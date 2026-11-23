package app.unison.clipboard

import android.content.Context
import android.util.Log
import androidx.annotation.StringRes
import app.unison.R
import app.unison.core.CoreClient
import app.unison.core.CoreStarter
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import unison.local.v1.Local

/** Pause duration offered by the pause dialog. */
enum class PauseOption(
    @StringRes val label: Int,
    val durationMs: Long,
) {
    FIVE_MINUTES(R.string.pause_five_minutes, 5 * 60 * 1000L),
    ONE_HOUR(R.string.pause_one_hour, 60 * 60 * 1000L),
    INDEFINITE(R.string.pause_indefinite, 0L),
    ;

    /** Request for the core that starts at [nowUnixMs]. */
    fun request(nowUnixMs: Long): PauseRequest =
        if (durationMs == 0L) {
            PauseRequest(Local.PauseMode.PAUSE_MODE_INDEFINITE, 0L)
        } else {
            PauseRequest(Local.PauseMode.PAUSE_MODE_UNTIL, nowUnixMs + durationMs)
        }
}

/** Change of the clipboard pause as the local API takes it. */
data class PauseRequest(
    val mode: Local.PauseMode,
    val untilUnixMs: Long,
) {
    companion object {
        /** Turns the pause off. */
        fun resume(): PauseRequest = PauseRequest(Local.PauseMode.PAUSE_MODE_OFF, 0L)
    }
}

/** Pause of the clipboard synchronization. */
object ClipboardPause {
    private const val TAG = "ClipboardPause"

    /** Current pause without starting the core; null when the core does not answer. */
    suspend fun read(): Local.Pause? =
        try {
            withContext(Dispatchers.IO) { CoreClient().pause() }
        } catch (e: Exception) {
            Log.d(TAG, "pause state is not available: ${e.message}")
            null
        }

    /** Current pause after starting the core when needed. */
    suspend fun state(context: Context): Local.Pause? {
        if (!CoreStarter.await(context)) {
            return null
        }
        return read()
    }

    /** Applies [request] and reports whether the core accepted it. */
    suspend fun apply(
        context: Context,
        request: PauseRequest,
    ): Boolean {
        if (!CoreStarter.await(context)) {
            return false
        }
        return try {
            withContext(Dispatchers.IO) {
                CoreClient().setPause(request.mode, request.untilUnixMs)
            }
            true
        } catch (e: Exception) {
            Log.w(TAG, "pause change failed: ${e.message}")
            false
        }
    }
}
