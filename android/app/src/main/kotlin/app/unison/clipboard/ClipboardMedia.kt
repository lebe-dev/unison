package app.unison.clipboard

import android.content.Context
import android.net.Uri
import android.webkit.MimeTypeMap
import androidx.core.content.FileProvider
import java.io.File
import java.util.UUID

/** Files of the clipboard that other apps read through the file provider. */
object ClipboardMedia {

    /** Stores the image of an incoming item and returns its URI, or null. */
    fun storeImage(context: Context, mimeType: String, data: ByteArray): Uri? {
        if (data.isEmpty()) {
            return null
        }
        val directory = File(context.cacheDir, DIRECTORY)
        if (!directory.isDirectory && !directory.mkdirs()) {
            return null
        }
        removeExpired(directory)

        val file = File(directory, UUID.randomUUID().toString() + extensionOf(mimeType))
        return try {
            file.writeBytes(data)
            FileProvider.getUriForFile(context, context.packageName + AUTHORITY_SUFFIX, file)
        } catch (e: Exception) {
            file.delete()
            null
        }
    }

    /** Removes images that stayed from earlier writes. */
    private fun removeExpired(directory: File) {
        val deadline = System.currentTimeMillis() - MAX_AGE_MS
        directory.listFiles()?.forEach { file ->
            if (file.lastModified() < deadline) {
                file.delete()
            }
        }
    }

    /** Extension of a MIME type, empty when it is unknown. */
    private fun extensionOf(mimeType: String): String {
        val extension = MimeTypeMap.getSingleton().getExtensionFromMimeType(mimeType) ?: return ""
        return "." + extension
    }

    /** Directory of the file provider inside the cache. */
    const val DIRECTORY = "clipboard"

    /** Authority suffix declared in the manifest. */
    const val AUTHORITY_SUFFIX = ".files"

    /** Age after which an unused image is removed. */
    private const val MAX_AGE_MS = 24 * 60 * 60 * 1000L
}
