package app.unison.ui.state

import unison.local.v1.Local

/**
 * A question that waits for the user: a clipboard item this device holds for
 * confirmation, a file offer, or an offer to introduce this device to another
 * one. It never carries the content of an item.
 */
data class PendingPrompt(
    /** Item ID for the clipboard, transfer ID for the files, session ID for the introduction. */
    val id: String,
    val kind: Kind,
    /** Name of the device that sent it, or of the mediator of an introduction. */
    val device: String,
    /** Kind of the clipboard item. */
    val clipKind: Local.Kind = Local.Kind.KIND_UNSPECIFIED,
    /** Names of the offered files. */
    val files: List<String> = emptyList(),
    /** Size of the item or of all the files in bytes. */
    val sizeBytes: Long = 0,
    /** Exchange of an introduction, set on [Kind.MEDIATED]. */
    val mediated: Local.MediatedPair? = null,
) {
    /** What the question is about. */
    enum class Kind {
        CLIPBOARD,
        FILES,
        MEDIATED,
    }

    companion object {
        /** Question about a held clipboard item. */
        fun clipboard(
            item: Local.ClipItem,
            device: String,
        ): PendingPrompt =
            PendingPrompt(
                id = item.id,
                kind = Kind.CLIPBOARD,
                device = device,
                clipKind = item.kind,
                sizeBytes = item.representationsList.sumOf { it.size },
            )

        /** Question about a file offer. */
        fun files(request: Local.ShareRequest): PendingPrompt =
            PendingPrompt(
                id = request.id,
                kind = Kind.FILES,
                device = request.deviceName,
                files = request.filesList.map { it.name }.filter { it.isNotEmpty() },
                sizeBytes = request.totalBytes,
            )

        /** Question about an offer to introduce this device to another one. */
        fun mediated(pair: Local.MediatedPair): PendingPrompt =
            PendingPrompt(
                id = pair.sessionId,
                kind = Kind.MEDIATED,
                device = pair.mediator.name,
                mediated = pair,
            )
    }
}
