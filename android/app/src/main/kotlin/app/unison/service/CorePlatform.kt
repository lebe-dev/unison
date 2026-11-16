package app.unison.service

import android.content.Context
import app.unison.clipboard.ClipboardWriter
import app.unison.core.AndroidKeystore
import app.unison.core.coreEvents
import app.unison.core.mobile.Platform
import app.unison.discovery.NsdDiscovery
import app.unison.storage.BlobSink

/** Platform callbacks the core uses to reach Android. */
class CorePlatform(
    private val context: Context,
    private val discovery: NsdDiscovery,
    private val blobs: BlobSink,
) : Platform {
    override fun onEvent(evt: ByteArray) = coreEvents.publish(evt)

    override fun writeClipboard(item: ByteArray) = ClipboardWriter.write(context, item)

    override fun openBlobSink(meta: ByteArray): Long = blobs.open(meta)

    override fun keystoreUnwrap(blob: ByteArray): ByteArray = AndroidKeystore.unwrap(blob)

    override fun keystoreWrap(plain: ByteArray): ByteArray = AndroidKeystore.wrap(plain)

    override fun announce(info: ByteArray) = discovery.announce(info)
}
