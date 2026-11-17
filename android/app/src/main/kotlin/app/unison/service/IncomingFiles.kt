package app.unison.service

import unison.local.v1.Local

/** Files that peers send to this device. */
object IncomingFiles {

    /** True when the event reports a file transfer towards this device. */
    fun arrives(event: Local.Event): Boolean =
        event.kind == Local.EventKind.EVENT_KIND_TRANSFER &&
            event.transfer.direction == Local.TransferDirection.TRANSFER_DIRECTION_INCOMING
}
