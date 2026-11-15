package app.unison.core

import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.async
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Test
import unison.mobile.v1.Mobile

class CoreEventStreamTest {

    @Test
    fun eventReachesCollectors() = runTest {
        val stream = CoreEventStream()
        val event = Mobile.Event.newBuilder()
            .setType(Mobile.EventType.EVENT_TYPE_PEER_ONLINE)
            .setTsUnixMs(42)
            .build()
        val received = async(start = CoroutineStart.UNDISPATCHED) { stream.events.first() }

        stream.publish(event.toByteArray())

        assertEquals(Mobile.EventType.EVENT_TYPE_PEER_ONLINE, received.await().type)
        assertEquals(42, received.await().tsUnixMs)
    }
}
