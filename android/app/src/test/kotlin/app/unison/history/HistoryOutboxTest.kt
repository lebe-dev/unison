package app.unison.history

import app.unison.core.CoreClient
import app.unison.core.CoreTransport
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import unison.local.v1.Local
import unison.mobile.v1.Mobile
import java.io.File

class HistoryOutboxTest {
    @get:Rule
    val folder = TemporaryFolder()

    /** Transport that writes a payload for a fetch and records the send request. */
    private class FakeTransport(
        private val payload: Boolean = true,
    ) : CoreTransport {
        var fetchPath: String? = null
        var sendRequest: Local.SendFilesRequest? = null

        override fun call(request: ByteArray): ByteArray? {
            val call = Mobile.CallRequest.parseFrom(request)
            val payloadBytes =
                when (call.method) {
                    "FetchHistoryItem" -> {
                        val fetch = Local.FetchHistoryItemRequest.parseFrom(call.payload)
                        fetchPath = fetch.path
                        val response = Local.FetchHistoryItemResponse.newBuilder()
                        if (payload) {
                            val file = File(fetch.path, "report.pdf")
                            file.parentFile?.mkdirs()
                            file.writeText("payload")
                            response.addFiles(file.absolutePath)
                        }
                        response.build().toByteString()
                    }
                    "SendFiles" -> {
                        sendRequest = Local.SendFilesRequest.parseFrom(call.payload)
                        Local.SendFilesResponse.getDefaultInstance().toByteString()
                    }
                    else -> error("unexpected call ${call.method}")
                }
            return Mobile.CallResponse
                .newBuilder()
                .setPayload(payloadBytes)
                .build()
                .toByteArray()
        }
    }

    @Test
    fun payloadIsStagedInTheOutboxAndSent() =
        runBlocking {
            val transport = FakeTransport()
            val outbox = HistoryOutbox(folder.root, CoreClient(transport))

            outbox.send("item", "device")

            val staging = File(transport.fetchPath.orEmpty())
            assertTrue("the payload was not staged in the outbox", staging.startsWith(folder.root))

            val request = transport.sendRequest
            assertNotNull(request)
            assertEquals(listOf("device"), request?.deviceIdsList)
            assertEquals(1, request?.pathsList?.size)
            val sent = File(request?.pathsList?.single().orEmpty())
            assertTrue("the staged file was removed before the transfer", sent.isFile)
            assertEquals("payload", sent.readText())
        }

    @Test
    fun missingPayloadIsAnError() =
        runBlocking {
            val transport = FakeTransport(payload = false)
            val outbox = HistoryOutbox(folder.root, CoreClient(transport))

            val error = runCatching { outbox.send("item", "device") }.exceptionOrNull()

            assertNotNull(error)
        }

    @Test
    fun onlyOldOutboxEntriesAreRemoved() {
        val now = 1_000_000_000_000L
        val old = File(folder.root, "old").apply { mkdirs() }
        val fresh = File(folder.root, "fresh").apply { mkdirs() }
        old.setLastModified(now - 25L * 60 * 60 * 1000)
        fresh.setLastModified(now)
        val outbox = HistoryOutbox(folder.root, CoreClient(FakeTransport()), clock = { now })

        outbox.clearExpired()

        assertFalse(old.exists())
        assertTrue(fresh.exists())
    }
}
