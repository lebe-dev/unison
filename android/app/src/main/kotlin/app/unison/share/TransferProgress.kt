package app.unison.share

import unison.local.v1.Local

/** Progress of the outgoing file transfers. */
object TransferProgress {

    /** Outgoing transfers that are still running. */
    fun active(transfers: List<Local.Transfer>): List<Local.Transfer> = transfers.filter { transfer ->
        transfer.direction == Local.TransferDirection.TRANSFER_DIRECTION_OUTGOING &&
            when (transfer.state) {
                Local.TransferState.TRANSFER_STATE_OFFERED,
                Local.TransferState.TRANSFER_STATE_TRANSFERRING,
                -> true
                else -> false
            }
    }

    /** Share of the payload that arrived, 0..100, zero when the size is unknown. */
    fun percent(transfers: List<Local.Transfer>): Int {
        val total = totalBytes(transfers)
        if (total <= 0) {
            return 0
        }
        return ((bytes(transfers) * 100) / total).coerceIn(0, 100).toInt()
    }

    /** Bytes of the payload that arrived. */
    fun bytes(transfers: List<Local.Transfer>): Long = transfers.sumOf { it.bytes }

    /** Size of the whole payload in bytes. */
    fun totalBytes(transfers: List<Local.Transfer>): Long = transfers.sumOf { it.totalBytes }

    /** Names of the peers, without repeats, sorted for a stable text. */
    fun names(transfers: List<Local.Transfer>): List<String> = transfers
        .map { if (it.deviceName.isBlank()) it.deviceId else it.deviceName }
        .filter { it.isNotEmpty() }
        .distinct()
        .sorted()
}
