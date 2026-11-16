package app.unison.share

import android.content.Context
import android.content.Intent
import android.net.Uri
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import java.io.File
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class ShareIntentsTest {

    private val context: Context get() = ApplicationProvider.getApplicationContext()

    @Test
    fun textCarriesItsPlainAndHtmlForms() {
        val intent = Intent(Intent.ACTION_SEND)
            .setType("text/plain")
            .putExtra(Intent.EXTRA_TEXT, "hello")
            .putExtra(Intent.EXTRA_HTML_TEXT, "<b>hello</b>")

        val content = ShareIntents.read(intent, context.contentResolver) as ShareContent.Text

        assertEquals("hello", content.draft.text)
        assertEquals("<b>hello</b>", content.draft.html)
    }

    @Test
    fun htmlBecomesPlainTextWithoutItsOwnForm() {
        val intent = Intent(Intent.ACTION_SEND)
            .setType("text/html")
            .putExtra(Intent.EXTRA_HTML_TEXT, "<b>hello</b>")

        val content = ShareIntents.read(intent, context.contentResolver) as ShareContent.Text

        assertEquals("hello", content.draft.text)
        assertEquals("<b>hello</b>", content.draft.html)
    }

    @Test
    fun singleStreamKeepsItsNameAndSize() {
        val uri = fileUri("note.txt", 12)

        val intent = Intent(Intent.ACTION_SEND)
            .setType("text/plain")
            .putExtra(Intent.EXTRA_STREAM, uri)

        val content = ShareIntents.read(intent, context.contentResolver) as ShareContent.Files

        assertEquals(1, content.files.size)
        val file = content.files.single()
        assertEquals("note.txt", file.name)
        assertEquals(12, file.size)
        assertEquals("text/plain", file.mimeType)
        assertEquals(uri, file.uri)
    }

    @Test
    fun multipleStreamsAreKeptInOrder() {
        val first = fileUri("first.bin", 3)
        val second = fileUri("second.bin", 5)

        val intent = Intent(Intent.ACTION_SEND_MULTIPLE)
            .setType("application/octet-stream")
            .putParcelableArrayListExtra(Intent.EXTRA_STREAM, arrayListOf(first, second))

        val content = ShareIntents.read(intent, context.contentResolver) as ShareContent.Files

        assertEquals(listOf("first.bin", "second.bin"), content.files.map { it.name })
        assertEquals(listOf(3L, 5L), content.files.map { it.size })
    }

    @Test
    fun clipDataStreamsAreReadToo() {
        val uri = fileUri("clip.bin", 4)
        val intent = Intent(Intent.ACTION_SEND).setType("application/octet-stream")
        intent.clipData = android.content.ClipData.newUri(context.contentResolver, "share", uri)

        val content = ShareIntents.read(intent, context.contentResolver) as ShareContent.Files

        assertEquals(listOf("clip.bin"), content.files.map { it.name })
    }

    @Test
    fun intentWithoutContentIsIgnored() {
        assertNull(ShareIntents.read(null, context.contentResolver))
        assertNull(
            ShareIntents.read(
                Intent(Intent.ACTION_SEND).setType("image/png"),
                context.contentResolver,
            ),
        )
        assertNull(
            ShareIntents.read(
                Intent(Intent.ACTION_SEND).setType("text/plain").putExtra(Intent.EXTRA_TEXT, ""),
                context.contentResolver,
            ),
        )
    }

    @Test
    fun onlyFilesAllowFriends() {
        val text = ShareIntents.read(
            Intent(Intent.ACTION_SEND).setType("text/plain").putExtra(Intent.EXTRA_TEXT, "hi"),
            context.contentResolver,
        ) as ShareContent.Text
        val files = ShareIntents.read(
            Intent(Intent.ACTION_SEND)
                .setType("text/plain")
                .putExtra(Intent.EXTRA_STREAM, fileUri("a.txt", 1)),
            context.contentResolver,
        ) as ShareContent.Files

        assertTrue(!text.allowsFriends)
        assertTrue(files.allowsFriends)
    }

    /** Puts a file of the given size into the cache and returns its provider URI. */
    private fun fileUri(name: String, size: Int): Uri {
        val dir = File(context.cacheDir, "clipboard").apply { mkdirs() }
        val file = File(dir, name).apply { writeBytes(ByteArray(size)) }
        return androidx.core.content.FileProvider.getUriForFile(
            context,
            "${context.packageName}.files",
            file,
        )
    }
}
