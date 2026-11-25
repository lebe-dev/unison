package app.unison.ui.state

import unison.local.v1.Local

/** Rules of the introductions through a trusted device. */
object Introductions {
    /** The device can be introduced: it is paired, online and speaks the feature. */
    fun canIntroduce(device: Local.Device): Boolean =
        device.deviceId.isNotEmpty() && !device.revoked && device.online && device.supportsMediatedPair

    /**
     * Roles the new pair can take. A chain with a Friends device never grants
     * more than Friends, so a Personal pair between Friends is not offered.
     */
    fun roles(
        peerA: Local.Device,
        peerB: Local.Device,
    ): List<Local.Role> =
        if (peerA.role == Local.Role.ROLE_FRIENDS || peerB.role == Local.Role.ROLE_FRIENDS) {
            listOf(Local.Role.ROLE_FRIENDS)
        } else {
            listOf(Local.Role.ROLE_PERSONAL, Local.Role.ROLE_FRIENDS)
        }

    /** Role offered first for the pair. */
    fun defaultRole(
        peerA: Local.Device,
        peerB: Local.Device,
    ): Local.Role = roles(peerA, peerB).first()
}
