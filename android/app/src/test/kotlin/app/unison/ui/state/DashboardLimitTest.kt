package app.unison.ui.state

import org.junit.Assert.assertEquals
import org.junit.Test

class DashboardLimitTest {
    @Test
    fun offeredValuesAreKept() {
        DashboardLimit.OPTIONS.forEach { value ->
            assertEquals(value, DashboardLimit.normalize(value))
        }
    }

    @Test
    fun otherValuesFallBackToTheDefault() {
        assertEquals(DashboardLimit.DEFAULT, DashboardLimit.normalize(0))
        assertEquals(DashboardLimit.DEFAULT, DashboardLimit.normalize(-1))
        assertEquals(DashboardLimit.DEFAULT, DashboardLimit.normalize(7))
        assertEquals(DashboardLimit.DEFAULT, DashboardLimit.normalize(96))
    }

    @Test
    fun defaultIsOneOfTheOfferedValues() {
        assertEquals(DashboardLimit.DEFAULT, DashboardLimit.normalize(DashboardLimit.DEFAULT))
    }
}
