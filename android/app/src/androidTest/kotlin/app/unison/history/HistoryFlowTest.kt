package app.unison.history

import android.content.ContentUris
import android.content.Context
import android.net.Uri
import android.provider.MediaStore
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.v2.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithText
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import app.unison.core.CoreClient
import app.unison.core.CoreStarter
import app.unison.core.coreEvents
import app.unison.ui.screens.HistoryScreen
import app.unison.ui.state.AppState
import app.unison.ui.theme.UnisonTheme
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import unison.local.v1.Local

/**
 * Runs the history against the live core: an item captured through the local API
 * has to appear in the screen, download into the Downloads collection and be
 * deleted on every device.
 */
@RunWith(AndroidJUnit4::class)
class HistoryFlowTest {

    @get:Rule
    val compose = createComposeRule()

    private val context = ApplicationProvider.getApplicationContext<Context>()

    @Test
    fun itemIsListedDownloadedAndDeleted() {
        assertTrue("the core did not start", runBlocking { CoreStarter.await(context, START_TIMEOUT_MS) })
        val core = CoreClient()
        val text = "history probe ${System.currentTimeMillis()}"
        val sent = runBlocking {
            core.call(
                method = "SetClip",
                request = Local.SetClipRequest.newBuilder().setText(text).build(),
                parse = Local.SetClipResponse::parseFrom,
            )
        }
        val id = sent.item.id
        assertTrue("SetClip returned no id", id.isNotEmpty())

        val state = AppState(core, coreEvents.events, HistoryDownloads(context))
        try {
            state.refreshHistory()
            compose.waitUntil(TIMEOUT_MS) { state.history.value.items.any { it.item.id == id } }

            val history = state.history
            compose.setContent {
                val current by history.collectAsState()
                UnisonTheme(dynamicColor = false) {
                    HistoryScreen(
                        history = current,
                        status = null,
                        onRefresh = state::refreshHistory,
                        onKindChange = state::setHistoryKind,
                        onDeviceChange = state::setHistoryDevice,
                        onCopy = { state.copyHistoryItem(it.item.id) },
                        onDownload = { state.downloadHistoryItem(it.item.id) },
                        onDelete = { state.deleteHistoryItem(it.item.id) },
                    )
                }
            }

            compose.waitUntil(TIMEOUT_MS) {
                compose.onAllNodesWithText(text).fetchSemanticsNodes().isNotEmpty()
            }
            compose.onNodeWithText(text).assertIsDisplayed()

            state.downloadHistoryItem(id)
            compose.waitUntil(TIMEOUT_MS) { id !in state.history.value.busy }

            val downloaded = awaitDownloaded(text)
            assertNotNull("the item was not saved into Downloads", downloaded)
            context.contentResolver.delete(downloaded!!, null, null)

            state.deleteHistoryItem(id)
            compose.waitUntil(TIMEOUT_MS) {
                state.history.value.loaded && state.history.value.items.none { it.item.id == id }
            }
        } finally {
            state.close()
        }
    }

    /** Waits until an entry with the content appears in the Downloads collection. */
    private fun awaitDownloaded(text: String): Uri? {
        val deadline = System.currentTimeMillis() + TIMEOUT_MS
        while (System.currentTimeMillis() < deadline) {
            downloads().firstOrNull { read(it) == text }?.let { return it }
            Thread.sleep(POLL_MS)
        }
        return null
    }

    /** Entries of the app inside the Downloads collection. */
    private fun downloads(): List<Uri> {
        val collection = MediaStore.Downloads.getContentUri(MediaStore.VOLUME_EXTERNAL_PRIMARY)
        val projection = arrayOf(MediaStore.Downloads._ID)
        val selection = "${MediaStore.Downloads.RELATIVE_PATH} LIKE ?"
        val uris = mutableListOf<Uri>()
        context.contentResolver.query(collection, projection, selection, arrayOf(DOWNLOADS_PATH), null)?.use { cursor ->
            while (cursor.moveToNext()) {
                uris += ContentUris.withAppendedId(collection, cursor.getLong(0))
            }
        }
        return uris
    }

    /** Content of an entry, empty when it cannot be read. */
    private fun read(uri: Uri): String = try {
        context.contentResolver.openInputStream(uri)?.use { it.readBytes().decodeToString() }.orEmpty()
    } catch (e: Exception) {
        ""
    }

    private companion object {
        const val TIMEOUT_MS = 20_000L
        const val START_TIMEOUT_MS = 20_000L
        const val POLL_MS = 100L
        const val DOWNLOADS_PATH = "Download/Unison%"
    }}
