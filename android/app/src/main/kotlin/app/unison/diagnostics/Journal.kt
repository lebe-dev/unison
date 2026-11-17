package app.unison.diagnostics

import android.content.Context
import java.io.File
import java.time.Instant

/** Level of a record, as the line carries it. */
enum class JournalLevel {
    DEBUG,
    INFO,
    WARN,
    ERROR,
}

/** Records the settings page shows; an export always carries every level. */
enum class JournalDisplay {
    INFO,
    DEBUG,
    ;

    /** True when the page shows a record of [level]. */
    fun shows(level: JournalLevel): Boolean = this == DEBUG || level != JournalLevel.DEBUG
}

/** One record of a log file. */
data class JournalRecord(val at: Instant, val level: JournalLevel, val text: String)

/**
 * The application log of the app shell.
 *
 * The records go to `logs/app.log` of the data directory, in the same format as
 * the log of the core, and only while `logs/journal.on` marks the journal as
 * turned on. A failure to write never reaches the caller.
 */
object Journal {
    /** Directory of the record files inside the data directory. */
    const val DIRECTORY = "logs"

    @Volatile
    private var logs: File? = null

    /** Points the journal at the data directory of the app. */
    fun init(context: Context) {
        logs = File(context.filesDir, DIRECTORY)
    }

    /** True while the user keeps the journal on. */
    fun enabled(): Boolean = File(logs ?: return false, JournalFiles.MARKER).isFile

    /** Writes a detail that helps investigate a problem. */
    fun debug(message: String, vararg attrs: Pair<String, Any?>) = write(JournalLevel.DEBUG, message, attrs)

    /** Writes a fact about what the app does. */
    fun info(message: String, vararg attrs: Pair<String, Any?>) = write(JournalLevel.INFO, message, attrs)

    /** Writes a problem that the app recovers from. */
    fun warn(message: String, vararg attrs: Pair<String, Any?>) = write(JournalLevel.WARN, message, attrs)

    /** Writes a failure that stops a part of the app. */
    fun error(message: String, vararg attrs: Pair<String, Any?>) = write(JournalLevel.ERROR, message, attrs)

    /**
     * Records of the shell and the core, oldest first. The visible list reads
     * the current files only; the export takes the backups too.
     */
    fun records(context: Context, backups: Boolean = true): List<JournalRecord> {
        val directory = File(context.filesDir, DIRECTORY)
        return try {
            JournalFiles.records(
                app = File(directory, JournalFiles.APP_FILE),
                core = File(directory, JournalFiles.CORE_FILE),
                backups = backups,
            )
        } catch (_: Exception) {
            emptyList()
        }
    }

    @Synchronized
    private fun write(level: JournalLevel, message: String, attrs: Array<out Pair<String, Any?>>) {
        val directory = logs ?: return
        // The marker appears and disappears with the toggle, so the service does
        // not have to restart for the change to apply.
        if (!File(directory, JournalFiles.MARKER).isFile) {
            return
        }
        try {
            JournalFiles.append(
                file = File(directory, JournalFiles.APP_FILE),
                line = JournalFormat.line(Instant.now(), level, message, attrs.toList()),
                limit = JournalFiles.APP_LIMIT,
                keep = JournalFiles.APP_BACKUPS,
            )
        } catch (_: Exception) {
            // A record that cannot be written must not break the app.
        }
    }
}
