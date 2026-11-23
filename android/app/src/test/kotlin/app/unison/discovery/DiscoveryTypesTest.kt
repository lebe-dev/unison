package app.unison.discovery

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.net.Inet6Address
import java.net.InetAddress
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
        val records =
            Candidates.records(
                mapOf(
                    "" to "value",
                    "long" to "x".repeat(256),
                    "ok" to "value",
                ),
            )

        assertEquals(setOf("ok"), records.keys)
    }

    @Test
    fun recordsFitTheCombinedLimit() {
        val key = "k".repeat(10)
        val records =
            Candidates.records(
                mapOf(
                    key to "x".repeat(250),
                    "fits" to "x".repeat(250),
                    "a=b" to "value",
                    "ключ" to "value",
                ),
            )

        assertEquals(setOf("fits"), records.keys)
    }

    @Test
    fun attributeWithoutValueIsKept() {
        val attributes = HashMap<String, ByteArray?>()
        attributes["flag"] = null
        attributes["v"] = "1".toByteArray()

        val txt = MobileProto.CandidateTxt.parseFrom(Candidates.proto(attributes))

        assertEquals("", txt.recordsMap.getValue("flag"))
        assertEquals("1", txt.recordsMap.getValue("v"))
    }

    @Test
    fun attributesTravelToTheCore() {
        val bytes =
            Candidates.proto(
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
    fun everyDialableAddressIsReported() {
        val addresses =
            Candidates.addresses(
                listOf(
                    Inet6Address.getByAddress(null, ipv6("2001:db8::7"), 0),
                    InetAddress.getByName("10.0.0.7"),
                    InetAddress.getByName("127.0.0.1"),
                    InetAddress.getByName("192.168.1.5"),
                ),
            )

        assertEquals(3, addresses.size)
        assertEquals("10.0.0.7", addresses[0].hostAddress)
        assertEquals("192.168.1.5", addresses[1].hostAddress)
        assertTrue(addresses[2] is Inet6Address)
    }

    @Test
    fun linkLocalAddressIsSkippedNextToARoutableOne() {
        // The core resolves the zone of a link local address on every packet,
        // which costs a refused system call on Android, so it is the last resort.
        val addresses =
            Candidates.addresses(
                listOf(
                    Inet6Address.getByAddress(null, ipv6("fe80::1"), 2),
                    InetAddress.getByName("10.0.0.7"),
                ),
            )

        assertEquals("10.0.0.7", addresses.single().hostAddress)
    }

    @Test
    fun linkLocalAddressIsKeptAlone() {
        val addresses = Candidates.addresses(listOf(Inet6Address.getByAddress(null, ipv6("fe80::1"), 2)))

        assertTrue(addresses.single().isLinkLocalAddress)
    }

    @Test
    fun ownAddressIsSkipped() {
        // The record of an earlier run of this app may still sit in the system
        // cache under another instance name; its address is this device.
        val addresses =
            Candidates.addresses(
                listOf(InetAddress.getByName("192.168.1.5"), InetAddress.getByName("192.168.1.9")),
                own = setOf(InetAddress.getByName("192.168.1.5")),
            )

        assertEquals("192.168.1.9", addresses.single().hostAddress)
    }

    @Test
    fun ownInstanceIsRecognized() {
        assertTrue(Candidates.isOwn("4X7RUGTFV5IGE", listOf("4x7rugtfv5ige")))
        assertFalse(Candidates.isOwn("other", listOf("4x7rugtfv5ige")))
        assertFalse(Candidates.isOwn("4x7rugtfv5ige", emptyList()))
    }

    @Test
    fun unusableHostsAreSkipped() {
        val unusable = listOf(null, InetAddress.getByName("127.0.0.1"))
        assertTrue(Candidates.addresses(unusable).isEmpty())
        assertTrue(Candidates.addresses(emptyList()).isEmpty())

        val host = Candidates.addresses(listOf(null, InetAddress.getByName("192.168.1.5")))
        assertEquals("192.168.1.5", host.single().hostAddress)
    }

    private fun ipv6(literal: String): ByteArray = InetAddress.getByName(literal).address
}
