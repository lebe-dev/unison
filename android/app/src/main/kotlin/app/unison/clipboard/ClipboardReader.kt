package app.unison.clipboard

import android.content.ClipboardManager
import android.content.Context
import android.text.Html

/** What the local clipboard holds. */
sealed interface ClipboardRead {
    /** Text content that can be sent. */
    data class Draft(
        val draft: ClipboardDraft,
    ) : ClipboardRead

    /** Nothing to send. */
    data object Empty : ClipboardRead

    /** Content the user marked as sensitive. */
    data object Sensitive : ClipboardRead

    /** Content without text, such as an image. */
    data object Unsupported : ClipboardRead
}

/**
 * Reads the local clipboard. Only the focused app may read it, so the caller
 * must already be in the foreground.
 */
object ClipboardReader {
    /** ClipDescription.EXTRA_IS_SENSITIVE, available from API 33. */
    private const val EXTRA_IS_SENSITIVE = "android.content.extra.IS_SENSITIVE"

    fun read(context: Context): ClipboardRead {
        val manager =
            context.getSystemService(ClipboardManager::class.java)
                ?: return ClipboardRead.Empty
        if (!manager.hasPrimaryClip()) {
            return ClipboardRead.Empty
        }
        val description = manager.primaryClipDescription
        if (description?.extras?.getBoolean(EXTRA_IS_SENSITIVE, false) == true) {
            return ClipboardRead.Sensitive
        }

        val clip = manager.primaryClip ?: return ClipboardRead.Empty
        if (clip.itemCount == 0) {
            return ClipboardRead.Empty
        }
        val item = clip.getItemAt(0)
        val html = item.htmlText
        val text = item.text?.toString() ?: html?.let(::plainTextOf)
        if (text.isNullOrEmpty() && html.isNullOrEmpty()) {
            return if (item.uri == null) ClipboardRead.Empty else ClipboardRead.Unsupported
        }
        return ClipboardRead.Draft(ClipboardDraft(text = text, html = html))
    }

    /** Plain text of an HTML fragment. */
    internal fun plainTextOf(html: String): String = Html.fromHtml(html, Html.FROM_HTML_MODE_LEGACY).toString()
}
