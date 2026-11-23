package app.unison.core

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import unison.local.v1.Local

class LocalAddressesTest {
    @Test
    fun addressesGetThePortOfTheListener() {
        assertEquals(
            listOf("192.168.1.5:41234", "10.0.0.2:41234"),
            qrCodeAddresses("0.0.0.0:41234", listOf("192.168.1.5", "10.0.0.2")),
        )
    }

    @Test
    fun addressListWithoutPortGivesNoCodeAddresses() {
        assertTrue(qrCodeAddresses("0.0.0.0", listOf("192.168.1.5")).isEmpty())
        assertTrue(qrCodeAddresses("", listOf("192.168.1.5")).isEmpty())
    }

    @Test
    fun duplicatesOfTheDeviceAreDropped() {
        assertEquals(
            listOf("192.168.1.5:41234"),
            qrCodeAddresses("0.0.0.0:41234", listOf("192.168.1.5", "192.168.1.5")),
        )
    }

    @Test
    fun ownServiceIsNotACandidate() {
        val candidates =
            listOf(
                candidate("192.168.1.5:41234"),
                candidate("192.168.1.7:41234"),
            )

        assertEquals(
            listOf("192.168.1.7:41234"),
            withoutOwnService(candidates, "0.0.0.0:41234", listOf("192.168.1.5")).map { it.addr },
        )
    }

    @Test
    fun candidatesStayWithoutAddresses() {
        val candidates = listOf(candidate("192.168.1.7:41234"))

        assertEquals(candidates, withoutOwnService(candidates, "0.0.0.0:41234", emptyList()))
    }

    private companion object {
        fun candidate(addr: String): Local.PairingCandidate =
            Local.PairingCandidate
                .newBuilder()
                .setAddr(addr)
                .build()
    }
}
