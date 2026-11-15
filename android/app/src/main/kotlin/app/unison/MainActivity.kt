package app.unison

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import app.unison.core.mobile.Mobile
import app.unison.core.mobile.Platform

class MainActivity : ComponentActivity() {
    private val coreVersion by lazy { Mobile.version() }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        Mobile.start(ByteArray(0), CorePlatform)
        setContent {
            UnisonApp(coreVersion)
        }
    }

    override fun onDestroy() {
        Mobile.stop()
        super.onDestroy()
    }
}

private object CorePlatform : Platform {
    override fun onEvent(evt: ByteArray) = Unit

    override fun writeClipboard(item: ByteArray) = Unit

    override fun openBlobSink(meta: ByteArray): Long = -1

    override fun keystoreUnwrap(blob: ByteArray): ByteArray = ByteArray(0)
}

@Composable
fun UnisonApp(coreVersion: String) {
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
                }
            }
        }
    }
}

@Preview(showBackground = true)
@Composable
private fun UnisonAppPreview() {
    UnisonApp(coreVersion = "0.1.0")
}
