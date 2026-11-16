package app.unison.ui.state

import unison.local.v1.Local

/** Snapshot of the history that the screens render. */
data class HistoryState(
    val loading: Boolean = false,
    /** True once the first list was loaded, so an empty list can be explained. */
    val loaded: Boolean = false,
    val items: List<Local.HistoryItem> = emptyList(),
    val failure: CoreFailure? = null,
    /** Kind filter, KIND_UNSPECIFIED lists every kind. */
    val kind: Local.Kind = Local.Kind.KIND_UNSPECIFIED,
    /** Device filter as a lowercase hex device ID, empty lists every device. */
    val deviceId: String = "",
    /** Entries with a running action, keyed by item ID. */
    val busy: Set<String> = emptySet(),
) {
    /** True when the list was loaded and holds no entries. */
    val isEmpty: Boolean
        get() = loaded && items.isEmpty()
}
