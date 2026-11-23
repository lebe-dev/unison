package app.unison.core

import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import unison.local.v1.Local
import unison.mobile.v1.Mobile

class CoreClientTest {
    private class FakeTransport(
        private val respond: (Mobile.CallRequest) -> Mobile.CallResponse?,
    ) : CoreTransport {
        var lastRequest: Mobile.CallRequest? = null

        override fun call(request: ByteArray): ByteArray? {
            val call = Mobile.CallRequest.parseFrom(request)
            lastRequest = call
            return respond(call)?.toByteArray()
        }
    }

    @Test
    fun statusSendsMethodAndDecodesResponse() =
        runBlocking {
            val status =
                Local.GetStatusResponse
                    .newBuilder()
                    .setDeviceId("abcd")
                    .setDeviceName("Pixel")
                    .setRunning(true)
                    .build()
            val transport =
                FakeTransport {
                    Mobile.CallResponse
                        .newBuilder()
                        .setPayload(status.toByteString())
                        .build()
                }

            val response = CoreClient(transport).status()

            assertEquals("Pixel", response.deviceName)
            assertEquals("abcd", response.deviceId)
            assertTrue(response.running)
            assertEquals("GetStatus", transport.lastRequest?.method)
            assertEquals(0, transport.lastRequest?.payload?.size())
        }

    @Test
    fun failureBecomesCoreException() =
        runBlocking {
            val transport =
                FakeTransport {
                    Mobile.CallResponse
                        .newBuilder()
                        .setCode(CoreException.CODE_FAILED_PRECONDITION)
                        .setMessage("mobile: core is not running")
                        .build()
                }

            val error = runCatching { CoreClient(transport).status() }.exceptionOrNull()

            assertTrue(error is CoreException)
            error as CoreException
            assertEquals(CoreException.CODE_FAILED_PRECONDITION, error.code)
            assertTrue(error.isNotRunning)
            assertEquals("mobile: core is not running", error.message)
        }

    @Test
    fun transportFailureBecomesCoreException() =
        runBlocking {
            val transport = CoreTransport { throw IllegalStateException("mobile: panic: boom") }

            val error = runCatching { CoreClient(transport).status() }.exceptionOrNull()

            assertTrue(error is CoreException)
            error as CoreException
            assertEquals(CoreException.CODE_INTERNAL, error.code)
            assertEquals("mobile: panic: boom", error.message)
        }

    @Test
    fun undecodableResponseBecomesCoreException() =
        runBlocking {
            val transport = CoreTransport { byteArrayOf(0xFF.toByte(), 0xFF.toByte()) }

            val error = runCatching { CoreClient(transport).status() }.exceptionOrNull()

            assertTrue(error is CoreException)
            assertEquals(CoreException.CODE_INTERNAL, (error as CoreException).code)
        }

    @Test
    fun undecodablePayloadBecomesCoreException() =
        runBlocking {
            val transport =
                CoreTransport {
                    Mobile.CallResponse
                        .newBuilder()
                        .setPayload(
                            com.google.protobuf.ByteString
                                .copyFrom(byteArrayOf(0xFF.toByte(), 0xFF.toByte())),
                        ).build()
                        .toByteArray()
                }

            val error = runCatching { CoreClient(transport).status() }.exceptionOrNull()

            assertTrue(error is CoreException)
            assertEquals(CoreException.CODE_INTERNAL, (error as CoreException).code)
        }

    @Test
    fun malformedEventIsDropped() {
        assertNull(decodeEvent(byteArrayOf(0xFF.toByte(), 0xFF.toByte())))
    }

    @Test
    fun emptyResponseIsAccepted() =
        runBlocking {
            val transport = FakeTransport { null }

            CoreClient(transport).deleteHistoryItem("item")

            assertEquals("DeleteHistoryItem", transport.lastRequest?.method)
        }

    @Test
    fun answerSendsTheRequestIdAndTheDecision() =
        runBlocking {
            val transport = FakeTransport { null }

            CoreClient(transport).decideRequest("request-id", accept = false)

            assertEquals("DecideRequest", transport.lastRequest?.method)
            val request = Local.DecideRequestRequest.parseFrom(transport.lastRequest?.payload)
            assertEquals("request-id", request.id)
            assertTrue(!request.accept)
        }

    @Test
    fun requestsAreDecoded() =
        runBlocking {
            val response =
                Local.ListRequestsResponse
                    .newBuilder()
                    .addRequests(
                        Local.ShareRequest
                            .newBuilder()
                            .setId("one")
                            .setDeviceName("Friend")
                            .build(),
                    ).build()
            val transport =
                FakeTransport {
                    Mobile.CallResponse
                        .newBuilder()
                        .setPayload(response.toByteString())
                        .build()
                }

            val requests = CoreClient(transport).requests()

            assertEquals("ListRequests", transport.lastRequest?.method)
            assertEquals("Friend", requests.requestsList.single().deviceName)
        }

    @Test
    fun peerConfirmationSendsTheFlag() =
        runBlocking {
            val transport = FakeTransport { null }

            CoreClient(transport).setDeviceConfirmation(
                deviceId = "abcd",
                scope = Local.ConfirmationScope.CONFIRMATION_SCOPE_FILES,
                side = Local.ConfirmationSide.CONFIRMATION_SIDE_PEER,
                require = true,
            )

            assertEquals("SetDeviceConfirmation", transport.lastRequest?.method)
            val request = Local.SetDeviceConfirmationRequest.parseFrom(transport.lastRequest?.payload)
            assertEquals("abcd", request.deviceId)
            assertTrue(request.require)
            assertEquals(Local.ConfirmationScope.CONFIRMATION_SCOPE_FILES, request.scope)
            assertEquals(Local.ConfirmationSide.CONFIRMATION_SIDE_PEER, request.side)
        }

    @Test
    fun selfFileConfirmationUpdatesTheSettings() =
        runBlocking {
            val transport = FakeTransport { null }

            CoreClient(transport).setSelfConfirmation(Local.ConfirmationScope.CONFIRMATION_SCOPE_FILES, require = true)

            assertEquals("UpdateSettings", transport.lastRequest?.method)
            val request = Local.UpdateSettingsRequest.parseFrom(transport.lastRequest?.payload)
            assertTrue(request.hasRequireFileConfirmation())
            assertTrue(request.requireFileConfirmation)
            assertTrue(!request.hasRequireConfirmation())
        }

    @Test
    fun deviceRemoteSettingsAreSent() =
        runBlocking {
            val transport = FakeTransport { null }

            CoreClient(transport).setDeviceRemoteSettings("abcd", allow = false)

            assertEquals("SetDeviceRemoteSettings", transport.lastRequest?.method)
            val request = Local.SetDeviceRemoteSettingsRequest.parseFrom(transport.lastRequest?.payload)
            assertEquals("abcd", request.deviceId)
            assertFalse(request.allow)
        }

    @Test
    fun globalRemoteSettingsUpdateTheSettings() =
        runBlocking {
            val settings =
                Local.Settings
                    .newBuilder()
                    .setAllowRemoteSettings(false)
                    .build()
            val transport =
                FakeTransport {
                    Mobile.CallResponse
                        .newBuilder()
                        .setPayload(
                            Local.UpdateSettingsResponse
                                .newBuilder()
                                .setSettings(settings)
                                .build()
                                .toByteString(),
                        ).build()
                }

            val response = CoreClient(transport).setAllowRemoteSettings(false)

            assertEquals("UpdateSettings", transport.lastRequest?.method)
            val request = Local.UpdateSettingsRequest.parseFrom(transport.lastRequest?.payload)
            assertTrue(request.hasAllowRemoteSettings())
            assertFalse(request.allowRemoteSettings)
            assertFalse(response.allowRemoteSettings)
        }

    @Test
    fun revokeSendsTheDeviceId() =
        runBlocking {
            val transport = FakeTransport { null }

            CoreClient(transport).revokeDevice("abcd")

            assertEquals("RevokeDevice", transport.lastRequest?.method)
            assertEquals("abcd", Local.RevokeDeviceRequest.parseFrom(transport.lastRequest?.payload).deviceId)
        }

    @Test
    fun applySendsTheItemId() =
        runBlocking {
            val transport = FakeTransport { null }

            CoreClient(transport).applyHistoryItem("item")

            assertEquals("ApplyHistoryItem", transport.lastRequest?.method)
            assertEquals("item", Local.ApplyHistoryItemRequest.parseFrom(transport.lastRequest?.payload).id)
        }
}
