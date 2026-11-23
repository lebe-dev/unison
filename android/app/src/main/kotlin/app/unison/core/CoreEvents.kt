package app.unison.core

import com.google.protobuf.InvalidProtocolBufferException
import kotlinx.coroutines.channels.BufferOverflow
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.asSharedFlow
import unison.mobile.v1.Mobile

/** Decodes one serialized unison.mobile.v1.Event, or returns null. */
internal fun decodeEvent(data: ByteArray): Mobile.Event? =
    try {
        Mobile.Event.parseFrom(data)
    } catch (_: InvalidProtocolBufferException) {
        null
    }

/** Events of the core, one stream for the whole process. */
class CoreEventStream {
    private val mutable =
        MutableSharedFlow<Mobile.Event>(
            extraBufferCapacity = BUFFER_SIZE,
            onBufferOverflow = BufferOverflow.DROP_OLDEST,
        )

    /** Events as they arrive. Slow collectors lose the oldest ones. */
    val events: SharedFlow<Mobile.Event> = mutable.asSharedFlow()

    /** Publishes a serialized event and drops it when it does not decode. */
    fun publish(data: ByteArray) {
        decodeEvent(data)?.let(mutable::tryEmit)
    }

    private companion object {
        const val BUFFER_SIZE = 32
    }
}

/** Events delivered by the running core. */
val coreEvents = CoreEventStream()
