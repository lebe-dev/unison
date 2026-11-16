package app.unison.bridge

import android.os.ParcelFileDescriptor
import android.util.Log
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import java.io.File
import java.io.OutputStream
import unison.local.v1.Local

/** Streams the content of a history entry into a pipe. */
class BridgeContent(
    private val core: BridgeCore,
    private val scope: CoroutineScope,
    private val cacheDir: File,
) {

    /**
     * Creates a pipe and writes the content into it on a background thread, so
     * the binder thread never waits for a download. The write end is closed when
     * the content ends or the reader does not take it in time.
     */
    fun open(itemId: String, mime: String): ParcelFileDescriptor {
        val pipe = ParcelFileDescriptor.createPipe()
        val read = pipe[0]
        val write = pipe[1]
        val writer = scope.launch(Dispatchers.IO) {
            try {
                ParcelFileDescriptor.AutoCloseOutputStream(write).use { output ->
                    stream(itemId, mime, output)
                }
            } catch (e: Exception) {
                Log.w(TAG, "streaming an item failed: ${e.message}")
            }
        }
        scope.launch {
            delay(STREAM_TIMEOUT_MS)
            if (!writer.isActive) {
                return@launch
            }
            Log.w(TAG, "the reader did not take the content in time")
            write.closeQuietly(TAG)
            writer.cancel()
        }.also { timeout -> writer.invokeOnCompletion { timeout.cancel() } }
        return read
    }

    private suspend fun stream(itemId: String, mime: String, output: OutputStream) {
        val name = safeName(itemId)
        if (name.isEmpty()) {
            return
        }
        val entry = core.item(itemId) ?: return
        val representation = representation(entry.item, mime) ?: return
        val inline = representation.inlineBytes
        if (!inline.isEmpty) {
            output.write(inline.toByteArray())
            return
        }

        val dir = File(cacheDir, name)
        try {
            val file = download(entry.item, representation, dir) ?: return
            file.inputStream().use { input -> input.copyTo(output, BUFFER_BYTES) }
        } finally {
            dir.deleteRecursively()
        }
    }

    /** Representation of the requested type, or the first one available. */
    private fun representation(item: Local.ClipItem, mime: String): Local.Representation? {
        val given = item.representationsList.firstOrNull { it.mimeType == mime }
        if (given != null) {
            return given
        }
        return item.representationsList.firstOrNull()
    }

    /** Downloads the payload of a representation and returns the written file. */
    private suspend fun download(
        item: Local.ClipItem,
        representation: Local.Representation,
        dir: File,
    ): File? {
        dir.mkdirs()
        val single = item.representationsCount == 1
        val target = if (single) File(dir, FILE_NAME) else dir
        val written = core.fetch(item.id, target.absolutePath)
        if (single) {
            return target.takeIf { it.isFile }
        }
        val index = item.representationsList.indexOf(representation)
        val path = written.getOrNull(index) ?: written.firstOrNull()
        return path?.let(::File)?.takeIf { it.isFile }
    }

    /** Item id without characters that do not belong in a file name. */
    private fun safeName(itemId: String): String =
        itemId.filter { it.isLetterOrDigit() || it == '-' }.take(MAX_NAME_LENGTH)

    private companion object {
        private const val TAG = "BridgeContent"
        private const val FILE_NAME = "payload"
        private const val BUFFER_BYTES = 64 * 1024
        private const val MAX_NAME_LENGTH = 64

        /** How long the reader may keep the pipe without taking the content. */
        private const val STREAM_TIMEOUT_MS = 60_000L
    }
}
