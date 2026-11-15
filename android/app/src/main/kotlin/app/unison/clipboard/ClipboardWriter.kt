package app.unison.clipboard

import android.content.ClipData
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
        if (draft == null) {
            Log.i(TAG, "content without text is skipped")
            return
        }

        val label = context.getString(R.string.app_name)
        val plain = draft.text ?: ClipboardReader.plainTextOf(draft.html.orEmpty())
        val clip = if (draft.html.isNullOrEmpty()) {
            ClipData.newPlainText(label, plain)
        } else {
            ClipData.newHtmlText(label, plain, draft.html)
        }

        try {
            val manager = context.getSystemService(ClipboardManager::class.java) ?: return
            manager.setPrimaryClip(clip)
        } catch (e: Exception) {
            Log.w(TAG, "clipboard write failed: ${e.message}")
            return
        }
        UnisonSettings.setLastClipHash(context, draft.contentHash())
        Log.i(TAG, "clipboard updated with text of ${plain.length} chars, html ${!draft.html.isNullOrEmpty()}")
    }
}
