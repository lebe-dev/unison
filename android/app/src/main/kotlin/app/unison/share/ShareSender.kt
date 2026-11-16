package app.unison.share

import android.content.Context
import android.util.Log
import androidx.annotation.StringRes
import app.unison.R
import app.unison.clipboard.ClipboardDraft
import app.unison.core.CoreStarter
import app.unison.core.mobile.Mobile
import java.io.IOException
import kotlin.coroutines.cancellation.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import unison.mobile.v1.Mobile as MobileProto

/** Result of a share attempt with the message to show to the user. */
enum class ShareOutcome(@StringRes val message: Int) {
    STARTED(R.string.share_started),
    NOTHING(R.string.share_empty),
    TOO_MANY_FILES(R.string.share_too_many_files),
    NO_DEVICES(R.string.share_no_devices),
    CORE_UNAVAILABLE(R.string.error_core_stopped),
    FAILED(R.string.share_failed),
}

/** Hands shared content over to the core. */
object ShareSender {
    private const val TAG = "ShareSender"

    /**
     * Sends the content to the given devices and reports what happened.
     * Text goes to every online Personal device, files only to the listed ones.
     */
    suspend fun send(context: Context, content: ShareContent, deviceIds: List<String>): ShareOutcome {
        when (content) {
            is ShareContent.Text -> if (content.draft.isEmpty) return ShareOutcome.NOTHING
            is ShareContent.Files -> {
                if (content.files.isEmpty()) {
                    return ShareOutcome.NOTHING
                }
                if (content.files.size > ShareIntents.MAX_FILES) {
                    return ShareOutcome.TOO_MANY_FILES
                }
            }
        }
        if (deviceIds.isEmpty()) {
            return ShareOutcome.NO_DEVICES
        }
        if (!CoreStarter.await(context)) {
            return ShareOutcome.CORE_UNAVAILABLE
        }
        return try {
            when (content) {
                is ShareContent.Text -> submitText(content.draft)
                is ShareContent.Files -> submitFiles(context, content.files, deviceIds)
            }
            ShareOutcome.STARTED
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            Log.w(TAG, "share failed: ${e.message}")
            ShareOutcome.FAILED
        }
    }

    private suspend fun submitText(draft: ClipboardDraft) {
        val payload = draft.payload().toByteArray()
        withContext(Dispatchers.IO) { Mobile.submitClip(payload) }
    }

    /** Sends the files one by one, the core copies every descriptor while it takes it. */
    private suspend fun submitFiles(context: Context, files: List<SharedFile>, deviceIds: List<String>) {
        for (file in files) {
            submitFile(context, file, deviceIds)
        }
    }

    private suspend fun submitFile(context: Context, file: SharedFile, deviceIds: List<String>) {
        val meta = MobileProto.FilePayload.newBuilder()
            .setFileName(file.name)
            .setMimeType(file.mimeType)
            .setSize(file.size)
            .addAllDeviceIds(deviceIds)
            .build()
        val payload = meta.toByteArray()

        withContext(Dispatchers.IO) {
            val descriptor = context.contentResolver.openFileDescriptor(file.uri, READ_MODE)
                ?: throw IOException("cannot open the shared file")
            // The descriptor belongs to the core from this point on.
            val fd = try {
                descriptor.detachFd()
            } catch (e: Exception) {
                descriptor.close()
                throw e
            }
            Mobile.submitFile(payload, fd.toLong())
        }
    }

    private const val READ_MODE = "r"
}
