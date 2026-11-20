package app.unison.share

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ShareStreamsTest {
    @Test
    fun contentStreamsAreAccepted() {
        assertTrue(ShareIntents.acceptsScheme("content"))
        assertTrue(ShareIntents.acceptsScheme("CONTENT"))
    }

    @Test
    fun fileAndOtherStreamsAreRefused() {
        assertFalse(ShareIntents.acceptsScheme("file"))
        assertFalse(ShareIntents.acceptsScheme("android.resource"))
        assertFalse(ShareIntents.acceptsScheme("https"))
        assertFalse(ShareIntents.acceptsScheme(""))
        assertFalse(ShareIntents.acceptsScheme(null))
    }
}
