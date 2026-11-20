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
    fun changesRequests(
        event: Local.Event,
        shown: Set<String>,
    ): Boolean =
        arrives(event) &&
            (event.transfer.state == Local.TransferState.TRANSFER_STATE_PENDING || event.transfer.id in shown)

    /** What the receive notification of an event shows. */
    enum class Receipt { NONE, PROGRESS, RECEIVED, FAILED, CANCELED }

    /**
     * Receive notification of an incoming transfer. Offers that wait for an
     * answer have their own notification, and a declined offer brought nothing.
     */
    fun receipt(event: Local.Event): Receipt {
        if (!arrives(event)) {
            return Receipt.NONE
        }
        return when (event.transfer.state) {
            Local.TransferState.TRANSFER_STATE_TRANSFERRING -> Receipt.PROGRESS
            Local.TransferState.TRANSFER_STATE_DONE -> Receipt.RECEIVED
            Local.TransferState.TRANSFER_STATE_FAILED -> Receipt.FAILED
            Local.TransferState.TRANSFER_STATE_CANCELED -> Receipt.CANCELED
            else -> Receipt.NONE
        }
    }

    /**
     * Keeps the receive notifications calm: the progress of a transfer is shown
     * at most once per interval, and its result once.
     */
    class Receipts(
        private val intervalMs: Long,
        private val finishedLimit: Int = FINISHED_LIMIT,
    ) {
        private val shownAt = HashMap<String, Long>()
        private val finished = LinkedHashSet<String>()

        /**
         * Receive notification to show for the event at [nowMs], or [Receipt.NONE]
         * when the event changes nothing the user should see now.
         */
        fun next(
            event: Local.Event,
            nowMs: Long,
        ): Receipt {
            val receipt = receipt(event)
            val id = event.transfer.id
            val due =
                when (receipt) {
                    Receipt.NONE -> false
                    Receipt.PROGRESS -> progressDue(id, nowMs)
                    else -> finish(id)
                }
            return if (due) receipt else Receipt.NONE
        }

        /** True when the progress of the transfer may be shown at [nowMs]. */
        fun progressDue(
            id: String,
            nowMs: Long,
        ): Boolean {
            if (id in finished) {
                return false
            }
            val last = shownAt[id]
            if (last != null && nowMs - last < intervalMs) {
                return false
            }
            shownAt[id] = nowMs
            return true
        }

        /** Transfers whose progress is shown and whose result is not. */
        fun running(): Set<String> = shownAt.keys.toSet()

        /** True the first time the transfer ends, when its result is still to show. */
        fun finish(id: String): Boolean {
            shownAt.remove(id)
            if (!finished.add(id)) {
                return false
            }
            if (finished.size > finishedLimit) {
                finished.remove(finished.first())
            }
            return true
        }
    }

    private const val FINISHED_LIMIT = 256
}
