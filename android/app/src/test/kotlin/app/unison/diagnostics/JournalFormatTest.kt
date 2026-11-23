package app.unison.diagnostics

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.time.Instant

class JournalFormatTest {
    private val at = Instant.parse("2026-11-17T19:48:02.123Z")

    @Test
    fun lineCarriesTheTimeTheLevelTheMessageAndTheAttributes() {
        val line =
            JournalFormat.line(
                at = at,
                level = JournalLevel.DEBUG,
                message = "connected to the peer",
                attrs = listOf("peer" to "3f2a", "attempt" to 2),
            )

        assertEquals("2026-11-17T19:48:02.123Z DEBUG connected to the peer peer=3f2a attempt=2\n", line)
    }

    @Test
    fun attributesWithSpacesOrEqualsAreQuoted() {
        val line =
            JournalFormat.line(
                at = at,
                level = JournalLevel.WARN,
                message = "read failed",
                attrs = listOf("note" to "two words", "pair" to "a=b"),
            )

        assertEquals("2026-11-17T19:48:02.123Z WARN read failed note=\"two words\" pair=\"a=b\"\n", line)
    }

    @Test
    fun emptyAttributeIsQuoted() {
        val line = JournalFormat.line(at, JournalLevel.INFO, "a fact", listOf("value" to ""))

        assertEquals("2026-11-17T19:48:02.123Z INFO a fact value=\"\"\n", line)
    }

    @Test
    fun pathInMessageAndAttributeIsReplaced() {
        val line =
            JournalFormat.line(
                at = at,
                level = JournalLevel.WARN,
                message = "cannot open /Users/alice/Library/Unison",
                attrs = listOf("error" to "open /home/alice/unison.db: denied"),
            )

        assertEquals(
            "2026-11-17T19:48:02.123Z WARN cannot open [path] " +
                "error=\"open [path]: denied\"\n",
            line,
        )
    }

    @Test
    fun messageWithNewlineKeepsOneLine() {
        val line = JournalFormat.line(at, JournalLevel.WARN, "two\nlines")

        assertEquals("2026-11-17T19:48:02.123Z WARN \"two\\nlines\"\n", line)
        assertEquals(1, line.count { it == '\n' })
    }

    @Test
    fun lineIsParsedBackIntoARecord() {
        val line = JournalFormat.line(at, JournalLevel.ERROR, "failed", listOf("code" to 13))

        val record = JournalFormat.parse(line)

        assertEquals(at, record?.at)
        assertEquals(JournalLevel.ERROR, record?.level)
        assertEquals("2026-11-17T19:48:02.123Z ERROR failed code=13", record?.text)
    }

    @Test
    fun otherLinesAreNotRecords() {
        assertNull(JournalFormat.parse(""))
        assertNull(JournalFormat.parse("something else"))
        assertNull(JournalFormat.parse("2026-11-17T19:48:02.123Z TRACE a detail"))
    }

    @Test
    fun infoHidesDebugRecords() {
        assertFalse(JournalDisplay.INFO.shows(JournalLevel.DEBUG))
        assertTrue(JournalDisplay.INFO.shows(JournalLevel.INFO))
        assertTrue(JournalDisplay.INFO.shows(JournalLevel.WARN))
        assertTrue(JournalDisplay.INFO.shows(JournalLevel.ERROR))
    }

    @Test
    fun debugShowsEveryRecord() {
        JournalLevel.entries.forEach { level ->
            assertTrue(JournalDisplay.DEBUG.shows(level))
        }
    }
}
