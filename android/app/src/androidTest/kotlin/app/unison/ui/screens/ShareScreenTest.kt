package app.unison.ui.screens

import android.net.Uri
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.assertIsEnabled
import androidx.compose.ui.test.assertIsNotEnabled
import androidx.compose.ui.test.junit4.v2.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.test.ext.junit.runners.AndroidJUnit4
import app.unison.R
import app.unison.clipboard.ClipboardDraft
import app.unison.history.HistoryText
import app.unison.history.SizeUnits
import app.unison.testContext
import app.unison.testQuantity
import app.unison.testString
import app.unison.share.ShareContent
import app.unison.share.SharedFile
import app.unison.ui.state.CoreState
import app.unison.ui.theme.UnisonTheme
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import unison.local.v1.Local

@RunWith(AndroidJUnit4::class)
class ShareScreenTest {

    @get:Rule
    val compose = createComposeRule()

    @Test
    fun filesOfferPersonalAndFriendsSeparately() {
        setContent(files(), selection = emptySet())

        compose.onNodeWithText(testString(R.string.devices_section_personal)).assertIsDisplayed()
        compose.onNodeWithText(testString(R.string.devices_section_friends)).assertIsDisplayed()
        compose.onNodeWithText("Laptop").assertIsDisplayed()
        compose.onNodeWithText("Friend").assertIsDisplayed()
        compose.onNodeWithText(
            testQuantity(
                R.plurals.share_files_summary,
                1,
                1,
                HistoryText.formatSize(1024, SizeUnits.of(testContext)),
            ),
        ).assertIsDisplayed()
    }

    @Test
    fun offlineDevicesCannotBePicked() {
        setContent(files(), selection = emptySet())

        compose.onNodeWithText("Old phone").assertIsNotEnabled()
    }

    @Test
    fun pickedDevicesAreToggled() {
        setContent(files(), selection = emptySet())

        compose.onNodeWithText("Laptop").performClick()

        assertEquals(PERSONAL_ID to true, toggled)
    }

    @Test
    fun sendNeedsAnOnlineSelection() {
        setContent(files(), selection = emptySet())

        compose.onNodeWithText(testString(R.string.action_send)).assertIsNotEnabled()
    }

    @Test
    fun sendStartsWithOnlineDevicesPicked() {
        setContent(files(), selection = setOf(PERSONAL_ID))

        compose.onNodeWithText(testString(R.string.action_send)).assertIsEnabled().performClick()

        assertTrue(sent)
    }

    @Test
    fun textHidesFriendsAndListsRecipients() {
        setContent(text(), selection = emptySet())

        compose.onNodeWithText(testString(R.string.share_text_note)).assertIsDisplayed()
        compose.onNodeWithText(testString(R.string.share_text_recipients, "Laptop")).assertIsDisplayed()
        compose.onNodeWithText(testString(R.string.devices_section_friends)).assertDoesNotExist()
        compose.onNodeWithText(testString(R.string.action_send)).assertIsEnabled()
    }

    private var toggled: Pair<String, Boolean>? = null
    private var sent = false

    private fun setContent(content: ShareContent, selection: Set<String>) {
        compose.setContent {
            UnisonTheme(dynamicColor = false) {
                ShareScreen(
                    content = content,
                    state = CoreState(loading = false, status = status()),
                    selection = selection,
                    sending = false,
                    onToggle = { id, selected -> toggled = id to selected },
                    onSend = { sent = true },
                    onRetry = {},
                    onClose = {},
                )
            }
        }
    }

    private fun files(): ShareContent = ShareContent.Files(
        listOf(SharedFile(Uri.parse("content://test/doc"), "photo.jpg", 1024, "image/jpeg")),
    )

    private fun text(): ShareContent = ShareContent.Text(
        ClipboardDraft(text = "hello", html = null),
    )

    private fun status(): Local.GetStatusResponse = Local.GetStatusResponse.newBuilder()
        .setDeviceName("Pixel")
        .addDevices(device("Laptop", PERSONAL_ID, Local.Role.ROLE_PERSONAL, online = true))
        .addDevices(device("Old phone", OFFLINE_ID, Local.Role.ROLE_PERSONAL, online = false))
        .addDevices(device("Friend", FRIEND_ID, Local.Role.ROLE_FRIENDS, online = true))
        .build()

    private fun device(name: String, id: String, role: Local.Role, online: Boolean): Local.Device =
        Local.Device.newBuilder()
            .setName(name)
            .setDeviceId(id)
            .setRole(role)
            .setPlatform(Local.Platform.PLATFORM_MACOS)
            .setPath(if (online) Local.Path.PATH_LAN else Local.Path.PATH_UNSPECIFIED)
            .setOnline(online)
            .build()

    private companion object {
        val PERSONAL_ID = "11".repeat(32)
        val OFFLINE_ID = "22".repeat(32)
        val FRIEND_ID = "33".repeat(32)
    }
}
