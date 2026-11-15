package app.unison.ui.screens

import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.junit4.v2.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performTextReplacement
import androidx.test.ext.junit.runners.AndroidJUnit4
import app.unison.clipboard.PauseRequest
import app.unison.service.OperatingMode
import app.unison.ui.state.CoreState
import app.unison.ui.theme.UnisonTheme
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import unison.local.v1.Local

@RunWith(AndroidJUnit4::class)
class SettingsScreenTest {

    @get:Rule
    val compose = createComposeRule()

    @Test
    fun serviceModeIsSelected() {
        var selected: OperatingMode? = null
        setContent(operatingMode = OperatingMode.ALWAYS, onOperatingModeChange = { selected = it })

        compose.onNodeWithText("Wi-Fi only").performClick()

        assertEquals(OperatingMode.WIFI_ONLY, selected)
    }

    @Test
    fun deviceNameIsSaved() {
        var saved: String? = null
        setContent(deviceName = "Pixel", onDeviceNameChange = { saved = it })

        compose.onNodeWithText("Name shown to other devices").performTextReplacement("Desk")
        compose.onNodeWithText("Save").performClick()

        assertEquals("Desk", saved)
    }

    @Test
    fun saveIsDisabledWithoutChanges() {
        var saved: String? = null
        setContent(deviceName = "Pixel", onDeviceNameChange = { saved = it })

        compose.onNodeWithText("Save").performClick()

        assertEquals(null, saved)
    }

    @Test
    fun pauseOptionsAreOffered() {
        var request: PauseRequest? = null
        setContent(operatingMode = OperatingMode.ALWAYS, onPause = { request = it })

        compose.onNodeWithText("5 minutes").performClick()

        assertEquals(Local.PauseMode.PAUSE_MODE_UNTIL, request?.mode)
    }

    @Test
    fun pausedClipboardCanBeResumed() {
        var request: PauseRequest? = null
        setContent(
            status = Local.GetStatusResponse.newBuilder()
                .setDeviceName("Pixel")
                .setPause(Local.Pause.newBuilder().setPaused(true).setIndefinite(true).build())
                .build(),
            onPause = { request = it },
        )

        compose.onAllNodesWithText("Until turned off").assertCountEquals(2)
        compose.onNodeWithText("Resume").performClick()

        assertEquals(Local.PauseMode.PAUSE_MODE_OFF, request?.mode)
    }

    private fun setContent(
        deviceName: String = "Pixel",
        operatingMode: OperatingMode = OperatingMode.ALWAYS,
        status: Local.GetStatusResponse = Local.GetStatusResponse.newBuilder()
            .setDeviceName(deviceName)
            .setVersion("0.1.0")
            .build(),
        onDeviceNameChange: (String) -> Unit = {},
        onOperatingModeChange: (OperatingMode) -> Unit = {},
        onPause: (PauseRequest) -> Unit = {},
    ) {
        compose.setContent {
            UnisonTheme(dynamicColor = false) {
                SettingsScreen(
                    state = CoreState(loading = false, status = status),
                    deviceName = deviceName,
                    operatingMode = operatingMode,
                    onDeviceNameChange = onDeviceNameChange,
                    onOperatingModeChange = onOperatingModeChange,
                    onPause = onPause,
                    onRefresh = {},
                )
            }
        }
    }
}
