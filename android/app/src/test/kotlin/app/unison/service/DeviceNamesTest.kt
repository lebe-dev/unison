package app.unison.service

import org.junit.Assert.assertEquals
import org.junit.Test

class DeviceNamesTest {
    @Test
    fun shortNameIsKept() {
        assertEquals("Pixel 9", DeviceNames.fit("Pixel 9"))
    }

    @Test
    fun longNameIsCutToTheLimitOfTheCore() {
        val fitted = DeviceNames.fit("a".repeat(300))

        assertEquals(DeviceNames.MAX_BYTES, fitted.toByteArray(Charsets.UTF_8).size)
    }

    @Test
    fun multiByteNameIsCutOnACharacter() {
        val fitted = DeviceNames.fit("ж".repeat(100))

        assertEquals("ж".repeat(DeviceNames.MAX_BYTES / 2), fitted)
    }

    @Test
    fun surrogatePairIsNeverSplit() {
        val fitted = DeviceNames.fit("a" + "😀".repeat(40))

        // One ASCII byte and 31 emoji of four bytes fill 125 bytes; a 32nd would not fit.
        assertEquals("a" + "😀".repeat(31), fitted)
    }

    @Test
    fun surroundingSpacesAreDropped() {
        assertEquals("Phone", DeviceNames.fit("  Phone  "))
    }
}
