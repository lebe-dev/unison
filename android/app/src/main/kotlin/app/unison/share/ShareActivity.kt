package app.unison.share

import android.content.Intent
import android.os.Bundle
import android.widget.Toast
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.lifecycle.lifecycleScope
import app.unison.R
import app.unison.core.CoreApi
import app.unison.core.CoreException
import app.unison.core.RunningCore
import app.unison.ui.screens.ShareScreen
import app.unison.ui.state.CoreFailure
import app.unison.ui.state.CoreState
import app.unison.ui.theme.UnisonTheme
import kotlinx.coroutines.launch
import unison.local.v1.Local
import kotlin.coroutines.cancellation.CancellationException

/** Share target: sends content of another app to the paired devices. */
class ShareActivity : ComponentActivity() {
    private lateinit var core: CoreApi
    private var content by mutableStateOf<ShareContent?>(null)
    private var state by mutableStateOf(CoreState())
    private var selection by mutableStateOf<Set<String>>(emptySet())
    private var preselected = false
    private var sending by mutableStateOf(false)

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        core = RunningCore(this)
        selection = savedInstanceState?.getStringArrayList(KEY_SELECTION)?.toSet().orEmpty()
        preselected = savedInstanceState?.getBoolean(KEY_PRESELECTED) == true

        setContent {
            UnisonTheme {
                content?.let { shared ->
                    ShareScreen(
                        content = shared,
                        state = state,
                        selection = selection,
                        sending = sending,
                        onToggle = ::toggle,
                        onSend = ::send,
                        onRetry = ::load,
                        onClose = ::finish,
                    )
                }
            }
        }
        takeContent(intent)
    }

    override fun onStart() {
        super.onStart()
        if (content != null) {
            load()
        }
    }

    /**
     * The activity is a single instance, so another share reaches the running
     * one: the content is replaced and the devices are read again.
     */
    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        takeContent(intent)
        if (content != null) {
            load()
        }
    }

    /** Reads the shared content and closes the screen when it carries nothing to send. */
    private fun takeContent(intent: Intent?) {
        val shared = ShareIntents.read(intent, contentResolver)
        if (shared == null) {
            Toast.makeText(this, R.string.share_empty, Toast.LENGTH_SHORT).show()
            finish()
            return
        }
        content = shared
        selection = emptySet()
        preselected = false
        sending = false
    }

    override fun onSaveInstanceState(outState: Bundle) {
        super.onSaveInstanceState(outState)
        outState.putStringArrayList(KEY_SELECTION, ArrayList(selection))
        outState.putBoolean(KEY_PRESELECTED, preselected)
    }

    /** Reads the devices of the core. */
    private fun load() {
        state = state.copy(loading = true, failure = null)
        lifecycleScope.launch {
            state =
                try {
                    CoreState(loading = false, status = core.status())
                } catch (e: CancellationException) {
                    throw e
                } catch (e: Exception) {
                    CoreState(loading = false, failure = failureOf(e))
                }
            selectDefaults()
        }
    }

    /** Picks every online Personal device the first time the list arrives. */
    private fun selectDefaults() {
        val offered = devices().map { it.deviceId }.toSet()
        selection = selection.intersect(offered)
        if (preselected) {
            return
        }
        preselected = true
        selection =
            state
                .devices(Local.Role.ROLE_PERSONAL)
                .filter { it.online }
                .map { it.deviceId }
                .toSet()
    }

    private fun toggle(
        deviceId: String,
        selected: Boolean,
    ) {
        selection = if (selected) selection + deviceId else selection - deviceId
    }

    /** Devices the screen offers; Friends receive files only. */
    private fun devices(): List<Local.Device> =
        state.devices(Local.Role.ROLE_PERSONAL) +
            if (content?.allowsFriends == true) state.devices(Local.Role.ROLE_FRIENDS) else emptyList()

    /** Hands the content to the core and closes the screen when it started. */
    private fun send() {
        val shared = content ?: return
        if (sending) {
            return
        }
        val targets =
            devices()
                .filter { it.online && it.deviceId in selection }
                .map { it.deviceId }
        sending = true
        lifecycleScope.launch {
            val outcome = ShareSender.send(this@ShareActivity, shared, targets)
            sending = false
            Toast.makeText(this@ShareActivity, outcome.message, Toast.LENGTH_SHORT).show()
            if (outcome == ShareOutcome.STARTED) {
                finish()
            }
        }
    }

    private fun failureOf(e: Exception): CoreFailure =
        if (e is CoreException && e.isNotRunning) CoreFailure.STOPPED else CoreFailure.UNAVAILABLE

    private companion object {
        const val KEY_SELECTION = "selection"
        const val KEY_PRESELECTED = "preselected"
    }
}
