package app.unison.pairing

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class PairingLinkTest {

    @Test
    fun viewOfPairingCodeIsAccepted() {
        val uri = "unison://pair?v=1&id=abc"
        assertEquals(uri, PairingLink.code(PairingLink.ACTION_VIEW, uri))
    }

    @Test
    fun otherActionIsIgnored() {
        assertNull(PairingLink.code("android.intent.action.MAIN", "unison://pair?v=1"))
        assertNull(PairingLink.code(null, "unison://pair?v=1"))
    }

    @Test
    fun otherLinkIsIgnored() {
        assertNull(PairingLink.code(PairingLink.ACTION_VIEW, null))
        assertNull(PairingLink.code(PairingLink.ACTION_VIEW, ""))
        assertNull(PairingLink.code(PairingLink.ACTION_VIEW, "https://example.com/pair?v=1"))
        assertNull(PairingLink.code(PairingLink.ACTION_VIEW, "unison://other?v=1"))
    }

    @Test
    fun schemeAndHostIgnoreCase() {
        val uri = "UNISON://Pair?v=1"
        assertEquals(uri, PairingLink.code(PairingLink.ACTION_VIEW, uri))
    }
}
