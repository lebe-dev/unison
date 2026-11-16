package app.unison.bridge

import android.content.Context
import android.util.Log
import app.unison.core.CoreClient
import app.unison.core.CoreException
import app.unison.core.CoreStarter
import app.unison.core.mobile.Mobile
import unison.local.v1.Local
import unison.mobile.v1.Mobile as MobileProto

/** Outcome of handing content to the core. */
enum class SubmitOutcome {
    ACCEPTED,
    PAUSED,
    CORE_UNAVAILABLE,
    UNSUPPORTED_TYPE,
    FAILED,
}

/** Core operations the bridge needs. */
interface BridgeCore {
    /** Starts the core when it does not answer and reports whether it does. */
    suspend fun ensureRunning(): Boolean

    suspend fun status(): Local.GetStatusResponse

    suspend fun pause(): Local.Pause

    suspend fun setPause(mode: Local.PauseMode, untilUnixMs: Long): Local.Pause

    /** History entries, newest first. */
    suspend fun history(limit: Int): List<Local.HistoryItem>

    /** One history entry, null when it is gone. */
    suspend fun item(id: String): Local.HistoryItem?

    /** Downloads the payloads of an entry and returns the written files. */
    suspend fun fetch(id: String, path: String): List<String>

    /** Sends local content to the core. */
    suspend fun submit(payload: MobileProto.ClipPayload): SubmitOutcome
}

/** Bridge core backed by the in-process local API and the gomobile facade. */
class MobileBridgeCore(
    private val context: Context,
    private val client: CoreClient = CoreClient(),
) : BridgeCore {

    override suspend fun ensureRunning(): Boolean = CoreStarter.await(context)

    override suspend fun status(): Local.GetStatusResponse = client.status()

    override suspend fun pause(): Local.Pause = client.pause()

    override suspend fun setPause(mode: Local.PauseMode, untilUnixMs: Long): Local.Pause {
        requireRunning()
        return client.setPause(mode, untilUnixMs)
    }

    override suspend fun history(limit: Int): List<Local.HistoryItem> =
        client.listHistory(limit = limit).itemsList

    override suspend fun item(id: String): Local.HistoryItem? {
        val response = client.call(
            method = "GetHistoryItem",
            request = Local.GetHistoryItemRequest.newBuilder().setId(id).build(),
            parse = Local.GetHistoryItemResponse::parseFrom,
        )
        return if (response.hasItem()) response.item else null
    }

    override suspend fun fetch(id: String, path: String): List<String> =
        client.fetchHistoryItem(id, path).filesList

    override suspend fun submit(payload: MobileProto.ClipPayload): SubmitOutcome {
        if (!ensureRunning()) {
            return SubmitOutcome.CORE_UNAVAILABLE
        }
        return try {
            Mobile.submitClip(payload.toByteArray())
            SubmitOutcome.ACCEPTED
        } catch (e: Exception) {
            Log.d(TAG, "submit failed: ${e.message}")
            outcomeOf(e.message.orEmpty())
        }
    }

    /**
     * The facade reports every failure as an exception that carries the message of the core,
     * so the outcome is read from that message.
     */
    private fun outcomeOf(message: String): SubmitOutcome = when {
        message.contains(PAUSED_MARK) -> SubmitOutcome.PAUSED
        message.contains(UNSUPPORTED_MARK) -> SubmitOutcome.UNSUPPORTED_TYPE
        message.contains(NOT_RUNNING_MARK) -> SubmitOutcome.CORE_UNAVAILABLE
        else -> SubmitOutcome.FAILED
    }

    private suspend fun requireRunning() {
        if (!ensureRunning()) {
            throw CoreException(CoreException.CODE_FAILED_PRECONDITION, "core is not running")
        }
    }

    private companion object {
        private const val TAG = "MobileBridgeCore"

        /** Parts of the error messages of clip.ErrPaused, clip.ErrUnsupportedContent and the facade. */
        private const val PAUSED_MARK = "synchronization is paused"
        private const val UNSUPPORTED_MARK = "unsupported content"
        private const val NOT_RUNNING_MARK = "core is not running"
    }
}
