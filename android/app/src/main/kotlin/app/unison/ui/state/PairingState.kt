package app.unison.ui.state

import androidx.annotation.StringRes
import unison.local.v1.Local

/** Pairing window and exchanges that the add screen renders. */
data class PairingState(
    /** Role the next window proposes to the devices it pairs with. */
    val role: Local.Role = Local.Role.ROLE_PERSONAL,
    /** Open window, null while pairing is off. */
    val window: Local.PairingWindow? = null,
    /** Code of the open window for another device to scan. */
    val qr: Local.PairingQr? = null,
    /** Devices that are open for pairing. */
    val candidates: List<Local.PairingCandidate> = emptyList(),
    /** Device that asked to pair and waits for the answer. */
    val request: PairingRequest? = null,
    /** Address of the candidate an exchange was started with. */
    val dialing: String = "",
    /** A scanned code waits for the other device to confirm. */
    val scanning: Boolean = false,
    /** A call of the core is in flight. */
    val busy: Boolean = false,
    /** Failure of the last call, as a string resource. */
    @StringRes val failure: Int? = null,
    /** Outcome of the last exchange, absent until it ends. */
    val result: PairingResult? = null,
)

/** A device that asks to pair. */
data class PairingRequest(
    val deviceId: String,
    val name: String,
    val role: Local.Role,
    val platform: Local.Platform,
    /** Short authentication string, set on a compared code. */
    val sas: Local.Sas?,
    val method: Local.PairingMethod,
)

/** Outcome of a pairing exchange. */
data class PairingResult(
    val paired: Boolean,
    /** Name of the other device, empty when it did not report one. */
    val name: String = "",
    /** Reason reported by the core, empty when it reported none. */
    val error: String = "",
    /** The other device was refused by the user. */
    val rejected: Boolean = false,
    /** The other device did not answer on any address; error holds the details. */
    val unreachable: Boolean = false,
)
