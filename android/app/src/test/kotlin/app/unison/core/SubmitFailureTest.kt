package app.unison.core

import org.junit.Assert.assertEquals
import org.junit.Test

class SubmitFailureTest {
    @Test
    fun pauseIsRecognized() {
        assertEquals(SubmitFailure.PAUSED, SubmitFailure.of(Exception("clip: synchronization is paused")))
    }

    @Test
    fun unsupportedContentIsRecognized() {
        assertEquals(SubmitFailure.UNSUPPORTED, SubmitFailure.of(Exception("clip: unsupported content")))
    }

    @Test
    fun stoppedCoreIsRecognized() {
        assertEquals(SubmitFailure.NOT_RUNNING, SubmitFailure.of(Exception("mobile: core is not running")))
    }

    @Test
    fun otherFailuresStayUnknown() {
        assertEquals(SubmitFailure.OTHER, SubmitFailure.of(Exception("disk is full")))
        assertEquals(SubmitFailure.OTHER, SubmitFailure.of(Exception()))
    }
}
