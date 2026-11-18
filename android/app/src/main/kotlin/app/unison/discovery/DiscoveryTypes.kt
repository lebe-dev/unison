package app.unison.discovery

import java.net.Inet6Address
import java.net.InetAddress
import unison.mobile.v1.Mobile as MobileProto

/** Services the core publishes on the local network. */
object ServiceTypes {
    /** A running core that accepts connections. */
    const val MAIN = "main"

    /** A core that is open for pairing. */
    const val PAIRING = "pairing"

    private const val MAIN_TYPE = "_unison._udp."
    private const val PAIRING_TYPE = "_unison-pair._udp."

    /** DNS-SD type of the service, null for an unknown name. */
    fun typeOf(service: String): String? = when (service) {
        MAIN -> MAIN_TYPE
        PAIRING -> PAIRING_TYPE
        else -> null
    }
}

/** Converts between DNS-SD records and the candidate data of the core. */
object Candidates {
    /** Bound of the key and the value of one TXT record together, as NsdServiceInfo checks it. */
    private const val MAX_RECORD = 255

    /**
     * TXT records of an announcement that NsdServiceInfo accepts. One record it
     * refuses would make it refuse the whole announcement, so such a record is
     * dropped instead.
     */
    fun records(txt: Map<String, String>): Map<String, String> = buildMap {
        for ((key, value) in txt) {
            if (!validKey(key)) {
                continue
            }
            if (key.length + value.toByteArray(Charsets.UTF_8).size >= MAX_RECORD) {
                continue
            }
            put(key, value)
        }
    }

    /** Printable ASCII without '=', the keys NsdServiceInfo takes. */
    private fun validKey(key: String): Boolean =
        key.isNotEmpty() && key.all { it in ' '..'~' && it != '=' }

    /**
     * Serialized unison.mobile.v1.CandidateTxt of DNS-SD attributes. Any device
     * on the network writes these, and a key without a value comes as null.
     */
    fun proto(attributes: Map<String, ByteArray?>): ByteArray {
        val records = buildMap {
            for ((key, value) in attributes) {
                if (key.isEmpty()) {
                    continue
                }
                put(key, value?.toString(Charsets.UTF_8).orEmpty())
            }
        }
        return MobileProto.CandidateTxt.newBuilder()
            .putAllRecords(records)
            .build()
            .toByteArray()
    }

    /** Address of a candidate in "host:port" form, null when it is not dialable. */
    fun address(host: InetAddress?, port: Int): String? {
        if (host == null || !dialable(host) || port !in 1..65535) {
            return null
        }
        if (host !is Inet6Address) {
            return "${host.hostAddress}:$port"
        }
        val zone = host.scopedInterface?.name ?: host.scopeId.takeIf { it > 0 }?.toString()
        val literal = host.hostAddress?.substringBefore('%') ?: return null
        return if (zone == null) "[$literal]:$port" else "[$literal%$zone]:$port"
    }

    /**
     * Dialable addresses of a service, IPv4 first. A link local address is kept
     * only when the service has no other one: the core resolves its zone on every
     * packet, and Android refuses the interface list that needs.
     */
    fun addresses(hosts: Iterable<InetAddress?>): List<InetAddress> {
        val dialable = hosts
            .filterNotNull()
            .filter { dialable(it) }
            .sortedBy { if (it is Inet6Address) 1 else 0 }
        val routable = dialable.filterNot { it.isLinkLocalAddress }
        return routable.ifEmpty { dialable }
    }

    /** True when [instance] is one of the [own] announcements; DNS-SD names ignore case. */
    fun isOwn(instance: String, own: Iterable<String>): Boolean =
        own.any { it.equals(instance, ignoreCase = true) }

    private fun dialable(host: InetAddress): Boolean =
        !host.isAnyLocalAddress && !host.isLoopbackAddress && !host.isMulticastAddress
}
