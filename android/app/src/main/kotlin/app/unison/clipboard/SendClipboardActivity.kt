package app.unison.clipboard

import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.widget.Toast
import androidx.activity.ComponentActivity
import androidx.lifecycle.lifecycleScope
import kotlinx.coroutines.launch

/**
 * Transparent activity of the send tile and of the notification action. The
 * clipboard is readable only by the focused app, so the content is read when
 * the window gains focus.
 */
class SendClipboardActivity : ComponentActivity() {
    private val handler = Handler(Looper.getMainLooper())
    private var started = false

    private val focusTimeout = Runnable {
        if (started) {
            return@Runnable
        }
        started = true
        finish()
    }

    override fun onResume() {
        super.onResume()
        handler.postDelayed(focusTimeout, FOCUS_TIMEOUT_MS)
    }

    override fun onPause() {
        handler.removeCallbacks(focusTimeout)
        super.onPause()
    }

    override fun onWindowFocusChanged(hasFocus: Boolean) {
        super.onWindowFocusChanged(hasFocus)
        if (!hasFocus || started) {
            return
        }
        started = true
        lifecycleScope.launch {
            val outcome = ClipboardSender.sendCurrent(this@SendClipboardActivity)
            Toast.makeText(this@SendClipboardActivity, outcome.message, Toast.LENGTH_SHORT).show()
            finish()
        }
    }

    private companion object {
        /** Time to wait for the window focus. */
        const val FOCUS_TIMEOUT_MS = 3000L
    }
}
