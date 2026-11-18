package app.unison.service

import unison.local.v1.Local

/** Files that peers send to this device. */
object IncomingFiles {

    /** True when the event reports a file transfer towards this device. */
    fun arrives(event: Local.Event): Boolean =
        event.kind == Local.EventKind.EVENT_KIND_TRANSFER &&
            event.transfer.direction == Local.TransferDirection.TRANSFER_DIRECTION_INCOMING

    /** True when the event reports a file transfer from this device. */
    fun leaves(event: Local.Event): Boolean =
        event.kind == Local.EventKind.EVENT_KIND_TRANSFER &&
            event.transfer.direction == Local.TransferDirection.TRANSFER_DIRECTION_OUTGOING

    /**
     * True when the event may change the offers that wait for an answer: a new
     * offer, or any change of an offer in [shown]. Progress of an accepted
     * transfer leaves the offers as they are.
     */
    fun changesRequests(event: Local.Event, shown: Set<String>): Boolean =
        arrives(event) &&
            (event.transfer.state == Local.TransferState.TRANSFER_STATE_PENDING || event.transfer.id in shown)
}
