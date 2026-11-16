package app.unison.history

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import app.unison.core.CoreClient
import app.unison.core.CoreStarter
import java.io.File
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import unison.local.v1.Local

/** Exercises the history calls of the local API through the in-process core. */
@RunWith(AndroidJUnit4::class)
class HistoryCallsTest {
    private val context = ApplicationProvider.getApplicationContext<Context>()

    @Test
    fun itemIsListedFetchedAndDeleted() {
        assertTrue("the core did not start", runBlocking { CoreStarter.await(context, TIMEOUT_MS) })

        val core = CoreClient()
        val text = "history call ${System.currentTimeMillis()}"
        val sent = runBlocking {
            core.call(
                method = "SetClip",
                request = Local.SetClipRequest.newBuilder().setText(text).build(),
                parse = Local.SetClipResponse::parseFrom,
            )
        }
        val id = sent.item.id

        val listed = runBlocking { core.listHistory() }
        assertTrue("the item is not listed", listed.itemsList.any { it.item.id == id })

        val staging = File(context.cacheDir, "probe").apply { mkdirs() }
        val fetched = runBlocking { core.fetchHistoryItem(id, staging.absolutePath) }
        assertEquals("the payload was not written", 1, fetched.filesCount)
        assertEquals(text, File(fetched.getFiles(0)).readText())
        staging.deleteRecursively()

        runBlocking { core.deleteHistoryItem(id) }

        val afterDelete = runBlocking { core.listHistory() }
        assertTrue("the item is still listed", afterDelete.itemsList.none { it.item.id == id })

        val missing = runCatching {
            runBlocking {
                core.call(
                    method = "GetHistoryItem",
                    request = Local.GetHistoryItemRequest.newBuilder().setId(id).build(),
                    parse = Local.GetHistoryItemResponse::parseFrom,
                )
            }
        }
        assertTrue("the deleted item is still readable", missing.isFailure)
    }

    private companion object {
        const val TIMEOUT_MS = 30_000L
    }
}
