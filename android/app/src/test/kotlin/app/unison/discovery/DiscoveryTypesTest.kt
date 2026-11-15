package app.unison.discovery

import java.net.Inet6Address
import java.net.InetAddress
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import unison.mobile.v1.Mobile as MobileProto

class ServiceTypesTest {

    @Test
    fun namesMapToTypes() {
        assertEquals("_unison._udp.", ServiceTypes.typeOf("main"))
        assertEquals("_unison-pair._udp.", ServiceTypes.typeOf("pairing"))
        assertNull(ServiceTypes.typeOf("unknown"))
        assertNull(ServiceTypes.typeOf(""))
    }
}

class CandidatesTest {

    @Test
    fun recordsKeepPlainValues() {
        val records = Candidates.records(mapOf("v" to "1", "tag" to "abcdefghijklm"))

        assertEquals("1", records.getValue("v"))
        assertEquals("abcdefghijklm", records.getValue("tag"))
    }

    @Test
    fun emptyAndOversizedRecordsAreDropped() {
        val records = Candidates.records(
            mapOf(
                "" to "value",
                "long" to "x".repeat(256),
                "ok" to "value",
            ),
        )

        assertEquals(setOf("ok"), records.keys)
    }

    @Test
    fun attributesTravelToTheCore() {
        val bytes = Candidates.proto(
            mapOf(
                "v" to "1".toByteArray(),
                "tag" to "abcdefghijklm".toByteArray(),
            ),
        )
        val txt = MobileProto.CandidateTxt.parseFrom(bytes)

        assertEquals("1", txt.recordsMap.getValue("v"))
        assertEquals("abcdefghijklm", txt.recordsMap.getValue("tag"))
    }

    @Test
    fun emptyAttributesGiveEmptyMetadata() {
        val txt = MobileProto.CandidateTxt.parseFrom(Candidates.proto(emptyMap()))

        assertEquals(0, txt.recordsCount)
    }

    @Test
    fun ipv4AddressHasHostAndPort() {
        val host = InetAddress.getByName("192.168.1.5")

        assertEquals("192.168.1.5:4242", Candidates.address(host, 4242))
    }

    @Test
    fun ipv6AddressIsBracketed() {
        val host = Inet6Address.getByAddress(null, ipv6("fe80::1"), 2)
        val addr = Candidates.address(host, 4242)

        assertEquals(true, addr?.startsWith("[fe80"))
        assertEquals(true, addr?.endsWith("%2]:4242"))
    }

    @Test
    fun unusableAddressesAreRejected() {
        assertNull(Candidates.address(null, 4242))
        assertNull(Candidates.address(InetAddress.getByName("127.0.0.1"), 4242))
        assertNull(Candidates.address(InetAddress.getByName("0.0.0.0"), 4242))
        assertNull(Candidates.address(InetAddress.getByName("192.168.1.5"), 0))
        assertNull(Candidates.address(InetAddress.getByName("192.168.1.5"), 70000))
    }

    @Test
    fun ipv4IsPreferred() {
        val host = Candidates.pick(
            listOf(
                Inet6Address.getByAddress(null, ipv6("fe80::1"), 2),
                InetAddress.getByName("10.0.0.7"),
            ),
        )

        assertEquals("10.0.0.7", host?.hostAddress)
    }

    @Test
    fun unusableHostsAreSkipped() {
        assertNull(Candidates.pick(listOf(null, InetAddress.getByName("127.0.0.1"))))
        assertNull(Candidates.pick(emptyList()))

        val host = Candidates.pick(listOf(null, InetAddress.getByName("192.168.1.5")))
        assertEquals("192.168.1.5", host?.hostAddress)
    }

    private fun ipv6(literal: String): ByteArray =
        InetAddress.getByName(literal).address
}
