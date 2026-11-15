package app.unison.service

import org.junit.Assert.assertEquals
import org.junit.Test

class OperatingModeTest {

    @Test
    fun storedValueMapsToMode() {
        assertEquals(OperatingMode.ALWAYS, OperatingMode.fromValue("always"))
        assertEquals(OperatingMode.WIFI_ONLY, OperatingMode.fromValue("wifi_only"))
        assertEquals(OperatingMode.APP_OPEN, OperatingMode.fromValue("app_open"))
    }

    @Test
    fun unknownValueFallsBackToDefault() {
        assertEquals(OperatingMode.DEFAULT, OperatingMode.fromValue(null))
        assertEquals(OperatingMode.DEFAULT, OperatingMode.fromValue("unknown"))
    }
}
