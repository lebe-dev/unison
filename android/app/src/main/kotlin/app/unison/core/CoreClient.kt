package app.unison.core

import app.unison.core.mobile.Mobile
import app.unison.diagnostics.Diagnostics
import app.unison.history.HistoryTtlProto
import app.unison.policy.PolicyConfig
import app.unison.policy.PolicyProto
import com.google.protobuf.InvalidProtocolBufferException
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

    /** The request was refused as malformed. */
    val isInvalidArgument: Boolean
        get() = code == CODE_INVALID_ARGUMENT

    /** The device the request needs is not reachable. */
    val isUnavailable: Boolean
        get() = code == CODE_UNAVAILABLE

    companion object {
        const val CODE_INVALID_ARGUMENT = 3
        const val CODE_FAILED_PRECONDITION = 9
        const val CODE_INTERNAL = 13
        const val CODE_UNAVAILABLE = 14
    }
}

/** Client of the local API served by the core in process. */
class CoreClient(private val transport: CoreTransport = MobileTransport) : CoreApi {

    /** Version of the core library. */
    val version: String
        get() = Mobile.version()

    /**
     * Runs one local API method and returns its decoded response. Every failure,
     * including one of the facade itself or a response that does not decode,
     * comes out as a [CoreException].
     */
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
        val raw = try {
            transport.call(call.toByteArray())
        } catch (e: Exception) {
            throw CoreException(CoreException.CODE_INTERNAL, e.message ?: "core call failed")
        }
        val response = decode("response of $method") {
            if (raw == null) {
                MobileProto.CallResponse.getDefaultInstance()
            } else {
                MobileProto.CallResponse.parseFrom(raw)
            }
        }
        if (response.code != 0) {
            throw CoreException(response.code, response.message)
        }
        decode("payload of $method") { parse(response.payload.toByteArray()) }
    }

    private inline fun <T> decode(what: String, block: () -> T): T = try {
        block()
    } catch (e: InvalidProtocolBufferException) {
        Diagnostics.capture(e)
        throw CoreException(CoreException.CODE_INTERNAL, "$what does not decode: ${e.message}")
    }

    /** Version and runtime state of the core. */
    override suspend fun status(): Local.GetStatusResponse = call(
        method = "GetStatus",
        request = Local.GetStatusRequest.getDefaultInstance(),
        parse = { Local.GetStatusResponse.parseFrom(it) },
    )

    /** Stores the name this device announces to the peers. */
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

    /** Stores the whole network policy and returns the resulting settings. */
    suspend fun updatePolicy(table: PolicyConfig): Local.Settings {
        val response = call(
            method = "UpdateSettings",
            request = PolicyProto.request(table),
            parse = { Local.UpdateSettingsResponse.parseFrom(it) },
        )
        return response.settings
    }

    /** Drops the stored policy overrides and returns the defaults. */
    suspend fun resetPolicy(): Local.Settings {
        val response = call(
            method = "UpdateSettings",
            request = PolicyProto.reset(),
            parse = { Local.UpdateSettingsResponse.parseFrom(it) },
        )
        return response.settings
    }

    /** Changes the lifetime of the history items of one kind. */
    suspend fun setHistoryTtl(kind: Local.Kind, seconds: Long): Local.Settings {
        val response = call(
            method = "UpdateSettings",
            request = HistoryTtlProto.request(kind, seconds),
            parse = { Local.UpdateSettingsResponse.parseFrom(it) },
        )
        return response.settings
    }

    /** Drops the stored history lifetimes and returns the defaults. */
    suspend fun resetHistoryTtl(): Local.Settings {
        val response = call(
            method = "UpdateSettings",
            request = HistoryTtlProto.reset(),
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

    /** Current settings of the core. */
    override suspend fun settings(): Local.Settings {
        val response = call(
            method = "GetSettings",
            request = Local.GetSettingsRequest.getDefaultInstance(),
            parse = { Local.GetSettingsResponse.parseFrom(it) },
        )
        return response.settings
    }

    /** Mirrors the consent to report errors to the developer. */
    override suspend fun setSentryEnabled(enabled: Boolean): Local.Settings {
        val request = Local.UpdateSettingsRequest.newBuilder()
            .setSentryEnabled(enabled)
            .build()
        val response = call(
            method = "UpdateSettings",
            request = request,
            parse = { Local.UpdateSettingsResponse.parseFrom(it) },
        )
        return response.settings
    }

    /** Turns the application log on or off. */
    override suspend fun setJournalEnabled(enabled: Boolean): Local.Settings {
        val request = Local.UpdateSettingsRequest.newBuilder()
            .setJournalEnabled(enabled)
            .build()
        val response = call(
            method = "UpdateSettings",
            request = request,
            parse = { Local.UpdateSettingsResponse.parseFrom(it) },
        )
        return response.settings
    }

    /** Removes the tracking parameters from the links of the clipboard text. */
    override suspend fun setStripTracking(enabled: Boolean): Local.Settings {
        val request = Local.UpdateSettingsRequest.newBuilder()
            .setStripTracking(enabled)
            .build()
        val response = call(
            method = "UpdateSettings",
            request = request,
            parse = { Local.UpdateSettingsResponse.parseFrom(it) },
        )
        return response.settings
    }

    /** Relay access of the group and the state of its client. */
    override suspend fun relay(): Local.Relay {
        val response = call(
            method = "GetRelay",
            request = Local.GetRelayRequest.getDefaultInstance(),
            parse = { Local.GetRelayResponse.parseFrom(it) },
        )
        return response.relay
    }

    /** Stores the relay address and token. */
    override suspend fun setRelay(url: String, token: String?, filesOnRequest: Boolean?): Local.Relay {
        val request = Local.SetRelayRequest.newBuilder().setUrl(url)
        if (token != null) {
            request.setToken(token)
        }
        if (filesOnRequest != null) {
            request.setFilesOnRequest(filesOnRequest)
        }
        val response = call(
            method = "SetRelay",
            request = request.build(),
            parse = { Local.SetRelayResponse.parseFrom(it) },
        )
        return response.relay
    }

    /** Turns the relay off for the whole group. */
    override suspend fun relayOff(): Local.Relay {
        val response = call(
            method = "RelayOff",
            request = Local.RelayOffRequest.getDefaultInstance(),
            parse = { Local.RelayOffResponse.parseFrom(it) },
        )
        return response.relay
    }

    /** Checks the relay over each transport without storing anything. */
    override suspend fun testRelay(url: String, token: String?): Local.TestRelayResponse {
        val request = Local.TestRelayRequest.newBuilder().setUrl(url)
        if (token != null) {
            request.setToken(token)
        }
        return call(
            method = "TestRelay",
            request = request.build(),
            parse = { Local.TestRelayResponse.parseFrom(it) },
        )
    }

    /** Makes this device ask before it takes the content of the scope from any device. */
    override suspend fun setSelfConfirmation(scope: Local.ConfirmationScope, require: Boolean): Local.Settings {
        val builder = Local.UpdateSettingsRequest.newBuilder()
        when (scope) {
            Local.ConfirmationScope.CONFIRMATION_SCOPE_FILES -> builder.setRequireFileConfirmation(require)
            else -> builder.setRequireConfirmation(require)
        }
        val request = builder.build()
        val response = call(
            method = "UpdateSettings",
            request = request,
            parse = { Local.UpdateSettingsResponse.parseFrom(it) },
        )
        return response.settings
    }

    /** Changes one confirmation flag of a paired device. */
    override suspend fun setDeviceConfirmation(
        deviceId: String,
        scope: Local.ConfirmationScope,
        side: Local.ConfirmationSide,
        require: Boolean,
    ) {
        val request = Local.SetDeviceConfirmationRequest.newBuilder()
            .setDeviceId(deviceId)
            .setScope(scope)
            .setSide(side)
            .setRequire(require)
            .build()
        call(
            method = "SetDeviceConfirmation",
            request = request,
            parse = { Local.SetDeviceConfirmationResponse.parseFrom(it) },
        )
    }

    /** Removes a device from the group; the ID of this device leaves the group. */
    override suspend fun revokeDevice(deviceId: String) {
        val request = Local.RevokeDeviceRequest.newBuilder()
            .setDeviceId(deviceId)
            .build()
        call(
            method = "RevokeDevice",
            request = request,
            parse = { Local.RevokeDeviceResponse.parseFrom(it) },
        )
    }

    /** Changes the local name of a device, or the name this device announces. */
    override suspend fun renameDevice(deviceId: String, name: String): Local.Device {
        val request = Local.RenameDeviceRequest.newBuilder()
            .setDeviceId(deviceId)
            .setName(name)
            .build()
        val response = call(
            method = "RenameDevice",
            request = request,
            parse = { Local.RenameDeviceResponse.parseFrom(it) },
        )
        return response.device
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

    /** Writes an entry that waited for a confirmation to the clipboard. */
    override suspend fun applyHistoryItem(id: String) {
        val request = Local.ApplyHistoryItemRequest.newBuilder()
            .setId(id)
            .build()
        call(
            method = "ApplyHistoryItem",
            request = request,
            parse = { Local.ApplyHistoryItemResponse.parseFrom(it) },
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

    /** Every file transfer of this device, newest first. */
    override suspend fun listTransfers(): Local.ListTransfersResponse = call(
        method = "ListTransfers",
        request = Local.ListTransfersRequest.getDefaultInstance(),
        parse = { Local.ListTransfersResponse.parseFrom(it) },
    )

    /** Stops one file transfer. */
    override suspend fun cancelTransfer(id: String) {
        val request = Local.CancelTransferRequest.newBuilder()
            .setId(id)
            .build()
        call(
            method = "CancelTransfer",
            request = request,
            parse = { Local.CancelTransferResponse.parseFrom(it) },
        )
    }

    /** File offers of Friends that wait for an answer, newest first. */
    override suspend fun requests(): Local.ListRequestsResponse = call(
        method = "ListRequests",
        request = Local.ListRequestsRequest.getDefaultInstance(),
        parse = { Local.ListRequestsResponse.parseFrom(it) },
    )

    /** Accepts or declines an incoming file offer. */
    override suspend fun decideRequest(id: String, accept: Boolean) {
        val request = Local.DecideRequestRequest.newBuilder()
            .setId(id)
            .setAccept(accept)
            .build()
        call(
            method = "DecideRequest",
            request = request,
            parse = { Local.DecideRequestResponse.parseFrom(it) },
        )
    }

    /** Opens a pairing window that proposes [role] to the devices it pairs with. */
    override suspend fun openPairingWindow(role: Local.Role): Local.PairingWindow {
        val request = Local.OpenPairingWindowRequest.newBuilder()
            .setRole(role)
            .build()
        val response = call(
            method = "OpenPairingWindow",
            request = request,
            parse = { Local.OpenPairingWindowResponse.parseFrom(it) },
        )
        return response.window
    }

    /** Restarts the auto-close timer of the pairing window. */
    override suspend fun extendPairingWindow(): Local.PairingWindow {
        val response = call(
            method = "ExtendPairingWindow",
            request = Local.ExtendPairingWindowRequest.getDefaultInstance(),
            parse = { Local.ExtendPairingWindowResponse.parseFrom(it) },
        )
        return response.window
    }

    /** Closes the pairing window. */
    override suspend fun closePairingWindow() {
        call(
            method = "ClosePairingWindow",
            request = Local.ClosePairingWindowRequest.getDefaultInstance(),
            parse = { Local.ClosePairingWindowResponse.parseFrom(it) },
        )
    }

    /** Code of the open pairing window. */
    override suspend fun pairingQr(addrs: List<String>): Local.PairingQr {
        val request = Local.GetPairingQrRequest.newBuilder()
            .addAllAddrs(addrs)
            .build()
        val response = call(
            method = "GetPairingQr",
            request = request,
            parse = { Local.GetPairingQrResponse.parseFrom(it) },
        )
        return response.qr
    }

    /** Pairs with the device of a pairing code. */
    override suspend fun scanPairingQr(uri: String) {
        val request = Local.ScanPairingQrRequest.newBuilder()
            .setUri(uri)
            .build()
        call(
            method = "ScanPairingQr",
            request = request,
            parse = { Local.ScanPairingQrResponse.parseFrom(it) },
        )
    }

    /** Devices that are open for pairing. */
    override suspend fun pairingCandidates(): List<Local.PairingCandidate> {
        val response = call(
            method = "ListPairingCandidates",
            request = Local.ListPairingCandidatesRequest.getDefaultInstance(),
            parse = { Local.ListPairingCandidatesResponse.parseFrom(it) },
        )
        return response.candidatesList
    }

    /** Starts a pairing exchange with the candidate address. */
    override suspend fun startPairing(addr: String) {
        val request = Local.StartSasRequest.newBuilder()
            .setAddr(addr)
            .build()
        call(
            method = "StartSas",
            request = request,
            parse = { Local.StartSasResponse.parseFrom(it) },
        )
    }

    /** Accepts the pairing request of the device. */
    override suspend fun confirmPairing(deviceId: String) {
        val request = Local.ConfirmSasRequest.newBuilder()
            .setDeviceId(deviceId)
            .build()
        call(
            method = "ConfirmSas",
            request = request,
            parse = { Local.ConfirmSasResponse.parseFrom(it) },
        )
    }

    /** Refuses the pairing request of the device. */
    override suspend fun rejectPairing(deviceId: String) {
        val request = Local.RejectSasRequest.newBuilder()
            .setDeviceId(deviceId)
            .build()
        call(
            method = "RejectSas",
            request = request,
            parse = { Local.RejectSasResponse.parseFrom(it) },
        )
    }
}
