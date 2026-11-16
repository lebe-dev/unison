package app.unison.ui.screens

import androidx.compose.ui.test.assertIsEnabled
import androidx.compose.ui.test.assertIsNotEnabled
import androidx.compose.ui.test.junit4.v2.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollTo
import androidx.compose.ui.test.performTextReplacement
import androidx.test.ext.junit.runners.AndroidJUnit4
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
        compose.onNodeWithText("Save").performScrollTo().performClick()

        assertEquals(Triple("relay.example.com:443", "secret", true), saved)
    }

    @Test
    fun saveIsDisabledWithoutToken() {
        setContent(onSave = { _, _, _ -> })

        compose.onNodeWithTag("relay_url").performScrollTo().performTextReplacement("relay.example.com:443")
        compose.onNodeWithText("Save").performScrollTo().assertIsNotEnabled()
    }

    @Test
    fun storedTokenIsKeptWithoutTyping() {
        var savedToken: String? = "unset"
        setContent(storedToken = true, onSave = { _, token, _ ->
            savedToken = token
        })

        compose.onNodeWithTag("relay_url").performScrollTo().performTextReplacement("relay.example.com:443")
        compose.onNodeWithText("Save").performScrollTo().assertIsEnabled().performClick()

        assertEquals(null, savedToken)
    }

    @Test
    fun relayIsTurnedOff() {
        var off = false
        setContent(storedToken = true, onOff = { off = true })
        compose.onNodeWithText("Turn off for the group").performScrollTo().performClick()

        assertEquals(true, off)
    }

    private fun setContent(
        storedToken: Boolean = false,
        onSave: (url: String, token: String?, filesOnRequest: Boolean?) -> Unit = { _, _, _ -> },
        onOff: () -> Unit = {},
    ) {
        val relay = Local.Relay.newBuilder()
            .setUrl(if (storedToken) "relay.example.com:443" else "")
            .setTokenSet(storedToken)
            .setFilesOnRequest(true)
            .build()
        compose.setContent {
            UnisonTheme(dynamicColor = false) {
                RelaySection(
                    relay = relay,
                    onSave = onSave,
                    onOff = onOff,
                    onRefresh = {},
                )
            }
        }
    }
}
