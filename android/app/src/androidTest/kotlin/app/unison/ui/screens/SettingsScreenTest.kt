package app.unison.ui.screens

import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.v2.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollTo
import androidx.compose.ui.test.performTextReplacement
import androidx.test.ext.junit.runners.AndroidJUnit4
import app.unison.clipboard.PauseRequest
import app.unison.policy.PolicyDefaults
import app.unison.service.OperatingMode
import app.unison.ui.state.CoreState
import app.unison.ui.theme.UnisonTheme
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
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

        compose.onNodeWithText("5 minutes").performScrollTo().performClick()

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
        compose.onNodeWithText("Resume").performScrollTo().performClick()

        assertEquals(Local.PauseMode.PAUSE_MODE_OFF, request?.mode)
    }

    @Test
    fun selfConfirmationIsOffered() {
        var require: Boolean? = null
        setContent(onSelfConfirmationChange = { require = it })

        compose.onNodeWithTag(CONFIRMATION_TAG).performScrollTo().performClick()

        assertEquals(true, require)
    }

    @Test
    fun leavingTheGroupAsksForConfirmation() {
        var left = false
        setContent(onLeaveGroup = { left = true })

        compose.onNodeWithText("Remove this device from the group").performScrollTo().performClick()
        compose.onNodeWithText("Remove this device from the group?").assertIsDisplayed()
        assertFalse(left)

        compose.onNodeWithText("Remove").performClick()

        assertTrue(left)
    }

    private fun setContent(
        deviceName: String = "Pixel",
        operatingMode: OperatingMode = OperatingMode.ALWAYS,
        status: Local.GetStatusResponse = Local.GetStatusResponse.newBuilder()
            .setDeviceName(deviceName)
            .setVersion("0.1.0")
            .build(),
        settings: Local.Settings = Local.Settings.getDefaultInstance(),
        onDeviceNameChange: (String) -> Unit = {},
        onOperatingModeChange: (OperatingMode) -> Unit = {},
        onPause: (PauseRequest) -> Unit = {},
        onRelaySave: (url: String, token: String?, filesOnRequest: Boolean?) -> Unit = { _, _, _ -> },
        onRelayOff: () -> Unit = {},
        onRefreshRelay: () -> Unit = {},
        onSelfConfirmationChange: (Boolean) -> Unit = {},
        onLeaveGroup: () -> Unit = {},
    ) {
        compose.setContent {
            UnisonTheme(dynamicColor = false) {
                SettingsScreen(
                    state = CoreState(loading = false, status = status, settings = settings),
                    deviceName = deviceName,
                    operatingMode = operatingMode,
                    policy = PolicyDefaults.config,
                    onDeviceNameChange = onDeviceNameChange,
                    onOperatingModeChange = onOperatingModeChange,
                    onPolicyChange = {},
                    onPolicyReset = {},
                    onPause = onPause,
                    onRelaySave = onRelaySave,
                    onRelayOff = onRelayOff,
                    onRefreshRelay = onRefreshRelay,
                    onSelfConfirmationChange = onSelfConfirmationChange,
                    onLeaveGroup = onLeaveGroup,
                    onRefresh = {},
                )
            }
        }
    }
}
