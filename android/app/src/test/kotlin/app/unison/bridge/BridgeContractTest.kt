package app.unison.bridge

import app.unison.history.HistoryText
import com.google.protobuf.ByteString
import org.junit.Assert.assertEquals
import org.junit.Test
import unison.local.v1.Local

class BridgeContractTest {
    @Test
    fun previewCollapsesWhitespace() {
        assertEquals("hello world", BridgeContract.preview("  hello\n\t  world \n"))
    }

    @Test
    fun previewKeepsShortText() {
        val text = "x".repeat(BridgeContract.PREVIEW_LIMIT)

        assertEquals(text, BridgeContract.preview(text))
    }

    @Test
    fun previewCutsLongText() {
        val text = "x".repeat(BridgeContract.PREVIEW_LIMIT + 1)

        assertEquals("x".repeat(BridgeContract.PREVIEW_LIMIT) + "...", BridgeContract.preview(text))
    }

    @Test
    fun previewDropsTheSpaceBeforeTheCut() {
        val text = "x".repeat(BridgeContract.PREVIEW_LIMIT - 1) + " tail"

        assertEquals("x".repeat(BridgeContract.PREVIEW_LIMIT - 1) + "...", BridgeContract.preview(text))
    }

    @Test
    fun previewOfNothingIsEmpty() {
        assertEquals("", BridgeContract.preview(null))
        assertEquals("", BridgeContract.preview(" \n "))
    }

    @Test
    fun previewMatchesTheSummaryOfATextItem() {
        val texts =
            listOf(
                "hello",
                "  spaced\r\n\r\nlines\t\tand tabs  ",
                "word ".repeat(100),
                "y".repeat(400),
            )
        for (text in texts) {
            assertEquals(HistoryText.text(textItem(text)), BridgeContract.preview(text))
        }
    }

    private fun textItem(text: String): Local.ClipItem =
        Local.ClipItem
            .newBuilder()
            .setKind(Local.Kind.KIND_TEXT)
            .addRepresentations(
                Local.Representation
                    .newBuilder()
                    .setMimeType("text/plain")
                    .setInlineBytes(ByteString.copyFromUtf8(text)),
            ).build()
}
