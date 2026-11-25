package app.unison.history

import unison.local.v1.Local

/** Seconds in one minute, hour and day. */
private const val MINUTE = 60L
private const val HOUR = 60 * MINUTE
private const val DAY = 24 * HOUR

/** Lifetimes of the history items, in seconds. */
data class HistoryTtl(
    val text: Long,
    val image: Long,
    val files: Long,
) {
    /** Lifetime of one kind. */
    fun of(kind: Local.Kind): Long =
        when (kind) {
            Local.Kind.KIND_IMAGE -> image
            Local.Kind.KIND_FILES -> files
            else -> text
        }

    /** Copy with another lifetime of one kind. */
    fun with(
        kind: Local.Kind,
        seconds: Long,
    ): HistoryTtl =
        when (kind) {
            Local.Kind.KIND_IMAGE -> copy(image = seconds)
            Local.Kind.KIND_FILES -> copy(files = seconds)
            else -> copy(text = seconds)
        }
}

/** Defaults and limits of the lifetimes. */
object HistoryTtlDefaults {
    /** Lifetime of a text item. */
    const val TEXT = DAY

    /** Lifetime of an image item. */
    const val IMAGE = 7 * DAY

    /** Lifetime of a file item. */
    const val FILES = 7 * DAY

    /** Longest lifetime the core accepts. */
    const val MAX = 365 * DAY

    /** Lifetimes of a device without history settings. */
    val values: HistoryTtl = HistoryTtl(TEXT, IMAGE, FILES)
}

/** Text form of a lifetime. */
object HistoryTtlText {
    /** Renders seconds as "7d", "24h" or "90m". */
    fun format(seconds: Long): String {
        if (seconds <= 0) {
            return ""
        }
        return when {
            seconds % DAY == 0L -> "${seconds / DAY}d"
            seconds % HOUR == 0L -> "${seconds / HOUR}h"
            seconds % MINUTE == 0L -> "${seconds / MINUTE}m"
            else -> "${seconds}s"
        }
    }

    /** Seconds of a value like "7d", "24h" or "90m", null when it is not usable. */
    fun parse(value: String): Long? {
        val text = value.trim().lowercase()
        for ((suffix, unit) in units) {
            if (!text.endsWith(suffix)) {
                continue
            }
            val count = text.dropLast(suffix.length).toLongOrNull() ?: return null
            if (count <= 0 || count > HistoryTtlDefaults.MAX / unit) {
                return null
            }
            return count * unit
        }
        return null
    }

    /** Units of a lifetime, longest first. */
    private val units = listOf("d" to DAY, "h" to HOUR, "m" to MINUTE, "s" to 1L)
}
