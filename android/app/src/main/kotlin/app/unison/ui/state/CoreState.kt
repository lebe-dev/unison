package app.unison.ui.state

import androidx.annotation.StringRes
import app.unison.R
import app.unison.diagnostics.Journal
import unison.local.v1.Local

/** Failure of the core that the screens report. */
enum class CoreFailure(
    @StringRes val message: Int,
) {
    /** The core is not running. */
    STOPPED(R.string.error_core_stopped),

    /** The core did not answer. */
    UNAVAILABLE(R.string.error_core_unavailable),
}

/** Snapshot of the core that the screens render. */
data class CoreState(
    val loading: Boolean = true,
    val status: Local.GetStatusResponse? = null,
    /** Settings of the core, absent until the first load. */
    val settings: Local.Settings? = null,
    /** Relay access of the group, absent until the first load. */
    val relay: Local.Relay? = null,
    /** A relay check runs. */
    val relayChecking: Boolean = false,
    /** Outcome of the last relay check, absent until one finishes. */
    val relayCheck: Local.TestRelayResponse? = null,
    val failure: CoreFailure? = null,
) {
    /** This device asks before it applies the clipboard of any device. */
    val requiresConfirmation: Boolean
        get() = settings?.requireConfirmation == true

    /** This device asks before it accepts files from any device. */
    val requiresFileConfirmation: Boolean
        get() = settings?.requireFileConfirmation == true

    /** Paired devices may change the confirmation flags of this device. */
    val allowsRemoteSettings: Boolean
        get() = settings?.allowRemoteSettings ?: true

    /** This device reports errors to the developer. */
    val reportsErrors: Boolean
        get() = settings?.sentryEnabled == true

    /** This device removes the tracking parameters from the links. */
    val stripTracking: Boolean
        get() = settings?.stripTracking ?: true

    /** The user turned on the application log, from the marker while the settings are absent. */
    val journalEnabled: Boolean
        get() = settings?.journalEnabled ?: Journal.enabled()

    /** Paired devices of [role], the online ones first. */
    fun devices(role: Local.Role): List<Local.Device> =
        status
            ?.devicesList
            .orEmpty()
            .filter { !it.revoked && it.role == role }
            .sortedWith(compareByDescending<Local.Device> { it.online }.thenBy { it.name.lowercase() })
}
