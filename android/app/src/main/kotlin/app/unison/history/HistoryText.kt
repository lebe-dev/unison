package app.unison.history

import app.unison.bridge.BridgeContract
import unison.local.v1.Local

/** First file names of an entry with the number of names that did not fit. */
data class FileNames(
    val shown: String,
    val rest: Int,
)

/** Formatting of a history entry that does not depend on Android. */
object HistoryText {
    /** Inline text of a text entry, shortened for the list. */
    fun text(
        item: Local.ClipItem,
        limit: Int = PREVIEW_LIMIT,
    ): String {
        val inline =
            item.representationsList.firstOrNull { it.mimeType == MIME_TEXT_PLAIN }
                ?: return ""
        return shorten(inline.inlineBytes.toStringUtf8(), limit)
    }

    /** Whole inline text of a text entry, without shortening. */
    fun fullText(item: Local.ClipItem): String =
        item.representationsList
            .firstOrNull { it.mimeType == MIME_TEXT_PLAIN }
            ?.inlineBytes
            ?.toStringUtf8()
            .orEmpty()

    /** First names of the files of an entry. */
    fun fileNames(item: Local.ClipItem): FileNames {
        val names = item.representationsList.mapNotNull { it.fileName.ifBlank { null } }
        if (names.isEmpty()) {
            return FileNames("", 0)
        }
        return FileNames(
            names.take(MAX_NAMES).joinToString(", "),
            (names.size - MAX_NAMES).coerceAtLeast(0),
        )
    }

    /** Shortens a summary to the preview the bridge carries. */
    fun preview(
        text: String,
        limit: Int = PREVIEW_LIMIT,
    ): String = shorten(text, limit)

    /** Size of every payload of an entry in bytes. */
    fun size(item: Local.ClipItem): Long =
        item.representationsList.sumOf { representation ->
            if (representation.size > 0) {
                representation.size
            } else {
                representation.inlineBytes.size().toLong()
            }
        }

    /** Human readable size with one unit. */
    fun formatSize(
        bytes: Long,
        units: SizeUnits = SizeUnits.ENGLISH,
    ): String {
        if (bytes < KIB) {
            return "$bytes ${units.bytes}"
        }
        var value = bytes.toDouble() / KIB
        var unit = 0
        while (value >= KIB && unit < units.names.lastIndex) {
            value /= KIB
            unit++
        }
        if (value >= 10 || value == value.toLong().toDouble()) {
            return "${value.toLong()} ${units.names[unit]}"
        }
        return String.format(units.locale, "%.1f %s", value, units.names[unit])
    }

    /** The rule is part of the bridge contract, so a keyboard can repeat it exactly. */
    private fun shorten(
        text: String,
        limit: Int,
    ): String = BridgeContract.preview(text, limit)

    /** Length of the preview in characters. */
    const val PREVIEW_LIMIT = BridgeContract.PREVIEW_LIMIT

    private const val MAX_NAMES = 2
    private const val KIB = 1024.0
    private const val MIME_TEXT_PLAIN = "text/plain"
}
