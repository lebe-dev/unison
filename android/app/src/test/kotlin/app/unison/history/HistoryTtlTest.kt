package app.unison.history

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import unison.local.v1.Local

class HistoryTtlTest {
    @Test
    fun lifetimeIsRenderedInTheLargestUnit() {
        assertEquals("7d", HistoryTtlText.format(7 * 24 * 60 * 60L))
        assertEquals("1d", HistoryTtlText.format(24 * 60 * 60L))
        assertEquals("90m", HistoryTtlText.format(90 * 60L))
        assertEquals("45s", HistoryTtlText.format(45L))
        assertEquals("", HistoryTtlText.format(0L))
    }

    @Test
    fun lifetimeIsParsedFromDaysAndHours() {
        assertEquals(7 * 24 * 60 * 60L, HistoryTtlText.parse("7d"))
        assertEquals(24 * 60 * 60L, HistoryTtlText.parse(" 24H "))
        assertEquals(90 * 60L, HistoryTtlText.parse("90m"))
        assertEquals(HistoryTtlDefaults.MAX, HistoryTtlText.parse("365d"))
    }

    @Test
    fun lifetimeOutsideTheRangeIsRefused() {
        assertNull(HistoryTtlText.parse("0d"))
        assertNull(HistoryTtlText.parse("366d"))
        assertNull(HistoryTtlText.parse("week"))
        assertNull(HistoryTtlText.parse(""))
        assertNull(HistoryTtlText.parse("-1d"))
    }

    @Test
    fun lifetimeOfAKindIsReplaced() {
        val changed = HistoryTtlDefaults.values.with(Local.Kind.KIND_FILES, 3600L)

        assertEquals(3600L, changed.of(Local.Kind.KIND_FILES))
        assertEquals(HistoryTtlDefaults.TEXT, changed.of(Local.Kind.KIND_TEXT))
        assertEquals(HistoryTtlDefaults.IMAGE, changed.of(Local.Kind.KIND_IMAGE))
    }
}
