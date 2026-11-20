package app.unison.clipboard

import android.content.ClipData
import android.content.ClipDescription
import android.content.ClipboardManager
import android.content.Context
import android.os.PersistableBundle
import android.util.Log
import app.unison.bridge.BridgeContract
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
    fun write(
        context: Context,
        item: ByteArray,
    ) {
        val payload =
            try {
                Mobile.ClipPayload.parseFrom(item)
            } catch (e: InvalidProtocolBufferException) {
                Log.w(TAG, "payload does not decode: ${e.message}")
                return
            }

        val itemId = payload.itemId
        val draft = ClipboardDraft.fromPayload(payload)
        if (draft != null) {
            writeText(context, draft, itemId)
            return
        }

        val image = imagePart(payload)
        if (image != null) {
            writeImage(context, image, itemId)
            return
        }
        Log.i(TAG, "content without text or image is skipped")
    }

    /** Writes plain and HTML text into the clipboard. */
    private fun writeText(
        context: Context,
        draft: ClipboardDraft,
        itemId: String,
    ) {
        val label = BridgeContract.CLIP_LABEL
        val plain = draft.text ?: ClipboardReader.plainTextOf(draft.html.orEmpty())
        val clip =
            if (draft.html.isNullOrEmpty()) {
                ClipData.newPlainText(label, plain)
            } else {
                ClipData.newHtmlText(label, plain, draft.html)
            }
        tag(clip, itemId)

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
    private fun writeImage(
        context: Context,
        part: Mobile.ClipPart,
        itemId: String,
    ) {
        val uri = ClipboardMedia.storeImage(context, part.mimeType, part.data.toByteArray())
        if (uri == null) {
            Log.w(TAG, "cannot store the image of ${part.mimeType}")
            return
        }

        val description = ClipDescription(BridgeContract.CLIP_LABEL, arrayOf(part.mimeType))
        val clip = ClipData(description, ClipData.Item(uri))
        tag(clip, itemId)
        if (!apply(context, clip)) {
            return
        }
        Log.i(TAG, "clipboard updated with ${part.mimeType} of ${part.data.size()} bytes")
    }

    /**
     * Names the item in the clip description, so a keyboard can tell the write from a copy of
     * the user. Content that is not a recorded item stays untagged.
     */
    private fun tag(
        clip: ClipData,
        itemId: String,
    ) {
        if (itemId.isEmpty()) {
            return
        }
        clip.description.extras =
            PersistableBundle().apply {
                putString(BridgeContract.EXTRA_ITEM_ID, itemId)
            }
    }

    /** Sets the primary clip, reporting whether it worked. */
    private fun apply(
        context: Context,
        clip: ClipData,
    ): Boolean =
        try {
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
            return Mobile.ClipPart
                .newBuilder()
                .setMimeType(payload.mimeType)
                .setData(payload.data)
                .build()
        }
        return null
    }

    private const val MIME_IMAGE = "image/"
}
