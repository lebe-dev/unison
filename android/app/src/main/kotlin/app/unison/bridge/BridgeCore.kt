package app.unison.bridge

import android.content.Context
import app.unison.core.CoreClient
import app.unison.core.CoreException
import app.unison.core.CoreStarter
import app.unison.core.mobile.Mobile
import unison.local.v1.Local
import unison.mobile.v1.Mobile as MobileProto

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
    suspend fun submit(payload: MobileProto.ClipPayload)
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

    override suspend fun submit(payload: MobileProto.ClipPayload) {
        requireRunning()
        Mobile.submitClip(payload.toByteArray())
    }

    private suspend fun requireRunning() {
        if (!ensureRunning()) {
            throw CoreException(CoreException.CODE_FAILED_PRECONDITION, "core is not running")
        }
    }
}
