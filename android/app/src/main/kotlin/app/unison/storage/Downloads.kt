package app.unison.storage

import android.content.ContentValues
import android.content.Context
import android.net.Uri
import android.os.Environment
import android.provider.DocumentsContract
import android.provider.MediaStore
import android.util.Log
import android.webkit.MimeTypeMap
import java.io.File

/**
 * Files the app puts into the shared Downloads collection. Entries are created
 * as pending, so a half written file never becomes visible to other apps.
 */
object Downloads {
    /** Directory of the app inside the Downloads collection. */
    val RELATIVE_PATH = Environment.DIRECTORY_DOWNLOADS + "/Unison"

    /** MIME type used when the real one is unknown. */
    const val DEFAULT_MIME = "application/octet-stream"

    /** Creates a pending entry and returns the URI of its content. */
    fun create(
        context: Context,
        name: String,
        mimeType: String,
    ): Uri? {
        DownloadsFolder.current(context)?.let { tree ->
            try {
                DownloadsFolder.create(context, tree, name, mimeType)?.let { return it }
            } catch (e: Exception) {
                Log.w(TAG, "cannot create a file in the chosen folder: ${e.message}")
            }
        }
        return try {
            context.contentResolver.insert(collection(), values(name, mimeType))
        } catch (e: Exception) {
            Log.w(TAG, "cannot create an entry: ${e.message}")
            null
        }
    }

    /** Copies a file into a new entry and publishes it. */
    fun save(
        context: Context,
        file: File,
        name: String,
        mimeType: String,
    ): Boolean {
        val uri = create(context, name, mimeType) ?: return false
        val written =
            try {
                context.contentResolver.openOutputStream(uri)?.use { target ->
                    file.inputStream().use { it.copyTo(target) }
                }
            } catch (e: Exception) {
                Log.w(TAG, "cannot write an entry: ${e.message}")
                null
            }
        if (written == null) {
            discard(context, uri)
            return false
        }
        publish(context, uri)
        return true
    }

    /** Makes a finished entry visible to other apps. */
    fun publish(
        context: Context,
        uri: Uri,
    ) {
        // Files in a picked folder are visible from the start.
        if (DocumentsContract.isDocumentUri(context, uri)) {
            return
        }
        val values = ContentValues().apply { put(MediaStore.Downloads.IS_PENDING, 0) }
        try {
            context.contentResolver.update(uri, values, null, null)
        } catch (e: Exception) {
            Log.w(TAG, "cannot publish an entry: ${e.message}")
        }
    }

    /** Removes an entry that was not finished. */
    fun discard(
        context: Context,
        uri: Uri,
    ) {
        try {
            if (DocumentsContract.isDocumentUri(context, uri)) {
                DocumentsContract.deleteDocument(context.contentResolver, uri)
                return
            }
            context.contentResolver.delete(uri, null, null)
        } catch (e: Exception) {
            Log.w(TAG, "cannot remove an entry: ${e.message}")
        }
    }

    /** MIME type of a file name, or [DEFAULT_MIME]. */
    fun mimeTypeOf(name: String): String {
        val extension = name.substringAfterLast('.', "").lowercase()
        return MimeTypeMap.getSingleton().getMimeTypeFromExtension(extension) ?: DEFAULT_MIME
    }

    /** File name without directories, with an extension that fits the MIME type. */
    fun nameWithExtension(
        name: String,
        mimeType: String,
    ): String {
        val base =
            name
                .substringAfterLast('/')
                .substringAfterLast('\\')
                .trim()
                .ifBlank { FALLBACK_NAME }
        if (base.contains('.')) {
            return base
        }
        val extension = MimeTypeMap.getSingleton().getExtensionFromMimeType(mimeType) ?: return base
        return "$base.$extension"
    }

    private fun collection(): Uri = MediaStore.Downloads.getContentUri(MediaStore.VOLUME_EXTERNAL_PRIMARY)

    private fun values(
        name: String,
        mimeType: String,
    ): ContentValues =
        ContentValues().apply {
            put(MediaStore.Downloads.DISPLAY_NAME, name)
            put(MediaStore.Downloads.MIME_TYPE, mimeType)
            put(MediaStore.Downloads.RELATIVE_PATH, RELATIVE_PATH)
            put(MediaStore.Downloads.IS_PENDING, 1)
        }

    private const val TAG = "Downloads"
    private const val FALLBACK_NAME = "unison"
}
