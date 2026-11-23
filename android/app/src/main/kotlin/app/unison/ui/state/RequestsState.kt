package app.unison.ui.state

import unison.local.v1.Local

/** Snapshot of the file offers that wait for an answer. */
data class RequestsState(
    val loading: Boolean = false,
    /** True once the list was loaded, so an empty list can be explained. */
    val loaded: Boolean = false,
    /** Offers of Friends, newest first. */
    val requests: List<Local.ShareRequest> = emptyList(),
    val failure: CoreFailure? = null,
    /** Offers with an answer in flight. */
    val answering: Set<String> = emptySet(),
) {
    /** Number of offers that wait for an answer. */
    val pending: Int
        get() = requests.size

    /** True when the list was loaded and holds no offers. */
    val isEmpty: Boolean
        get() = loaded && requests.isEmpty()
}
