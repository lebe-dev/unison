package app.unison.clipboard

import android.content.Context
import android.util.Log
import androidx.annotation.StringRes
import app.unison.R
import app.unison.core.CoreStarter
import app.unison.core.SubmitFailure
import app.unison.core.mobile.Mobile
import app.unison.diagnostics.Journal
import app.unison.service.UnisonSettings
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlin.coroutines.cancellation.CancellationException

/** Result of a send attempt, with the message to show to the user. */
enum class SendOutcome(
    @StringRes val message: Int,
) {
    SENT(R.string.clipboard_sent),
    EMPTY(R.string.clipboard_empty),
    SENSITIVE(R.string.clipboard_sensitive),
    UNSUPPORTED(R.string.clipboard_unsupported),
    PAUSED(R.string.clipboard_paused),
    CORE_UNAVAILABLE(R.string.clipboard_core_unavailable),
    FAILED(R.string.clipboard_failed),
}

/** Sends the local clipboard to the paired devices. */
object ClipboardSender {
    private const val TAG = "ClipboardSender"

    /** Reads the clipboard and sends it. The caller must be in the foreground. */
    suspend fun sendCurrent(context: Context): SendOutcome {
        val draft =
            when (val read = ClipboardReader.read(context)) {
                is ClipboardRead.Draft -> read.draft
                ClipboardRead.Empty -> return SendOutcome.EMPTY
                ClipboardRead.Sensitive -> return SendOutcome.SENSITIVE
                ClipboardRead.Unsupported -> return SendOutcome.UNSUPPORTED
            }
        return send(context, draft)
    }

    /** Sends content that was read before. */
    suspend fun send(
        context: Context,
        draft: ClipboardDraft,
    ): SendOutcome {
        if (draft.isEmpty) {
            return SendOutcome.EMPTY
        }
        if (!CoreStarter.await(context)) {
            return SendOutcome.CORE_UNAVAILABLE
        }
        if (ClipboardPause.read()?.paused == true) {
            return SendOutcome.PAUSED
        }
        try {
            withContext(Dispatchers.IO) { Mobile.submitClip(draft.payload().toByteArray()) }
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            Log.w(TAG, "submit failed: ${e.message}")
            Journal.warn("clipboard submit failed", "error" to e.message)
            // The pause may start between the check above and the submit.
            return when (SubmitFailure.of(e)) {
                SubmitFailure.PAUSED -> SendOutcome.PAUSED
                SubmitFailure.UNSUPPORTED -> SendOutcome.UNSUPPORTED
                SubmitFailure.NOT_RUNNING -> SendOutcome.CORE_UNAVAILABLE
                SubmitFailure.OTHER -> SendOutcome.FAILED
            }
        }
        UnisonSettings.setLastClipHash(context, draft.contentHash())
        return SendOutcome.SENT
    }

    /** True when the clipboard holds content that was not sent or applied before. */
    fun isNew(
        context: Context,
        draft: ClipboardDraft,
    ): Boolean = draft.contentHash() != UnisonSettings.lastClipHash(context)
}
