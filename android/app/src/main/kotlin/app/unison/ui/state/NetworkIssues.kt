package app.unison.ui.state

import unison.local.v1.Local

/**
 * Paired devices that do not answer. The screens show them in two levels: a
 * short message in plain words, and technical details on request. The details
 * quote the core and are not translated.
 */
object NetworkIssues {
    /** Whether a paired device is offline and does not answer. */
    fun isUnreachable(device: Local.Device): Boolean = device.unreachable && !device.online

    /**
     * Technical details of a device that does not answer: when it stopped
     * answering, described by since, and the addresses with their failures.
     */
    fun deviceDetails(device: Local.Device, since: (Long) -> String): String =
        buildList {
            if (device.unreachableSinceUnixMs > 0) {
                add(since(device.unreachableSinceUnixMs))
            }
            if (device.unreachableDetail.isNotEmpty()) {
                add(device.unreachableDetail)
            }
        }.joinToString("\n")
}
