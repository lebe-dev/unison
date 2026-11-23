package app.unison.ui.screens

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.assertIsEnabled
import androidx.compose.ui.test.assertIsNotEnabled
import androidx.compose.ui.test.junit4.v2.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollTo
import androidx.compose.ui.test.performTextReplacement
import androidx.test.ext.junit.runners.AndroidJUnit4
import app.unison.R
import app.unison.testString
import app.unison.ui.theme.UnisonTheme
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import unison.local.v1.Local

@RunWith(AndroidJUnit4::class)
class RelaySectionTest {
    @get:Rule
    val compose = createComposeRule()

    @Test
    fun relayIsSavedWithTypedToken() {
        var saved: Triple<String, String?, Boolean?>? = null
        setContent(onSave = { url, token, files ->
            saved = Triple(url, token, files)
        })

        compose.onNodeWithTag("relay_url").performScrollTo().performTextReplacement("relay.example.com:443")
        compose.onNodeWithTag("relay_token").performScrollTo().performTextReplacement("secret")
        compose.onNodeWithText(testString(R.string.action_save)).performScrollTo().performClick()

        assertEquals(Triple("relay.example.com:443", "secret", true), saved)
    }

    @Test
    fun saveIsDisabledWithoutToken() {
        setContent(onSave = { _, _, _ -> })

        compose.onNodeWithTag("relay_url").performScrollTo().performTextReplacement("relay.example.com:443")
        compose.onNodeWithText(testString(R.string.action_save)).performScrollTo().assertIsNotEnabled()
    }

    @Test
    fun storedTokenIsKeptWithoutTyping() {
        var savedToken: String? = "unset"
        setContent(storedToken = true, onSave = { _, token, _ ->
            savedToken = token
        })

        compose.onNodeWithTag("relay_url").performScrollTo().performTextReplacement("relay.example.com:443")
        compose
            .onNodeWithText(testString(R.string.action_save))
            .performScrollTo()
            .assertIsEnabled()
            .performClick()

        assertEquals(null, savedToken)
    }

    @Test
    fun relayIsTurnedOff() {
        var off = false
        setContent(storedToken = true, onOff = { off = true })
        compose.onNodeWithText(testString(R.string.relay_off)).performScrollTo().performClick()

        assertEquals(true, off)
    }

    @Test
    fun typedRelayIsChecked() {
        var checked: Pair<String, String?>? = null
        setContent(storedToken = true, onCheck = { url, token -> checked = url to token })

        compose.onNodeWithTag("relay_url").performScrollTo().performTextReplacement("other.example.com:443")
        compose.onNodeWithText(testString(R.string.relay_check)).performScrollTo().performClick()

        assertEquals("other.example.com:443" to null, checked)
    }

    @Test
    fun checkResultExplainsEachTransport() {
        val check =
            Local.TestRelayResponse
                .newBuilder()
                .setQuic(
                    Local.RelayCheck
                        .newBuilder()
                        .setTransport(Local.RelayTransport.RELAY_TRANSPORT_QUIC)
                        .setFailure(Local.RelayCheckFailure.RELAY_CHECK_FAILURE_TIMEOUT)
                        .setDetail("timeout: no recent network activity"),
                ).setWebsocket(
                    Local.RelayCheck
                        .newBuilder()
                        .setTransport(Local.RelayTransport.RELAY_TRANSPORT_WSS)
                        .setOk(true)
                        .setFailure(Local.RelayCheckFailure.RELAY_CHECK_FAILURE_NONE)
                        .setLatencyMs(85),
                ).build()
        setContent(storedToken = true, check = check)

        compose.onNodeWithText(testString(R.string.relay_check_wss_only)).performScrollTo().assertExists()
        compose.onNodeWithText(testString(R.string.relay_fail_timeout)).performScrollTo().assertExists()
        compose.onNodeWithText(testString(R.string.relay_check_ok, 85L)).performScrollTo().assertExists()
    }

    private fun setContent(
        storedToken: Boolean = false,
        onSave: (url: String, token: String?, filesOnRequest: Boolean?) -> Unit = { _, _, _ -> },
        onOff: () -> Unit = {},
        onCheck: (url: String, token: String?) -> Unit = { _, _ -> },
        check: Local.TestRelayResponse? = null,
    ) {
        val relay =
            Local.Relay
                .newBuilder()
                .setUrl(if (storedToken) "relay.example.com:443" else "")
                .setTokenSet(storedToken)
                .setFilesOnRequest(true)
                .build()
        compose.setContent {
            UnisonTheme(dynamicColor = false) {
                Column(modifier = Modifier.verticalScroll(rememberScrollState())) {
                    RelaySection(
                        relay = relay,
                        onSave = onSave,
                        onOff = onOff,
                        onRefresh = {},
                        checking = false,
                        check = check,
                        onCheck = onCheck,
                    )
                }
            }
        }
    }
}
