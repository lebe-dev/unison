package app.unison.ui.screens

import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.assertIsNotEnabled
import androidx.compose.ui.test.junit4.v2.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.test.ext.junit.runners.AndroidJUnit4
import app.unison.ui.state.CoreFailure
import app.unison.ui.state.RequestsState
import app.unison.ui.theme.UnisonTheme
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import unison.local.v1.Local

@RunWith(AndroidJUnit4::class)
class RequestsScreenTest {

    @get:Rule
    val compose = createComposeRule()

    @Test
    fun offerShowsTheSenderTheFilesAndTheSize() {
        setContent(
            requests = RequestsState(
                loaded = true,
                requests = listOf(request(id = "one", name = "Friend", files = listOf("notes.txt", "plan.pdf"))),
            ),
        )

        compose.onNodeWithText("Friend").assertIsDisplayed()
        compose.onNodeWithText("2 files - 2 KB").assertIsDisplayed()
        compose.onNodeWithText("notes.txt, plan.pdf").assertIsDisplayed()
    }

    @Test
    fun emptyListIsExplained() {
        setContent(requests = RequestsState(loaded = true))

        compose.onNodeWithText("No requests").assertIsDisplayed()
        compose.onAllNodesWithText("Accept").assertCountEquals(0)
    }

    @Test
    fun stoppedCoreShowsRetry() {
        var retried = false
        setContent(
            requests = RequestsState(loaded = true, failure = CoreFailure.STOPPED),
            onRefresh = { retried = true },
        )

        compose.onNodeWithText("Unison is not running").assertIsDisplayed()
        compose.onNodeWithText("Retry").performClick()

        assertTrue(retried)
    }

    @Test
    fun answersReportTheOffer() {
        val requests = RequestsState(
            loaded = true,
            requests = listOf(request(id = "one", name = "Friend", files = listOf("notes.txt"))),
        )
        var accepted: Local.ShareRequest? = null
        var declined: Local.ShareRequest? = null
        setContent(
            requests = requests,
            onAccept = { accepted = it },
            onDecline = { declined = it },
        )

        compose.onNodeWithText("Accept").performClick()
        assertEquals("one", accepted?.id)

        compose.onNodeWithText("Decline").performClick()
        assertEquals("one", declined?.id)
    }

    @Test
    fun answeredOfferWaitsForTheCore() {
        setContent(
            requests = RequestsState(
                loaded = true,
                requests = listOf(request(id = "one", name = "Friend", files = listOf("notes.txt"))),
                answering = setOf("one"),
            ),
        )

        compose.onNodeWithText("Accept").assertIsNotEnabled()
        compose.onNodeWithText("Decline").assertIsNotEnabled()
    }

    private fun setContent(
        requests: RequestsState,
        onRefresh: () -> Unit = {},
        onAccept: (Local.ShareRequest) -> Unit = {},
        onDecline: (Local.ShareRequest) -> Unit = {},
    ) {
        compose.setContent {
            UnisonTheme(dynamicColor = false) {
                RequestsScreen(
                    requests = requests,
                    onRefresh = onRefresh,
                    onAccept = onAccept,
                    onDecline = onDecline,
                )
            }
        }
    }

    private fun request(id: String, name: String, files: List<String>): Local.ShareRequest {
        val builder = Local.ShareRequest.newBuilder()
            .setId(id)
            .setDeviceId("friend-id")
            .setDeviceName(name)
        files.forEach { file ->
            builder.addFiles(Local.TransferFile.newBuilder().setName(file).setSize(1024).build())
        }
        builder.setTotalBytes(files.size * 1024L)
        return builder.build()
    }
}
