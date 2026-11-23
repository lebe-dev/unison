package app.unison.clipboard

import org.junit.Assert.assertEquals
import org.junit.Test
import unison.local.v1.Local

class PauseOptionTest {
    private val now = 1_700_000_000_000L

    @Test
    fun timedOptionsSetADeadline() {
        assertEquals(
            PauseRequest(Local.PauseMode.PAUSE_MODE_UNTIL, now + 5 * 60 * 1000L),
            PauseOption.FIVE_MINUTES.request(now),
        )
        assertEquals(
            PauseRequest(Local.PauseMode.PAUSE_MODE_UNTIL, now + 60 * 60 * 1000L),
            PauseOption.ONE_HOUR.request(now),
        )
    }

    @Test
    fun indefiniteOptionHasNoDeadline() {
        assertEquals(
            PauseRequest(Local.PauseMode.PAUSE_MODE_INDEFINITE, 0L),
            PauseOption.INDEFINITE.request(now),
        )
    }

    @Test
    fun resumeTurnsThePauseOff() {
        assertEquals(PauseRequest(Local.PauseMode.PAUSE_MODE_OFF, 0L), PauseRequest.resume())
    }
}
