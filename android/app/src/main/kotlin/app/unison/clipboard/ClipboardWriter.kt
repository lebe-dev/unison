package app.unison.clipboard

import android.content.ClipData
import android.content.ClipDescription
import android.content.ClipboardManager
import android.content.Context
import android.util.Log
import app.unison.R
import app.unison.service.UnisonSettings
import com.google.protobuf.InvalidProtocolBufferException
import unison.mobile.v1.Mobile

/**
 * Writes incoming clipboard content into the system clipboard. Writing is
 * allowed in the background, so the core calls this from the service.
 */
object ClipboardWriter {
    private const val TAG = "ClipboardWriter"

    /** Applies a serialized unison.mobile.v1.ClipPayload. */
    fun write(context: Context, item: ByteArray) {
        val payload = try {
            Mobile.ClipPayload.parseFrom(item)
        } catch (e: InvalidProtocolBufferException) {
            Log.w(TAG, "payload does not decode: ${e.message}")
            return
        }

        val draft = ClipboardDraft.fromPayload(payload)
        if (draft != null) {
            writeText(context, draft)
            return
        }

        val image = imagePart(payload)
        if (image != null) {
            writeImage(context, image)
            return
        }
        Log.i(TAG, "content without text or image is skipped")
    }

    /** Writes plain and HTML text into the clipboard. */
    private fun writeText(context: Context, draft: ClipboardDraft) {
        val label = context.getString(R.string.app_name)
        val plain = draft.text ?: ClipboardReader.plainTextOf(draft.html.orEmpty())
        val clip = if (draft.html.isNullOrEmpty()) {
            ClipData.newPlainText(label, plain)
        } else {
            ClipData.newHtmlText(label, plain, draft.html)
        }

        if (!apply(context, clip)) {
            return
        }
        UnisonSettings.setLastClipHash(context, draft.contentHash())
        Log.i(TAG, "clipboard updated with text of ${plain.length} chars, html ${!draft.html.isNullOrEmpty()}")
    }

    /**
     * Writes an image into the clipboard as a file provider URI. Receiving apps
     * get a temporary read grant from the clipboard service, so the image is
     * never stored in a shared collection.
     */
    private fun writeImage(context: Context, part: Mobile.ClipPart) {
        val uri = ClipboardMedia.storeImage(context, part.mimeType, part.data.toByteArray())
        if (uri == null) {
            Log.w(TAG, "cannot store the image of ${part.mimeType}")
            return
        }

        val label = context.getString(R.string.app_name)
        val description = ClipDescription(label, arrayOf(part.mimeType))
        val clip = ClipData(description, ClipData.Item(uri))
        if (!apply(context, clip)) {
            return
        }
        Log.i(TAG, "clipboard updated with ${part.mimeType} of ${part.data.size()} bytes")
    }

    /** Sets the primary clip, reporting whether it worked. */
    private fun apply(context: Context, clip: ClipData): Boolean = try {
        val manager = context.getSystemService(ClipboardManager::class.java)
        manager?.setPrimaryClip(clip) ?: return false
        true
    } catch (e: Exception) {
        Log.w(TAG, "clipboard write failed: ${e.message}")
        false
    }

    /** Image representation of a payload, or null when it carries no image. */
    private fun imagePart(payload: Mobile.ClipPayload): Mobile.ClipPart? {
        val parts = payload.partsList.filter { it.mimeType.startsWith(MIME_IMAGE) && !it.data.isEmpty }
        if (parts.isNotEmpty()) {
            return parts.first()
        }
        if (payload.mimeType.startsWith(MIME_IMAGE) && !payload.data.isEmpty) {
            return Mobile.ClipPart.newBuilder()
                .setMimeType(payload.mimeType)
                .setData(payload.data)
                .build()
        }
        return null
    }

    private const val MIME_IMAGE = "image/"
}
