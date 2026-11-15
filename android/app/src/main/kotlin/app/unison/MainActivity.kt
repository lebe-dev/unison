package app.unison

import android.os.Bundle
import android.widget.Toast
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Card
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import androidx.lifecycle.lifecycleScope
import app.unison.clipboard.ClipboardDraft
import app.unison.clipboard.ClipboardRead
import app.unison.clipboard.ClipboardReader
import app.unison.clipboard.ClipboardSender
import app.unison.core.mobile.Mobile
import app.unison.service.OperatingMode
import app.unison.service.UnisonService
import app.unison.service.UnisonSettings
import kotlinx.coroutines.launch

class MainActivity : ComponentActivity() {
    private val coreVersion by lazy { Mobile.version() }
    private var pending by mutableStateOf<ClipboardDraft?>(null)

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        setContent {
            UnisonApp(
                coreVersion = coreVersion,
                pending = pending,
                onSend = ::send,
                onDismiss = { pending = null },
            )
        }
    }

    override fun onStart() {
        super.onStart()
        UnisonService.start(this)
    }

    override fun onStop() {
        if (UnisonSettings.operatingMode(this) == OperatingMode.APP_OPEN) {
            UnisonService.stop(this)
        }
        super.onStop()
    }

    override fun onWindowFocusChanged(hasFocus: Boolean) {
        super.onWindowFocusChanged(hasFocus)
        if (!hasFocus) {
            return
        }
        refreshPending()
    }

    /** Offers the clipboard when it holds content that was not synchronized before. */
    private fun refreshPending() {
        val draft = (ClipboardReader.read(this) as? ClipboardRead.Draft)?.draft
        pending = draft?.takeIf { !it.isEmpty && ClipboardSender.isNew(this, it) }
    }

    private fun send(draft: ClipboardDraft) {
        lifecycleScope.launch {
            val outcome = ClipboardSender.send(this@MainActivity, draft)
            Toast.makeText(this@MainActivity, outcome.message, Toast.LENGTH_SHORT).show()
            refreshPending()
        }
    }
}

@Composable
fun UnisonApp(
    coreVersion: String,
    pending: ClipboardDraft?,
    onSend: (ClipboardDraft) -> Unit,
    onDismiss: () -> Unit,
) {
    MaterialTheme {
        Scaffold(modifier = Modifier.fillMaxSize()) { innerPadding ->
            Surface(
                modifier = Modifier
                    .fillMaxSize()
                    .padding(innerPadding),
            ) {
                Column(
                    modifier = Modifier
                        .fillMaxSize()
                        .padding(24.dp),
                    verticalArrangement = Arrangement.Center,
                    horizontalAlignment = Alignment.CenterHorizontally,
                ) {
                    Text(text = stringResource(R.string.core_version, coreVersion))
                    if (pending != null) {
                        Spacer(modifier = Modifier.height(24.dp))
                        ClipboardOffer(pending, onSend, onDismiss)
                    }
                }
            }
        }
    }
}

@Composable
private fun ClipboardOffer(
    draft: ClipboardDraft,
    onSend: (ClipboardDraft) -> Unit,
    onDismiss: () -> Unit,
) {
    Card(modifier = Modifier.fillMaxWidth()) {
        Column(modifier = Modifier.padding(16.dp)) {
            Text(
                text = stringResource(R.string.clipboard_offer_title),
                style = MaterialTheme.typography.titleMedium,
            )
            val preview = draft.preview()
            if (preview.isNotEmpty()) {
                Spacer(modifier = Modifier.height(8.dp))
                Text(text = preview, style = MaterialTheme.typography.bodyMedium, maxLines = 3)
            }
            Spacer(modifier = Modifier.height(8.dp))
            Text(
                text = stringResource(R.string.clipboard_offer_body),
                style = MaterialTheme.typography.bodySmall,
            )
            Spacer(modifier = Modifier.height(16.dp))
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.End,
            ) {
                TextButton(onClick = onDismiss) {
                    Text(text = stringResource(R.string.action_dismiss))
                }
                TextButton(onClick = { onSend(draft) }) {
                    Text(text = stringResource(R.string.action_send))
                }
            }
        }
    }
}

@Preview(showBackground = true)
@Composable
private fun UnisonAppPreview() {
    UnisonApp(
        coreVersion = "0.1.0",
        pending = ClipboardDraft(text = "Hello from the desktop", html = null),
        onSend = {},
        onDismiss = {},
    )
}
