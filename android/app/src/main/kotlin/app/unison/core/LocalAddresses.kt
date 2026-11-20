package app.unison.core

import android.content.Context
import android.net.ConnectivityManager
import unison.local.v1.Local
import java.net.Inet4Address

/** Addresses of this device that another one on the same network can reach. */
class LocalAddresses(
    private val context: Context,
) {
    /** IPv4 addresses of the current networks. */
    fun ipv4(): List<String> {
        val manager = context.getSystemService(ConnectivityManager::class.java) ?: return emptyList()
        val addresses = mutableListOf<String>()
        for (network in manager.allNetworks) {
            val link = manager.getLinkProperties(network) ?: continue
            for (entry in link.linkAddresses) {
                val address = entry.address
                if (address !is Inet4Address || address.isLoopbackAddress || address.isLinkLocalAddress) {
                    continue
                }
                address.hostAddress?.let(addresses::add)
            }
        }
        return addresses.distinct().take(MAX_ADDRESSES)
    }
}

/**
 * Addresses of a pairing code: the addresses of this device with the port of
 * the pairing listener. An empty result lets the core list the interfaces.
 */
internal fun qrCodeAddresses(
    listenerAddr: String,
    ipv4: List<String>,
): List<String> {
    val port = listenerAddr.substringAfterLast(':', "")
    if (port.toIntOrNull() == null) {
        return emptyList()
    }
    return ipv4.map { "$it:$port" }.distinct().take(MAX_ADDRESSES)
}

/**
 * Drops the candidate that is the pairing listener of this device. The local
 * network may report the own service, which cannot pair with itself.
 */
internal fun withoutOwnService(
    candidates: List<Local.PairingCandidate>,
    windowAddr: String,
    ipv4: List<String>,
): List<Local.PairingCandidate> {
    val own = qrCodeAddresses(windowAddr, ipv4).toSet()
    if (own.isEmpty()) {
        return candidates
    }
    return candidates.filter { it.addr !in own }
}

/** Addresses of one pairing code, the same limit as the core. */
private const val MAX_ADDRESSES = 4
