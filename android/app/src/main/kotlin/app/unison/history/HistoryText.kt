package app.unison.history

import app.unison.bridge.BridgeContract
import java.util.Locale
import unison.local.v1.Local

/** Formatting of a history entry that does not depend on Android. */
object HistoryText {
    /** Inline text of a text entry, shortened for the list. */
    fun text(item: Local.ClipItem, limit: Int = PREVIEW_LIMIT): String {
        val inline = item.representationsList.firstOrNull { it.mimeType == MIME_TEXT_PLAIN }
            ?: return ""
        return shorten(inline.inlineBytes.toStringUtf8(), limit)
    }

    /** Names of the files of an entry, shortened for the list. */
    fun fileNames(item: Local.ClipItem, limit: Int = PREVIEW_LIMIT): String {
        val names = item.representationsList.mapNotNull { it.fileName.ifBlank { null } }
        if (names.isEmpty()) {
            return ""
        }
        val shown = names.take(MAX_NAMES).joinToString(", ")
        val rest = names.size - MAX_NAMES
        val text = if (rest > 0) "$shown and $rest more" else shown
        return shorten(text, limit)
    }

    /** Size of every payload of an entry in bytes. */
    fun size(item: Local.ClipItem): Long = item.representationsList.sumOf { representation ->
        if (representation.size > 0) {
            representation.size
        } else {
            representation.inlineBytes.size().toLong()
        }
    }

    /** Human readable size with one unit. */
    fun formatSize(bytes: Long): String {
        if (bytes < KIB) {
            return "$bytes B"
        }
        var value = bytes.toDouble() / KIB
        var unit = 0
        while (value >= KIB && unit < UNITS.lastIndex) {
            value /= KIB
            unit++
        }
        if (value >= 10 || value == value.toLong().toDouble()) {
            return "${value.toLong()} ${UNITS[unit]}"
        }
        return String.format(Locale.US, "%.1f %s", value, UNITS[unit])
    }

    /** The rule is part of the bridge contract, so a keyboard can repeat it exactly. */
    private fun shorten(text: String, limit: Int): String = BridgeContract.preview(text, limit)

    /** Length of the preview in characters. */
    const val PREVIEW_LIMIT = BridgeContract.PREVIEW_LIMIT

    private const val MAX_NAMES = 2
    private const val KIB = 1024.0
    private const val MIME_TEXT_PLAIN = "text/plain"
    private val UNITS = listOf("KB", "MB", "GB", "TB")
}
