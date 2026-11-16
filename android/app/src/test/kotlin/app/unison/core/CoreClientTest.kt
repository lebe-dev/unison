package app.unison.core

import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
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
    fun statusSendsMethodAndDecodesResponse() = runBlocking {
        val status = Local.GetStatusResponse.newBuilder()
            .setDeviceId("abcd")
            .setDeviceName("Pixel")
            .setRunning(true)
            .build()
        val transport = FakeTransport { Mobile.CallResponse.newBuilder().setPayload(status.toByteString()).build() }

        val response = CoreClient(transport).status()

        assertEquals("Pixel", response.deviceName)
        assertEquals("abcd", response.deviceId)
        assertTrue(response.running)
        assertEquals("GetStatus", transport.lastRequest?.method)
        assertEquals(0, transport.lastRequest?.payload?.size())
    }

    @Test
    fun failureBecomesCoreException() = runBlocking {
        val transport = FakeTransport {
            Mobile.CallResponse.newBuilder()
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
    fun malformedEventIsDropped() {
        assertNull(decodeEvent(byteArrayOf(0xFF.toByte(), 0xFF.toByte())))
    }

    @Test
    fun emptyResponseIsAccepted() = runBlocking {
        val transport = FakeTransport { null }

        CoreClient(transport).deleteHistoryItem("item")

        assertEquals("DeleteHistoryItem", transport.lastRequest?.method)
    }
}
