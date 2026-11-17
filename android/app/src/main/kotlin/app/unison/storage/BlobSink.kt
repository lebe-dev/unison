package app.unison.storage

import android.content.Context
import android.net.Uri
import android.os.ParcelFileDescriptor
import android.util.Log
import com.google.protobuf.InvalidProtocolBufferException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.DelicateCoroutinesApi
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import unison.mobile.v1.Mobile

/**
 * Receives the files a peer sends. The core asks for a descriptor, writes the
 * payload into it and closes it; the sink copies the payload into the Downloads
 * collection while it arrives.
 */
class BlobSink(
    private val context: Context,
    private val scope: CoroutineScope,
) {

    /** Opens a sink for one file and returns the write descriptor, or -1. */
    @OptIn(DelicateCoroutinesApi::class)
    fun open(meta: ByteArray): Long {
        val payload = try {
            Mobile.FilePayload.parseFrom(meta)
        } catch (e: InvalidProtocolBufferException) {
            Log.w(TAG, "file metadata does not decode: ${e.message}")
            return UNSUPPORTED
        }

        val mimeType = payload.mimeType.ifBlank { Downloads.DEFAULT_MIME }
        val uri = Downloads.create(context, Downloads.nameWithExtension(payload.fileName, mimeType), mimeType)
            ?: return UNSUPPORTED

        val pipe = try {
            ParcelFileDescriptor.createPipe()
        } catch (e: Exception) {
            Log.w(TAG, "cannot create a pipe: ${e.message}")
            Downloads.discard(context, uri)
            return UNSUPPORTED
        }

        // Started even when the service is going away: the read end must be drained and closed,
        // or the core blocks on a full pipe and the pending entry stays behind.
        scope.launch(Dispatchers.IO, start = CoroutineStart.ATOMIC) { receive(pipe[0], uri, payload.size) }
        return pipe[1].detachFd().toLong()
    }

    /** Copies the payload into the entry and publishes it. */
    private fun receive(source: ParcelFileDescriptor, uri: Uri, expected: Long) {
        val written = try {
            ParcelFileDescriptor.AutoCloseInputStream(source).use { input ->
                context.contentResolver.openOutputStream(uri)?.use { output ->
                    input.copyTo(output)
                }
            }
        } catch (e: Exception) {
            Log.w(TAG, "cannot save an incoming file: ${e.message}")
            null
        }

        if (written == null || (expected > 0 && written != expected)) {
            if (written != null) {
                Log.w(TAG, "saved $written bytes of $expected")
            }
            Downloads.discard(context, uri)
            return
        }
        Downloads.publish(context, uri)
        Log.i(TAG, "saved an incoming file of $written bytes")
    }

    private companion object {
        const val TAG = "BlobSink"
        const val UNSUPPORTED = -1L
    }
}
