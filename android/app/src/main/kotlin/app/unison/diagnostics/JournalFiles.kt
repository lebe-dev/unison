package app.unison.diagnostics

import java.io.File
import java.io.FileOutputStream

/** Reading, appending and rotating the record files of the data directory. */
internal object JournalFiles {
    /** File that marks the journal as turned on. */
    const val MARKER = "journal.on"

    /** File the app shell writes. */
    const val APP_FILE = "app.log"

    /** File the core writes. */
    const val CORE_FILE = "journal.log"

    /** Size a file of the shell reaches before it rotates. */
    const val APP_LIMIT = 2L shl 20

    /** Rotated files of the shell kept next to the current one. */
    const val APP_BACKUPS = 2

    /** Rotated files of the core kept next to the current one. */
    const val CORE_BACKUPS = 3

    /** Appends one line, rotating the file first when it would grow past the limit. */
    @Synchronized
    fun append(file: File, line: String, limit: Long, keep: Int) {
        val directory = file.parentFile
        if (directory != null && !directory.isDirectory && !directory.mkdirs()) {
            throw IllegalStateException("cannot create the log directory")
        }
        val bytes = line.toByteArray(Charsets.UTF_8)
        if (limit > 0 && keep > 0 && file.length() > 0 && file.length() + bytes.size > limit) {
            rotate(file, keep)
        }
        FileOutputStream(file, true).use { it.write(bytes) }
    }

    /** Records of one file and its rotated backups, oldest first. */
    fun read(file: File, keep: Int): List<JournalRecord> {
        val records = mutableListOf<JournalRecord>()
        for (index in keep downTo 1) {
            records += parse(File(file.path + "." + index))
        }
        records += parse(file)
        return records
    }

    /** Records of the shell and the core logs with their backups, oldest first. */
    fun records(
        app: File,
        core: File,
        backups: Boolean = true,
    ): List<JournalRecord> =
        (read(app, if (backups) APP_BACKUPS else 0) + read(core, if (backups) CORE_BACKUPS else 0))
            .sortedWith(compareBy { it.at })

    /** Records of one file, empty when it does not exist. */
    private fun parse(file: File): List<JournalRecord> {
        if (!file.isFile) {
            return emptyList()
        }
        return file.readLines()
            .mapNotNull(JournalFormat::parse)
    }

    /** Moves the file and its backups up by one and starts an empty file. */
    private fun rotate(file: File, keep: Int) {
        File(file.path + "." + keep).delete()
        for (index in keep - 1 downTo 1) {
            val from = File(file.path + "." + index)
            if (from.isFile) {
                from.renameTo(File(file.path + "." + (index + 1)))
            }
        }
        // A failed rename keeps the file, which then grows until the next try.
        if (file.isFile && !file.renameTo(File(file.path + ".1"))) {
            return
        }
    }
}
