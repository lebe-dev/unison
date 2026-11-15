package app.unison.clipboard

import android.app.AlertDialog
import android.os.Bundle
import android.widget.Toast
import androidx.activity.ComponentActivity
import androidx.annotation.StringRes
import androidx.lifecycle.lifecycleScope
import app.unison.R
import kotlinx.coroutines.launch
import unison.local.v1.Local

/** Dialog behind the pause tile with the pause durations. */
class PauseClipboardActivity : ComponentActivity() {
    private var applying = false

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        lifecycleScope.launch {
            show(ClipboardPause.state(this@PauseClipboardActivity))
        }
    }

    private fun show(pause: Local.Pause?) {
        if (isFinishing || isDestroyed) {
            return
        }
        if (pause == null) {
            report(R.string.clipboard_core_unavailable)
            return
        }

        val options = optionsOf(pause)
        AlertDialog.Builder(this)
            .setTitle(R.string.pause_title)
            .setItems(options.map { getString(it.label) }.toTypedArray()) { _, which ->
                apply(options[which].request)
            }
            .setNegativeButton(android.R.string.cancel, null)
            .setOnDismissListener {
                if (!applying) {
                    finish()
                }
            }
            .show()
    }

    private fun optionsOf(pause: Local.Pause): List<Option> = buildList {
        if (pause.paused) {
            add(Option(R.string.pause_resume, PauseRequest.resume()))
        }
        val now = System.currentTimeMillis()
        PauseOption.entries.forEach { add(Option(it.label, it.request(now))) }
    }

    private fun apply(request: PauseRequest) {
        applying = true
        lifecycleScope.launch {
            val changed = ClipboardPause.apply(this@PauseClipboardActivity, request)
            report(if (changed) R.string.pause_changed else R.string.pause_failed)
        }
    }

    private fun report(@StringRes message: Int) {
        Toast.makeText(this, message, Toast.LENGTH_SHORT).show()
        finish()
    }

    /** One row of the pause dialog. */
    private data class Option(@StringRes val label: Int, val request: PauseRequest)
}
