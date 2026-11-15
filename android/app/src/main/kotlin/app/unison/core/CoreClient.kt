package app.unison.core

import app.unison.core.mobile.Mobile
import com.google.protobuf.MessageLite
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import unison.local.v1.Local
import unison.mobile.v1.Mobile as MobileProto

/** Transport of one serialized local API call. */
fun interface CoreTransport {
    fun call(request: ByteArray): ByteArray
}

/** The gomobile core as a transport. */
object MobileTransport : CoreTransport {
    override fun call(request: ByteArray): ByteArray = Mobile.call(request)
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
class CoreClient(private val transport: CoreTransport = MobileTransport) {

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
        val response = MobileProto.CallResponse.parseFrom(transport.call(call.toByteArray()))
        if (response.code != 0) {
            throw CoreException(response.code, response.message)
        }
        parse(response.payload.toByteArray())
    }

    /** Version and runtime state of the core. */
    suspend fun status(): Local.GetStatusResponse = call(
        method = "GetStatus",
        request = Local.GetStatusRequest.getDefaultInstance(),
        parse = { Local.GetStatusResponse.parseFrom(it) },
    )

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
    suspend fun setPause(mode: Local.PauseMode, untilUnixMs: Long = 0): Local.Pause {
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
}
