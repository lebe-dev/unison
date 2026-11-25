package app.unison.ui.state

import unison.local.v1.Local

/** Snapshot of the dashboard that the screen renders. */
data class DashboardState(
    /** Number of newest items each tab keeps. */
    val limit: Int = DashboardLimit.DEFAULT,
    val loading: Boolean = false,
    /** True once the first load finished, so empty tabs can be explained. */
    val loaded: Boolean = false,
    val images: List<Local.HistoryItem> = emptyList(),
    val files: List<Local.HistoryItem> = emptyList(),
    val text: List<Local.HistoryItem> = emptyList(),
    val failure: CoreFailure? = null,
) {
    /** True when every tab was loaded and holds no entries. */
    val isEmpty: Boolean
        get() = loaded && images.isEmpty() && files.isEmpty() && text.isEmpty()
}
