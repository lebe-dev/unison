package app.unison.share

import android.content.ContentResolver
import android.content.Intent
import android.net.Uri
import android.provider.OpenableColumns
import android.util.Log
import androidx.core.content.IntentCompat
import app.unison.clipboard.ClipboardDraft
import app.unison.clipboard.ClipboardReader

/** Content another app shares with Unison. */
sealed interface ShareContent {

    /** Text of another app. */
    data class Text(val draft: ClipboardDraft) : ShareContent

    /** Files of another app. */
    data class Files(val files: List<SharedFile>) : ShareContent
}

/** One file of a share request. */
data class SharedFile(
    val uri: Uri,
    val name: String,
    val size: Long,
    val mimeType: String,
)

/** True when the content can go to Friends devices. */
val ShareContent.allowsFriends: Boolean
    get() = this is ShareContent.Files

/** Reads the content of the intent that opened the share screen. */
object ShareIntents {
    private const val TAG = "ShareIntents"

    /** Files of one share request, the limit of the core. */
    const val MAX_FILES = 32

    /** MIME type of a file whose type the sender does not know. */
    const val DEFAULT_MIME = "application/octet-stream"

    /** Content of a share intent, or null when it carries nothing to send. */
    fun read(intent: Intent?, resolver: ContentResolver): ShareContent? {
        if (intent == null) {
            return null
        }
        val uris = streams(intent)
        if (uris.isNotEmpty()) {
            return files(uris, intent.type, resolver)
        }
        val draft = text(intent) ?: return null
        if (draft.isEmpty) {
            return null
        }
        return ShareContent.Text(draft)
    }

    /** Shared streams of the intent, in the order the sender put them. */
    private fun streams(intent: Intent): List<Uri> {
        val streams = when (intent.action) {
            Intent.ACTION_SEND_MULTIPLE -> intent.multipleStreams()
            Intent.ACTION_SEND -> listOfNotNull(intent.singleStream())
            else -> emptyList()
        }
        return (streams + clipStreams(intent)).distinct()
    }

    private fun Intent.singleStream(): Uri? =
        IntentCompat.getParcelableExtra(this, Intent.EXTRA_STREAM, Uri::class.java)

    private fun Intent.multipleStreams(): List<Uri> =
        IntentCompat.getParcelableArrayListExtra(this, Intent.EXTRA_STREAM, Uri::class.java).orEmpty()

    /** Streams some senders put into the clip data instead of the extras. */
    private fun clipStreams(intent: Intent): List<Uri> {
        val clip = intent.clipData ?: return emptyList()
        return (0 until clip.itemCount).mapNotNull { clip.getItemAt(it).uri }
    }

    /** Shared text of the intent, with its HTML form when the sender provides one. */
    private fun text(intent: Intent): ClipboardDraft? {
        val text = intent.getCharSequenceExtra(Intent.EXTRA_TEXT)?.toString()
        val html = intent.getStringExtra(Intent.EXTRA_HTML_TEXT)
        if (text.isNullOrEmpty() && html.isNullOrEmpty()) {
            return null
        }
        val plain = text ?: ClipboardReader.plainTextOf(html.orEmpty())
        return ClipboardDraft(text = plain, html = html)
    }

    /** Files behind the shared streams, without the ones that cannot be read. */
    private fun files(uris: List<Uri>, type: String?, resolver: ContentResolver): ShareContent? {
        val files = uris.mapNotNull { descriptorOf(it, type, resolver) }
        if (files.isEmpty()) {
            return null
        }
        return ShareContent.Files(files)
    }

    /** Name, size and type of one shared stream, or null when it cannot be read. */
    private fun descriptorOf(uri: Uri, type: String?, resolver: ContentResolver): SharedFile? = try {
        var name = ""
        var size = 0L
        resolver.query(uri, COLUMNS, null, null, null)?.use { cursor ->
            if (cursor.moveToFirst()) {
                val nameIndex = cursor.getColumnIndex(OpenableColumns.DISPLAY_NAME)
                if (nameIndex >= 0 && !cursor.isNull(nameIndex)) {
                    name = cursor.getString(nameIndex)
                }
                val sizeIndex = cursor.getColumnIndex(OpenableColumns.SIZE)
                if (sizeIndex >= 0 && !cursor.isNull(sizeIndex)) {
                    size = cursor.getLong(sizeIndex)
                }
            }
        }
        val mime = resolver.getType(uri)
            ?: type?.takeIf { it.isNotEmpty() && it != ANY_MIME }
            ?: DEFAULT_MIME
        SharedFile(
            uri = uri,
            name = name.ifBlank { nameOf(uri) },
            size = size.coerceAtLeast(0),
            mimeType = mime,
        )
    } catch (e: Exception) {
        Log.w(TAG, "cannot read a shared file: ${e.message}")
        null
    }

    /** Last path segment of a stream as a file name. */
    private fun nameOf(uri: Uri): String =
        uri.lastPathSegment?.substringAfterLast('/')?.ifBlank { null } ?: DEFAULT_NAME

    private const val ANY_MIME = "*/*"
    private const val DEFAULT_NAME = "file"
    private val COLUMNS = arrayOf(OpenableColumns.DISPLAY_NAME, OpenableColumns.SIZE)
}
