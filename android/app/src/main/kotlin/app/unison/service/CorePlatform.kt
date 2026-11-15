package app.unison.service

import app.unison.core.AndroidKeystore
import app.unison.core.coreEvents
import app.unison.core.mobile.Platform

/** Platform callbacks the core uses to reach Android. */
class CorePlatform : Platform {
    override fun onEvent(evt: ByteArray) = coreEvents.publish(evt)

    override fun writeClipboard(item: ByteArray) = Unit

    override fun openBlobSink(meta: ByteArray): Long = -1L

    override fun keystoreUnwrap(blob: ByteArray): ByteArray = AndroidKeystore.unwrap(blob)

    override fun keystoreWrap(plain: ByteArray): ByteArray = AndroidKeystore.wrap(plain)

    override fun announce(info: ByteArray) = Unit
}
