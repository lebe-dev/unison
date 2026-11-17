package app.unison.history

import com.google.protobuf.ByteString
import org.junit.Assert.assertEquals
import org.junit.Test
import unison.local.v1.Local

class HistoryTextTest {

    @Test
    fun textIsShortenedAndCollapsed() {
        val item = textItem("hello\n   world")

        assertEquals("hello world", HistoryText.text(item))
    }

    @Test
    fun longTextIsCut() {
        val item = textItem("x".repeat(500))

        val preview = HistoryText.text(item, limit = 10)

        assertEquals("xxxxxxxxxx...", preview)
    }

    @Test
    fun fileNamesAreJoined() {
        val item = Local.ClipItem.newBuilder()
            .setKind(Local.Kind.KIND_FILES)
            .addRepresentations(representation("application/x-unison-file", "a.txt"))
            .addRepresentations(representation("application/x-unison-file", "b.txt"))
            .addRepresentations(representation("application/x-unison-file", "c.txt"))
            .build()

        val names = HistoryText.fileNames(item)

        assertEquals("a.txt, b.txt", names.shown)
        assertEquals(1, names.rest)
    }

    @Test
    fun sizeAddsThePayloads() {
        val item = Local.ClipItem.newBuilder()
            .addRepresentations(Local.Representation.newBuilder().setSize(100))
            .addRepresentations(Local.Representation.newBuilder().setSize(24))
            .build()

        assertEquals(124L, HistoryText.size(item))
    }

    @Test
    fun inlinePayloadCountsWhenTheSizeIsMissing() {
        val item = Local.ClipItem.newBuilder()
            .addRepresentations(Local.Representation.newBuilder().setInlineBytes(ByteString.copyFromUtf8("abc")))
            .build()

        assertEquals(3L, HistoryText.size(item))
    }

    @Test
    fun sizesUseOneUnit() {
        assertEquals("512 B", HistoryText.formatSize(512))
        assertEquals("1 KB", HistoryText.formatSize(1024))
        assertEquals("1.5 KB", HistoryText.formatSize(1536))
        assertEquals("1 MB", HistoryText.formatSize(1024L * 1024))
        assertEquals("2.5 MB", HistoryText.formatSize(5L * 1024 * 1024 / 2))
    }

    private fun textItem(text: String): Local.ClipItem = Local.ClipItem.newBuilder()
        .setKind(Local.Kind.KIND_TEXT)
        .addRepresentations(
            Local.Representation.newBuilder()
                .setMimeType("text/plain")
                .setInlineBytes(ByteString.copyFromUtf8(text))
                .build(),
        )
        .build()

    private fun representation(mime: String, name: String): Local.Representation =
        Local.Representation.newBuilder()
            .setMimeType(mime)
            .setFileName(name)
            .build()
}
