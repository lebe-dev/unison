package app.unison.core

import app.unison.core.mobile.Mobile
import com.google.protobuf.MessageLite
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import unison.local.v1.Local
import unison.mobile.v1.Mobile as MobileProto

/** Transport of one serialized local API call. */
fun interface CoreTransport {
    /** Returns the response, or null when the core sent an empty one. */
    fun call(request: ByteArray): ByteArray?
}

/** The gomobile core as a transport. */
object MobileTransport : CoreTransport {
    override fun call(request: ByteArray): ByteArray? = Mobile.call(request)
}

/** Failure reported by the core in place of a response. */
class CoreException(val code: Int, message: String) : Exception(message) {
    /** The core is not running, usually because the service stopped. */
    val isNotRunning: Boolean
        get() = code == CODE_FAILED_PRECONDITION

    companion object {
        const val CODE_FAILED_PRECONDITION = 9
    }
}

/** Client of the local API served by the core in process. */
class CoreClient(private val transport: CoreTransport = MobileTransport) : CoreApi {

    /** Version of the core library. */
    val version: String
        get() = Mobile.version()

    /** Runs one local API method and returns its decoded response. */
    suspend fun <T> call(
        method: String,
        request: MessageLite,
        parse: (ByteArray) -> T,
    ): T = withContext(Dispatchers.IO) {
        val call = MobileProto.CallRequest.newBuilder()
            .setMethod(method)
            .setPayload(request.toByteString())
            .build()
        // An empty response arrives as a null array, a failed one as a code.
        val raw = transport.call(call.toByteArray())
        val response = if (raw == null) {
            MobileProto.CallResponse.getDefaultInstance()
        } else {
            MobileProto.CallResponse.parseFrom(raw)
        }
        if (response.code != 0) {
            throw CoreException(response.code, response.message)
        }
        parse(response.payload.toByteArray())
    }

    /** Version and runtime state of the core. */
    override suspend fun status(): Local.GetStatusResponse = call(
        method = "GetStatus",
        request = Local.GetStatusRequest.getDefaultInstance(),
        parse = { Local.GetStatusResponse.parseFrom(it) },
    )

    /** Stores the device name used from the next start. */
    override suspend fun setDeviceName(name: String): Local.Settings {
        val request = Local.UpdateSettingsRequest.newBuilder()
            .setDeviceName(name)
            .build()
        val response = call(
            method = "UpdateSettings",
            request = request,
            parse = { Local.UpdateSettingsResponse.parseFrom(it) },
        )
        return response.settings
    }

    /** Current pause of the clipboard synchronization. */
    suspend fun pause(): Local.Pause {
        val response = call(
            method = "GetPause",
            request = Local.GetPauseRequest.getDefaultInstance(),
            parse = { Local.GetPauseResponse.parseFrom(it) },
        )
        return response.pause
    }

    /** Changes the pause of the clipboard synchronization. */
    override suspend fun setPause(mode: Local.PauseMode, untilUnixMs: Long): Local.Pause {
        val request = Local.SetPauseRequest.newBuilder()
            .setMode(mode)
            .setUntilUnixMs(untilUnixMs)
            .build()
        val response = call(
            method = "SetPause",
            request = request,
            parse = { Local.SetPauseResponse.parseFrom(it) },
        )
        return response.pause
    }

    /** Live history entries, newest first. */
    override suspend fun listHistory(
        kind: Local.Kind,
        deviceId: String,
        limit: Int,
    ): Local.ListHistoryResponse {
        val request = Local.ListHistoryRequest.newBuilder()
            .setKind(kind)
            .setDeviceId(deviceId)
            .setLimit(limit)
            .build()
        return call(
            method = "ListHistory",
            request = request,
            parse = { Local.ListHistoryResponse.parseFrom(it) },
        )
    }

    /** Writes one history entry back to the clipboard. */
    override suspend fun copyHistoryItem(id: String) {
        val request = Local.CopyHistoryItemRequest.newBuilder()
            .setId(id)
            .build()
        call(
            method = "CopyHistoryItem",
            request = request,
            parse = { Local.CopyHistoryItemResponse.parseFrom(it) },
        )
    }

    /** Deletes one history entry on every device. */
    override suspend fun deleteHistoryItem(id: String) {
        val request = Local.DeleteHistoryItemRequest.newBuilder()
            .setId(id)
            .build()
        call(
            method = "DeleteHistoryItem",
            request = request,
            parse = { Local.DeleteHistoryItemResponse.parseFrom(it) },
        )
    }

    /** Downloads the payloads of one entry into [path]. */
    override suspend fun fetchHistoryItem(id: String, path: String): Local.FetchHistoryItemResponse {
        val request = Local.FetchHistoryItemRequest.newBuilder()
            .setId(id)
            .setPath(path)
            .build()
        return call(
            method = "FetchHistoryItem",
            request = request,
            parse = { Local.FetchHistoryItemResponse.parseFrom(it) },
        )
    }
}
