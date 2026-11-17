package app.unison.network

import unison.mobile.v1.Mobile as MobileProto

/** Network conditions of the device, as the core and the service see them. */
data class NetworkConditions(
    /** A default network is available. */
    val online: Boolean = false,
    /** Traffic of the default network is billed by volume. */
    val metered: Boolean = false,
    /** The default network is a roaming cellular link. */
    val roaming: Boolean = false,
    /** The default network is a VPN. */
    val vpn: Boolean = false,
    /** The system data saver is on for sure, see [BackgroundData]. */
    val dataSaver: Boolean = false,
    /** A Wi-Fi network is available. */
    val wifi: Boolean = false,
    /** The default network is an Ethernet link. */
    val ethernet: Boolean = false,
) {
    /** A local network is available, so the Wi-Fi only mode may run the core. */
    val localNetwork: Boolean
        get() = wifi || ethernet

    /** The state in the form the core receives. */
    fun toProto(): MobileProto.NetworkState = MobileProto.NetworkState.newBuilder()
        .setOnline(online)
        .setMetered(metered)
        .setRoaming(roaming)
        .setVpn(vpn)
        .setDataSaver(dataSaver)
        .build()
}
