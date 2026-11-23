package app.unison.ui.state

import unison.local.v1.Local

/**
 * A question that waits for the user: a clipboard item this device holds for
 * confirmation, or a file offer. It never carries the content of an item.
 */
data class PendingPrompt(
    /** Item ID for the clipboard, transfer ID for the files. */
    val id: String,
    val kind: Kind,
    /** Name of the device that sent it. */
    val device: String,
    /** Kind of the clipboard item. */
    val clipKind: Local.Kind = Local.Kind.KIND_UNSPECIFIED,
    /** Names of the offered files. */
    val files: List<String> = emptyList(),
    /** Size of the item or of all the files in bytes. */
    val sizeBytes: Long = 0,
) {
    /** What the question is about. */
    enum class Kind {
        CLIPBOARD,
        FILES,
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
    }
}
