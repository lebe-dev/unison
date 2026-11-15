package app.unison.service

import android.content.Context
import app.unison.clipboard.ClipboardWriter
import app.unison.core.AndroidKeystore
import app.unison.core.coreEvents
import app.unison.core.mobile.Platform

/** Platform callbacks the core uses to reach Android. */
class CorePlatform(private val context: Context) : Platform {
    override fun onEvent(evt: ByteArray) = coreEvents.publish(evt)

    override fun writeClipboard(item: ByteArray) = ClipboardWriter.write(context, item)

    override fun openBlobSink(meta: ByteArray): Long = -1L

    override fun keystoreUnwrap(blob: ByteArray): ByteArray = AndroidKeystore.unwrap(blob)

    override fun keystoreWrap(plain: ByteArray): ByteArray = AndroidKeystore.wrap(plain)

    override fun announce(info: ByteArray) = Unit
}
