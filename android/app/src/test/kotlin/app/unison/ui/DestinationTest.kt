package app.unison.ui

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class DestinationTest {

    @Test
    fun notificationScreensAreResolved() {
        assertEquals(Destination.REQUESTS, Destination.ofScreen("requests"))
        assertEquals(Destination.HISTORY, Destination.ofScreen("history"))
    }

    @Test
    fun unknownScreenIsIgnored() {
        assertNull(Destination.ofScreen("settings"))
        assertNull(Destination.ofScreen(null))
    }
}
