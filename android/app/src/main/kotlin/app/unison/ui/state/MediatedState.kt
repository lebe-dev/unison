package app.unison.ui.state

import unison.local.v1.Local

/** Snapshot of the introductions through a trusted device. */
data class MediatedState(
    /** Introductions this device runs or waits on, newest last. */
    val pairs: List<Local.MediatedPair> = emptyList(),
    /** True once the list was read, so an empty one can be explained. */
    val loaded: Boolean = false,
    /** A start of an introduction is in flight. */
    val starting: Boolean = false,
    /** Sessions whose answer is in flight. */
    val answering: Set<String> = emptySet(),
    /** Sessions whose cancellation is in flight. */
    val canceling: Set<String> = emptySet(),
    val failure: CoreFailure? = null,
) {
    /** Introductions this device runs for two of its devices. */
    val outgoing: List<Local.MediatedPair>
        get() =
            pairs.filter {
                it.direction == Local.MediatedPairDirection.MEDIATED_PAIR_DIRECTION_MEDIATOR
            }

    /** Introductions another device offers to this one. */
    val incoming: List<Local.MediatedPair>
        get() =
            pairs.filter {
                it.direction == Local.MediatedPairDirection.MEDIATED_PAIR_DIRECTION_INCOMING
            }

    /** Offers that still wait for an answer, newest last. */
    val offers: List<Local.MediatedPair>
        get() = incoming.filter { it.state == Local.MediatedPairState.MEDIATED_PAIR_STATE_WAITING }

    /** Number of offers that wait for an answer. */
    val pending: Int
        get() = offers.size

    /** The exchange with [sessionId], null when it left the list. */
    fun exchange(sessionId: String?): Local.MediatedPair? = sessionId?.let { id -> pairs.firstOrNull { it.sessionId == id } }
}
