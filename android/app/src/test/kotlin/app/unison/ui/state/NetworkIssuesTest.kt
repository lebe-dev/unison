package app.unison.ui.state

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import unison.local.v1.Local

class NetworkIssuesTest {

    @Test
    fun unreachableDeviceNeedsTheFlagAndAnOfflineDevice() {
        val device = Local.Device.newBuilder().setUnreachable(true).build()
        assertTrue(NetworkIssues.isUnreachable(device))
        assertFalse(NetworkIssues.isUnreachable(device.toBuilder().setOnline(true).build()))
        assertFalse(NetworkIssues.isUnreachable(Local.Device.getDefaultInstance()))
    }

    @Test
    fun deviceDetailsStartWithTheTimeAndListTheAddresses() {
        val device = Local.Device.newBuilder()
            .setUnreachable(true)
            .setUnreachableSinceUnixMs(1_700_000_000_000)
            .setUnreachableDetail("192.168.1.2:4000: timeout\n100.64.0.2:4000: timeout")
            .build()

        val details = NetworkIssues.deviceDetails(device) { "Not answering since 13:05." }

        assertEquals("Not answering since 13:05.\n192.168.1.2:4000: timeout\n100.64.0.2:4000: timeout", details)
    }

    @Test
    fun deviceDetailsSkipAMissingTime() {
        val device = Local.Device.newBuilder().setUnreachable(true).setUnreachableDetail("a: timeout").build()

        assertEquals("a: timeout", NetworkIssues.deviceDetails(device) { "never" })
    }
}
