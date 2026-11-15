package app.unison.ui.state

import androidx.annotation.StringRes
import app.unison.R
import unison.local.v1.Local

/** Failure of the core that the screens report. */
enum class CoreFailure(@StringRes val message: Int) {
    /** The core is not running. */
    STOPPED(R.string.error_core_stopped),

    /** The core did not answer. */
    UNAVAILABLE(R.string.error_core_unavailable),
}

/** Snapshot of the core that the screens render. */
data class CoreState(
    val loading: Boolean = true,
    val status: Local.GetStatusResponse? = null,
    val failure: CoreFailure? = null,
) {

    /** Paired devices of [role], the online ones first. */
    fun devices(role: Local.Role): List<Local.Device> =
        status?.devicesList.orEmpty()
            .filter { !it.revoked && it.role == role }
            .sortedWith(compareByDescending<Local.Device> { it.online }.thenBy { it.name.lowercase() })
}
