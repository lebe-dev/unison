package app.unison.share

import android.content.Context
import android.net.Uri
import androidx.core.content.FileProvider
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import app.unison.clipboard.ClipboardDraft
import app.unison.core.CoreStarter
import app.unison.core.mobile.Mobile
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import java.io.File
import unison.mobile.v1.Mobile as MobileProto

@RunWith(AndroidJUnit4::class)
class ShareSenderTest {
    private val context: Context get() = ApplicationProvider.getApplicationContext()

    /** The service and the core stay up for the whole class; the run ends the process. */
    @Before
    fun startCore() {
        val started = runBlocking { CoreStarter.await(context, START_TIMEOUT_MS) }
        assertTrue("core did not start", started)
    }

    @Test
    fun textIsSubmitted() {
        val content = ShareContent.Text(ClipboardDraft(text = "hello", html = null))

        val outcome =
            runBlocking {
                ShareSender.send(context, content, listOf(UNKNOWN_ID))
            }

        assertEquals(ShareOutcome.STARTED, outcome)
    }

    @Test
    fun emptyTextIsRefused() {
        val content = ShareContent.Text(ClipboardDraft(text = "", html = null))

        val outcome =
            runBlocking {
                ShareSender.send(context, content, listOf(UNKNOWN_ID))
            }

        assertEquals(ShareOutcome.NOTHING, outcome)
    }

    @Test
    fun filesNeedADevice() {
        val content = ShareContent.Files(listOf(file("send.bin", 1024)))

        val outcome =
            runBlocking {
                ShareSender.send(context, content, emptyList())
            }

        assertEquals(ShareOutcome.NO_DEVICES, outcome)
    }

    @Test
    fun fileIsCopiedFromItsDescriptor() {
        val shared = file("stream.bin", MEBIBYTE)
        val descriptor =
            runBlocking(Dispatchers.IO) {
                context.contentResolver.openFileDescriptor(shared.uri, "r")
            }
        assertNotNull(descriptor)
        val fd = descriptor!!.detachFd()
        val meta =
            MobileProto.FilePayload
                .newBuilder()
                .setFileName(shared.name)
                .setMimeType(shared.mimeType)
                .setSize(shared.size)
                .addDeviceIds(UNKNOWN_ID)
                .build()

        // The core copies the whole descriptor before it offers the file, so
        // the missing peer is the only failure that can come out of this call.
        val error =
            runBlocking(Dispatchers.IO) {
                try {
                    Mobile.submitFile(meta.toByteArray(), fd.toLong())
                    null
                } catch (e: Exception) {
                    e.message
                }
            }

        assertNotNull("the core accepted a file without a peer", error)
        assertTrue("unexpected failure: $error", error!!.contains("peer", ignoreCase = true))
    }

    /** Puts a file into the cache and returns its provider metadata. */
    private fun file(
        name: String,
        size: Int,
    ): SharedFile {
        val dir = File(context.cacheDir, "clipboard").apply { mkdirs() }
        val file = File(dir, name)
        file.outputStream().use { out ->
            val block = ByteArray(64 * 1024) { it.toByte() }
            var left = size
            while (left > 0) {
                val count = minOf(left, block.size)
                out.write(block, 0, count)
                left -= count
            }
        }
        val uri: Uri = FileProvider.getUriForFile(context, "${context.packageName}.files", file)
        return SharedFile(uri = uri, name = name, size = size.toLong(), mimeType = MIME)
    }

    private companion object {
        const val START_TIMEOUT_MS = 30_000L
        const val MEBIBYTE = 1024 * 1024
        const val MIME = "application/octet-stream"
        val UNKNOWN_ID = "aa".repeat(32)
    }
}
