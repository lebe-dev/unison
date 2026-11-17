package app.unison.ui.screens

import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.assertIsEnabled
import androidx.compose.ui.test.assertIsNotEnabled
import androidx.compose.ui.test.assertIsOff
import androidx.compose.ui.test.assertIsOn
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.isToggleable
import androidx.compose.ui.test.junit4.v2.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performScrollTo
import androidx.compose.ui.test.performClick
import androidx.test.ext.junit.runners.AndroidJUnit4
import app.unison.clipboard.ClipboardDraft
import app.unison.ui.state.CoreFailure
import app.unison.ui.state.CoreState
import app.unison.ui.theme.UnisonTheme
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import unison.local.v1.Local

@RunWith(AndroidJUnit4::class)
class DevicesScreenTest {

    @get:Rule
    val compose = createComposeRule()

    @Test
    fun devicesAreSplitByRole() {
        val state = CoreState(
            loading = false,
            status = Local.GetStatusResponse.newBuilder()
                .setDeviceName("Pixel")
                .addDevices(device("Mac", Local.Role.ROLE_PERSONAL, Local.Path.PATH_LAN, online = true))
                .addDevices(device("Friend", Local.Role.ROLE_FRIENDS, Local.Path.PATH_RELAY, online = true))
                .build(),
        )
        setContent(state)

        compose.onNodeWithText("Personal").assertIsDisplayed()
        compose.onNodeWithText("Friends").assertIsDisplayed()
        compose.onNodeWithText("Mac").assertIsDisplayed()
        compose.onNodeWithText("Friend").assertIsDisplayed()
        compose.onNodeWithText("Online · macOS, LAN").assertIsDisplayed()
        compose.onNodeWithText("Online · macOS, Relay").assertIsDisplayed()
    }

    @Test
    fun emptyListsAreExplained() {
        val state = CoreState(
            loading = false,
            status = Local.GetStatusResponse.newBuilder().setDeviceName("Pixel").build(),
        )
        setContent(state)

        compose.onNodeWithText("No personal devices yet").assertIsDisplayed()
        compose.onNodeWithText("No friends yet").assertIsDisplayed()
    }

    @Test
    fun stoppedCoreShowsRetry() {
        var retried = false
        compose.setContent {
            UnisonTheme(dynamicColor = false) {
                DevicesScreen(
                    state = CoreState(loading = false, failure = CoreFailure.STOPPED),
                    clipboardOffer = null,
                    onRefresh = { retried = true },
                    onSendClipboard = {},
                    onDismissClipboard = {},
                    onSetConfirmation = { _, _, _, _ -> },
                    onRename = { _, _ -> },
                    onRevoke = {},
                )
            }
        }

        compose.onNodeWithText("Unison is not running").assertIsDisplayed()
        compose.onNodeWithText("Retry").performClick()

        assertTrue(retried)
    }

    @Test
    fun clipboardOfferIsSent() {
        val draft = ClipboardDraft(text = "Hello from the desktop", html = null)
        var sent: ClipboardDraft? = null
        compose.setContent {
            UnisonTheme(dynamicColor = false) {
                DevicesScreen(
                    state = CoreState(loading = false),
                    clipboardOffer = draft,
                    onRefresh = {},
                    onSendClipboard = { sent = it },
                    onDismissClipboard = {},
                    onSetConfirmation = { _, _, _, _ -> },
                    onRename = { _, _ -> },
                    onRevoke = {},
                )
            }
        }

        compose.onNodeWithText("Send the clipboard?").assertIsDisplayed()
        compose.onNodeWithText("Send").performClick()

        assertEquals(draft, sent)
    }

    @Test
    fun confirmationIsOfferedForPersonalDevicesOnly() {
        val state = CoreState(
            loading = false,
            status = Local.GetStatusResponse.newBuilder()
                .setDeviceName("Pixel")
                .addDevices(device("Mac", Local.Role.ROLE_PERSONAL, Local.Path.PATH_LAN, online = true))
                .addDevices(device("Friend", Local.Role.ROLE_FRIENDS, Local.Path.PATH_RELAY, online = true))
                .build(),
        )
        var changed: Triple<Local.ConfirmationScope, Local.ConfirmationSide, Boolean>? = null
        compose.setContent {
            UnisonTheme(dynamicColor = false) {
                DevicesScreen(
                    state = state,
                    clipboardOffer = null,
                    onRefresh = {},
                    onSendClipboard = {},
                    onDismissClipboard = {},
                    onSetConfirmation = { _, scope, side, require -> changed = Triple(scope, side, require) },
                    onRename = { _, _ -> },
                    onRevoke = {},
                )
            }
        }

        compose.onAllNodes(isToggleable()).assertCountEquals(0)
        compose.onNodeWithText("Mac").performClick()
        compose.onAllNodes(isToggleable()).assertCountEquals(4)
        compose.onNodeWithContentDescription("Mac asks before accepting files from this device").performClick()

        assertEquals(
            Triple(
                Local.ConfirmationScope.CONFIRMATION_SCOPE_FILES,
                Local.ConfirmationSide.CONFIRMATION_SIDE_PEER,
                true,
            ),
            changed,
        )
    }

    @Test
    fun theReportedConfirmationOfADeviceIsShown() {
        val state = CoreState(
            loading = false,
            status = Local.GetStatusResponse.newBuilder()
                .setDeviceName("Pixel")
                .addDevices(
                    device("Mac", Local.Role.ROLE_PERSONAL, Local.Path.PATH_LAN, online = false).toBuilder()
                        .setPeerRequireConfirmation(true)
                        .build(),
                )
                .build(),
        )
        setContent(state)
        compose.onNodeWithText("Mac").performClick()

        compose.onNodeWithContentDescription("Mac asks before applying the clipboard of this device")
            .assertIsOn()
            .assertIsNotEnabled()
        compose.onNodeWithContentDescription("This device asks before accepting files from Mac")
            .assertIsOff()
            .assertIsEnabled()
    }

    @Test
    fun removalAsksForConfirmation() {
        val state = CoreState(
            loading = false,
            status = Local.GetStatusResponse.newBuilder()
                .setDeviceName("Pixel")
                .addDevices(device("Mac", Local.Role.ROLE_PERSONAL, Local.Path.PATH_LAN, online = true))
                .build(),
        )
        var removed: Local.Device? = null
        compose.setContent {
            UnisonTheme(dynamicColor = false) {
                DevicesScreen(
                    state = state,
                    clipboardOffer = null,
                    onRefresh = {},
                    onSendClipboard = {},
                    onDismissClipboard = {},
                    onSetConfirmation = { _, _, _, _ -> },
                    onRename = { _, _ -> },
                    onRevoke = { removed = it },
                )
            }
        }

        compose.onNodeWithText("Mac").performClick()
        compose.onNodeWithText("Remove device").performScrollTo().performClick()
        compose.onNodeWithText("Remove Mac?").assertIsDisplayed()
        assertNull(removed)

        compose.onNodeWithText("Remove").performClick()

        assertEquals("Mac", removed?.name)
    }

    private fun setContent(state: CoreState) {
        compose.setContent {
            UnisonTheme(dynamicColor = false) {
                DevicesScreen(
                    state = state,
                    clipboardOffer = null,
                    onRefresh = {},
                    onSendClipboard = {},
                    onDismissClipboard = {},
                    onSetConfirmation = { _, _, _, _ -> },
                    onRename = { _, _ -> },
                    onRevoke = {},
                )
            }
        }
    }

    private fun device(name: String, role: Local.Role, path: Local.Path, online: Boolean): Local.Device =
        Local.Device.newBuilder()
            .setName(name)
            .setRole(role)
            .setPlatform(Local.Platform.PLATFORM_MACOS)
            .setPath(path)
            .setOnline(online)
            .build()
}
