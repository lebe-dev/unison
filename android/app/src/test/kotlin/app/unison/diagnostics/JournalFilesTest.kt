package app.unison.diagnostics

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import java.io.File
import java.time.Instant

class JournalFilesTest {
    @get:Rule
    val folder = TemporaryFolder()

    @Test
    fun recordsOfBothLogsAreMergedChronologically() {
        val app = File(folder.root, "app.log")
        val core = File(folder.root, "journal.log")
        append(app, "2026-11-17T19:48:02.100Z", JournalLevel.INFO, "service created")
        append(core, "2026-11-17T19:48:02.150Z", JournalLevel.DEBUG, "connected to the peer")
        append(app, "2026-11-17T19:48:02.200Z", JournalLevel.WARN, "network lost")

        val records = JournalFiles.records(app, core)

        assertEquals(
            listOf(
                "2026-11-17T19:48:02.100Z INFO service created",
                "2026-11-17T19:48:02.150Z DEBUG connected to the peer",
                "2026-11-17T19:48:02.200Z WARN network lost",
            ),
            records.map { it.text },
        )
    }

    @Test
    fun backupsOfBothLogsAreReadToo() {
        val app = File(folder.root, "app.log")
        val core = File(folder.root, "journal.log")
        append(app, "2026-11-17T19:48:01.000Z", JournalLevel.INFO, "app first")
        append(app, "2026-11-17T19:48:02.000Z", JournalLevel.INFO, "app second")
        append(app, "2026-11-17T19:48:03.000Z", JournalLevel.INFO, "app third")
        rotate(app, JournalFiles.APP_BACKUPS)
        append(core, "2026-11-17T19:48:04.000Z", JournalLevel.INFO, "core first")

        val records = JournalFiles.records(app, core)

        assertEquals(
            listOf("app first", "app second", "app third", "core first"),
            records.map { it.text.substringAfter("INFO ") },
        )
    }

    @Test
    fun rotationKeepsTheConfiguredBackups() {
        val file = File(folder.root, "app.log")
        for (index in 1..20) {
            JournalFiles.append(file, line(index, "record $index"), limit = 120, keep = 2)
        }

        assertTrue(file.isFile)
        assertTrue(File(file.path + ".1").isFile)
        assertTrue(File(file.path + ".2").isFile)
        assertFalse(File(file.path + ".3").exists())
        assertTrue(file.length() <= 120)
        assertTrue(File(file.path + ".1").length() <= 120)
        assertTrue(File(file.path + ".2").length() <= 120)

        val current = file.readText()
        assertTrue(current.contains("record 20"))
    }

    @Test
    fun recordsOlderThanTheBackupsAreDropped() {
        val file = File(folder.root, "app.log")
        for (index in 1..20) {
            JournalFiles.append(file, line(index, "record $index"), limit = 120, keep = 2)
        }

        val texts = JournalFiles.read(file, keep = 2).map { it.text }

        assertFalse(texts.any { it.endsWith("record 1") })
        assertTrue(texts.any { it.contains("record 20") })
    }

    @Test
    fun currentFilesOnlySkipTheBackups() {
        val app = File(folder.root, "app.log")
        val core = File(folder.root, "journal.log")
        append(app, "2026-11-17T19:48:01.000Z", JournalLevel.INFO, "app old")
        append(app, "2026-11-17T19:48:02.000Z", JournalLevel.INFO, "app rotated")
        rotate(app, JournalFiles.APP_BACKUPS)
        append(app, "2026-11-17T19:48:03.000Z", JournalLevel.INFO, "app current")
        append(core, "2026-11-17T19:48:04.000Z", JournalLevel.INFO, "core current")

        val all = JournalFiles.records(app, core).map { it.text }
        val current = JournalFiles.records(app, core, backups = false).map { it.text }

        assertTrue(all.any { it.contains("app old") })
        assertFalse(current.any { it.contains("app old") })
        assertTrue(current.any { it.contains("app current") })
        assertTrue(current.any { it.contains("core current") })
    }

    @Test
    fun missingFileGivesNoRecords() {
        assertEquals(emptyList<JournalRecord>(), JournalFiles.read(File(folder.root, "absent.log"), 2))
    }

    private fun append(
        file: File,
        time: String,
        level: JournalLevel,
        message: String,
    ) {
        file.appendText(JournalFormat.line(Instant.parse(time), level, message))
    }

    private fun rotate(
        file: File,
        keep: Int,
    ) {
        File(file.path + ".2").delete()
        File(file.path + ".1").renameTo(File(file.path + ".2"))
        file.renameTo(File(file.path + ".1"))
    }

    private fun line(
        index: Int,
        message: String,
    ): String = JournalFormat.line(Instant.parse("2026-11-17T19:48:02.000Z").plusMillis(index.toLong()), JournalLevel.DEBUG, message)
}
