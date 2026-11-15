package app.unison.clipboard

import com.google.protobuf.ByteString
import java.security.MessageDigest
import unison.mobile.v1.Mobile

/** Text content of the local clipboard. */
data class ClipboardDraft(val text: String?, val html: String?) {

    /** True when there is nothing to send. */
    val isEmpty: Boolean
        get() = text.isNullOrEmpty() && html.isNullOrEmpty()

    /** Payload the core accepts, one part per representation. */
    fun payload(): Mobile.ClipPayload {
        val builder = Mobile.ClipPayload.newBuilder()
        if (!text.isNullOrEmpty()) {
            builder.addParts(part(MIME_TEXT_PLAIN, text))
        }
        if (!html.isNullOrEmpty()) {
            builder.addParts(part(MIME_TEXT_HTML, html))
        }
        return builder.build()
    }

    /** Hash of the content, used to tell it from the content seen before. */
    fun contentHash(): String {
        val digest = MessageDigest.getInstance("SHA-256")
        digest.update(text.orEmpty().toByteArray(Charsets.UTF_8))
        digest.update(SEPARATOR)
        digest.update(html.orEmpty().toByteArray(Charsets.UTF_8))
        return digest.digest().toHex()
    }

    /** First characters of the text, for a preview in the app. */
    fun preview(limit: Int = PREVIEW_LIMIT): String {
        val source = text.orEmpty().trim().replace(WHITESPACE, " ")
        if (source.length <= limit) {
            return source
        }
        return source.take(limit).trimEnd() + "..."
    }

    private fun part(mime: String, value: String): Mobile.ClipPart =
        Mobile.ClipPart.newBuilder()
            .setMimeType(mime)
            .setData(ByteString.copyFromUtf8(value))
            .build()

    private fun ByteArray.toHex(): String {
        val out = StringBuilder(size * 2)
        for (byte in this) {
            out.append(HEX[(byte.toInt() shr 4) and 0x0F])
            out.append(HEX[byte.toInt() and 0x0F])
        }
        return out.toString()
    }

    companion object {
        const val MIME_TEXT_PLAIN = "text/plain"
        const val MIME_TEXT_HTML = "text/html"

        /** Length of the preview in characters. */
        const val PREVIEW_LIMIT = 120

        private const val HEX = "0123456789abcdef"
        private val SEPARATOR = byteArrayOf(0)
        private val WHITESPACE = Regex("\\s+")

        /** Text content of a core payload, or null when the payload carries no text. */
        fun fromPayload(payload: Mobile.ClipPayload): ClipboardDraft? {
            val text = partText(payload, MIME_TEXT_PLAIN)
            val html = partText(payload, MIME_TEXT_HTML)
            if (text.isNullOrEmpty() && html.isNullOrEmpty()) {
                return null
            }
            return ClipboardDraft(text = text, html = html)
        }

        /** Text of one representation, taken from the parts and then from the top fields. */
        private fun partText(payload: Mobile.ClipPayload, mime: String): String? {
            payload.partsList.firstOrNull { it.mimeType == mime }
                ?.let { return it.data.toStringUtf8() }
            if (payload.mimeType == mime && !payload.data.isEmpty) {
                return payload.data.toStringUtf8()
            }
            return null
        }
    }
}
