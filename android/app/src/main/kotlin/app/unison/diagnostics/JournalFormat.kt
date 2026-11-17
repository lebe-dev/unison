package app.unison.diagnostics

import java.time.Instant
import java.time.ZoneOffset
import java.time.format.DateTimeFormatter
import java.util.Locale

/** Format of one record, the same as the journal of the core. */
internal object JournalFormat {
    private val TIME: DateTimeFormatter =
        DateTimeFormatter.ofPattern("yyyy-MM-dd'T'HH:mm:ss.SSS'Z'", Locale.ROOT)
            .withZone(ZoneOffset.UTC)

    private val LINE = Regex(
        """^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z) (DEBUG|INFO|WARN|ERROR) (.*)$""",
    )

    /** Timestamp of a record, always in UTC. */
    fun time(at: Instant): String = TIME.format(at)

    /** One line of a record with its final newline. */
    fun line(
        at: Instant,
        level: JournalLevel,
        message: String,
        attrs: List<Pair<String, Any?>> = emptyList(),
    ): String = buildString {
        append(time(at))
        append(' ')
        append(level.name)
        append(' ')
        append(text(redactPaths(message)))
        for ((key, value) in attrs) {
            append(' ')
            append(key)
            append('=')
            append(valueText(redactPaths(valueOf(value))))
        }
        append('\n')
    }

    /** A record of a line, null when the line is not a record of this format. */
    fun parse(line: String): JournalRecord? {
        val match = LINE.matchEntire(line.trimEnd('\n')) ?: return null
        val at = try {
            Instant.parse(match.groupValues[1])
        } catch (_: Exception) {
            return null
        }
        val level = JournalLevel.entries.firstOrNull { it.name == match.groupValues[2] } ?: return null
        return JournalRecord(at = at, level = level, text = line.trimEnd('\n'))
    }

    /** Text of a value, errors keep their message. */
    private fun valueOf(value: Any?): String = when (value) {
        null -> "nil"
        is Throwable -> value.message ?: value.javaClass.simpleName
        else -> value.toString()
    }

    /** The message as it is, quoted when it would break the line. */
    private fun text(message: String): String =
        if (message.any { it < ' ' || it.code == 0x7f }) quoted(message) else message

    /** A value, quoted when the line would be ambiguous without it. */
    private fun valueText(value: String): String {
        if (value.isEmpty()) {
            return "\"\""
        }
        val ambiguous = value.any { it <= ' ' || it == '"' || it == '=' || it.code == 0x7f }
        return if (ambiguous) quoted(value) else value
    }

    /** A text in double quotes with backslash escapes. */
    private fun quoted(value: String): String = buildString {
        append('"')
        for (c in value) {
            when (c) {
                '\\' -> append("\\\\")
                '"' -> append("\\\"")
                '\n' -> append("\\n")
                '\r' -> append("\\r")
                '\t' -> append("\\t")
                else -> if (c < ' ' || c.code == 0x7f) {
                    append("\\u")
                    append(c.code.toString(16).padStart(4, '0'))
                } else {
                    append(c)
                }
            }
        }
        append('"')
    }
}
