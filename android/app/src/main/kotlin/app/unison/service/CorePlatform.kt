package app.unison.service

import android.content.Context
import android.util.Log
import app.unison.clipboard.ClipboardWriter
import app.unison.core.AndroidKeystore
import app.unison.core.coreEvents
import app.unison.core.mobile.Platform
import app.unison.diagnostics.Journal
import app.unison.discovery.NsdDiscovery
import app.unison.storage.BlobSink

/**
 * Platform callbacks the core uses to reach Android.
 *
 * The callbacks run on goroutines of the core through JNI and have no error
 * result, so an exception must never leave them: a pending Java exception on
 * a Go thread breaks the next JNI call. A failure is logged and reported the
 * way the core expects it: no descriptor, no key, nothing written.
 */
class CorePlatform(
    private val context: Context,
    private val discovery: NsdDiscovery,
    private val blobs: BlobSink,
) : Platform {
    override fun onEvent(evt: ByteArray) = guard("event delivery") { coreEvents.publish(evt) }

    override fun writeClipboard(item: ByteArray) = guard("clipboard write") { ClipboardWriter.write(context, item) }

    override fun openBlobSink(meta: ByteArray): Long = guarded("opening a file sink", NO_DESCRIPTOR) { blobs.open(meta) }

    // A null array reaches the core as an empty secret, which it treats as a failure.
    override fun keystoreUnwrap(blob: ByteArray): ByteArray? = guarded("keystore unwrap", null) { AndroidKeystore.unwrap(blob) }

    override fun keystoreWrap(plain: ByteArray): ByteArray? = guarded("keystore wrap", null) { AndroidKeystore.wrap(plain) }

    override fun announce(info: ByteArray) = guard("announcement") { discovery.announce(info) }

    private inline fun guard(
        what: String,
        block: () -> Unit,
    ) = guarded(what, Unit, block)

    private inline fun <T> guarded(
        what: String,
        fallback: T,
        block: () -> T,
    ): T =
        try {
            block()
        } catch (e: Exception) {
            Log.w(TAG, "$what failed: ${e.message}")
            Journal.warn("$what failed", "error" to e.message)
            fallback
        }

    private companion object {
        const val TAG = "CorePlatform"
        const val NO_DESCRIPTOR = -1L
    }
}
