package app.unison.ui.state

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class DeviceNamesTest {

    @Test
    fun nameIsTrimmedBeforeValidation() {
        assertTrue(DeviceNames.valid("  Pixel  "))
    }

    @Test
    fun blankNameIsRejected() {
        assertFalse(DeviceNames.valid(""))
        assertFalse(DeviceNames.valid("   "))
    }

    @Test
    fun longNameIsRejected() {
        assertFalse(DeviceNames.valid("a".repeat(DeviceNames.MAX_BYTES + 1)))
        assertTrue(DeviceNames.valid("a".repeat(DeviceNames.MAX_BYTES)))
    }

    @Test
    fun lengthIsMeasuredInBytes() {
        val twoBytes = "\u00e9"
        assertFalse(DeviceNames.valid(twoBytes.repeat(DeviceNames.MAX_BYTES / 2 + 1)))
    }
}
