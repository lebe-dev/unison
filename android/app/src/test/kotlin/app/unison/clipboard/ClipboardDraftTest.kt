package app.unison.clipboard

import com.google.protobuf.ByteString
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import unison.mobile.v1.Mobile

class ClipboardDraftTest {

    @Test
    fun payloadCarriesBothRepresentations() {
        val payload = ClipboardDraft(text = "hello", html = "<b>hello</b>").payload()

        assertEquals(2, payload.partsCount)
        assertEquals(ClipboardDraft.MIME_TEXT_PLAIN, payload.getParts(0).mimeType)
        assertEquals("hello", payload.getParts(0).data.toStringUtf8())
        assertEquals(ClipboardDraft.MIME_TEXT_HTML, payload.getParts(1).mimeType)
        assertEquals("<b>hello</b>", payload.getParts(1).data.toStringUtf8())
    }

    @Test
    fun payloadSkipsEmptyRepresentations() {
        val payload = ClipboardDraft(text = "", html = "<i>x</i>").payload()

        assertEquals(1, payload.partsCount)
        assertEquals(ClipboardDraft.MIME_TEXT_HTML, payload.getParts(0).mimeType)
    }

    @Test
    fun emptyDraftHasNothingToSend() {
        assertTrue(ClipboardDraft(text = null, html = null).isEmpty)
        assertTrue(ClipboardDraft(text = "", html = "").isEmpty)
        assertFalse(ClipboardDraft(text = "x", html = null).isEmpty)
        assertFalse(ClipboardDraft(text = null, html = "<i>x</i>").isEmpty)
    }

    @Test
    fun hashDependsOnEveryRepresentation() {
        val hash = ClipboardDraft(text = "hello", html = null).contentHash()

        assertEquals(hash, ClipboardDraft(text = "hello", html = null).contentHash())
        assertEquals(64, hash.length)
        assertNotEquals(hash, ClipboardDraft(text = "hello!", html = null).contentHash())
        assertNotEquals(hash, ClipboardDraft(text = "hello", html = "<b>hello</b>").contentHash())
        assertNotEquals(
            ClipboardDraft(text = "a", html = "b").contentHash(),
            ClipboardDraft(text = "ab", html = null).contentHash(),
        )
    }

    @Test
    fun previewCollapsesWhitespaceAndTruncates() {
        val draft = ClipboardDraft(text = "  one\n\ttwo   three  ", html = null)

        assertEquals("one two three", draft.preview())
        assertEquals("one...", draft.preview(limit = 3))
        assertEquals("", ClipboardDraft(text = null, html = "<i>x</i>").preview())
    }

    @Test
    fun draftFromPayloadReadsParts() {
        val payload = ClipboardDraft(text = "a", html = "<i>a</i>").payload()

        assertEquals(ClipboardDraft(text = "a", html = "<i>a</i>"), ClipboardDraft.fromPayload(payload))
    }

    @Test
    fun draftFromPayloadFallsBackToTopFields() {
        val payload = Mobile.ClipPayload.newBuilder()
            .setMimeType(ClipboardDraft.MIME_TEXT_PLAIN)
            .setData(ByteString.copyFromUtf8("top"))
            .build()

        assertEquals(ClipboardDraft(text = "top", html = null), ClipboardDraft.fromPayload(payload))
    }

    @Test
    fun draftFromPayloadWithoutTextIsNull() {
        val payload = Mobile.ClipPayload.newBuilder()
            .addParts(
                Mobile.ClipPart.newBuilder()
                    .setMimeType("image/png")
                    .setData(ByteString.copyFrom(byteArrayOf(1))),
            )
            .build()

        assertNull(ClipboardDraft.fromPayload(payload))
        assertNull(ClipboardDraft.fromPayload(Mobile.ClipPayload.getDefaultInstance()))
    }
}
