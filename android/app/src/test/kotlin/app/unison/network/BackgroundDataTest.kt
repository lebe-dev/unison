package app.unison.network

import android.net.ConnectivityManager
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class BackgroundDataTest {
    @Test
    fun unrestrictedAppIsNeitherSaverNorRestricted() {
        val status = ConnectivityManager.RESTRICT_BACKGROUND_STATUS_DISABLED

        assertFalse(BackgroundData.dataSaver(status))
        assertFalse(BackgroundData.restricted(status))
    }

    @Test
    fun allowedAppUnderDataSaverReportsDataSaver() {
        val status = ConnectivityManager.RESTRICT_BACKGROUND_STATUS_WHITELISTED

        assertTrue(BackgroundData.dataSaver(status))
        assertFalse(BackgroundData.restricted(status))
    }

    @Test
    fun restrictedAppIsNotTakenForDataSaver() {
        // The status is the same for the system data saver and for an app whose
        // background data the user turned off, so it says only that the app is
        // restricted.
        val status = ConnectivityManager.RESTRICT_BACKGROUND_STATUS_ENABLED

        assertFalse(BackgroundData.dataSaver(status))
        assertTrue(BackgroundData.restricted(status))
    }
}
