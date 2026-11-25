package app.unison.ui.state

import org.junit.Assert.assertEquals
import org.junit.Test
import unison.local.v1.Local

class MediatedStateTest {
    @Test
    fun onlyWaitingOffersAskForAnAnswer() {
        val state =
            MediatedState(
                pairs =
                    listOf(
                        pair("waiting", Local.MediatedPairDirection.MEDIATED_PAIR_DIRECTION_INCOMING),
                        pair("running", Local.MediatedPairDirection.MEDIATED_PAIR_DIRECTION_INCOMING)
                            .toBuilder()
                            .setState(Local.MediatedPairState.MEDIATED_PAIR_STATE_AWAITING_FINISH)
                            .build(),
                    ),
            )

        assertEquals(listOf("waiting"), state.offers.map { it.sessionId })
        assertEquals(2, state.incoming.size)
        assertEquals(1, state.pending)
    }

    @Test
    fun outgoingHasOnlyExchangesThisDeviceRuns() {
        val state =
            MediatedState(
                pairs =
                    listOf(
                        pair("offer", Local.MediatedPairDirection.MEDIATED_PAIR_DIRECTION_INCOMING),
                        pair("exchange", Local.MediatedPairDirection.MEDIATED_PAIR_DIRECTION_MEDIATOR),
                    ),
            )

        assertEquals(listOf("exchange"), state.outgoing.map { it.sessionId })
        assertEquals("offer", state.exchange("offer")?.sessionId)
        assertEquals(null, state.exchange("gone"))
    }

    private fun pair(
        sessionId: String,
        direction: Local.MediatedPairDirection,
    ): Local.MediatedPair =
        Local.MediatedPair
            .newBuilder()
            .setSessionId(sessionId)
            .setDirection(direction)
            .setState(Local.MediatedPairState.MEDIATED_PAIR_STATE_WAITING)
            .build()
}
