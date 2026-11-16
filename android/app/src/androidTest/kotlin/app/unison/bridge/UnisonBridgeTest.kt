package app.unison.bridge

import android.content.Context
import android.os.ParcelFileDescriptor
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import app.unison.core.coreEvents
import com.google.protobuf.ByteString
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import org.junit.After
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import java.util.concurrent.CountDownLatch
import java.util.concurrent.LinkedBlockingQueue
import java.util.concurrent.TimeUnit
import unison.local.v1.Local
import unison.mobile.v1.Mobile as MobileProto

/** Serves the bridge contract against a fake core. */
@RunWith(AndroidJUnit4::class)
class UnisonBridgeTest {

    private val context: Context = ApplicationProvider.getApplicationContext()
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)

    private lateinit var core: FakeCore
    private lateinit var bridge: UnisonBridge

    @Before
    fun setUp() {
        core = FakeCore()
        bridge = UnisonBridge(context, CallerVerifier.of(context), core, scope)
    }

    @After
    fun tearDown() {
        bridge.close()
        scope.cancel()
    }

    @Test
    fun reportsTheProtocolVersion() {
        assertEquals(BridgeContract.PROTOCOL_VERSION, bridge.protocolVersion())
    }

    @Test
    fun refusesAForeignCaller() {
        val foreignUid = context.packageManager.getApplicationInfo(FOREIGN_PACKAGE, 0).uid
        val foreign = UnisonBridge(
            context,
            CallerVerifier.of(context),
            core,
            scope,
            callerUid = { foreignUid },
        )
        try {
            val calls = listOf<() -> Any?>(
                { foreign.protocolVersion() },
                { foreign.publishText(payload(text = "hidden")) },
                { foreign.publishBinary(payload(mime = "image/png"), data = null) },
                { foreign.registerListener(null) },
                { foreign.unregisterListener(null) },
                { foreign.recentRemoteItems(1) },
                { foreign.openItem("item", "text/plain") },
                { foreign.setPaused(0) },
            )
            for (call in calls) {
                assertThrows(SecurityException::class.java) { call() }
            }
        } finally {
            foreign.close()
        }
    }

    @Test
    fun publishesText() {
        bridge.publishText(payload(text = "hello", html = "<b>hello</b>"))

        val submitted = core.awaitSubmit()
        assertEquals(2, submitted.partsCount)
        assertEquals(MIME_TEXT_PLAIN, submitted.getParts(0).mimeType)
        assertEquals("hello", submitted.getParts(0).data.toStringUtf8())
        assertEquals(MIME_TEXT_HTML, submitted.getParts(1).mimeType)
    }

    @Test
    fun ignoresAnEmptyTextPayload() {
        bridge.publishText(payload())
        assertTrue(core.submissions.isEmpty())
    }

    @Test
    fun publishesBinaryThroughThePipe() {
        val data = ByteArray(1024 * 1024 + 1) { (it % 251).toByte() }
        val pipe = ParcelFileDescriptor.createPipe()
        val writer = Thread {
            ParcelFileDescriptor.AutoCloseOutputStream(pipe[1]).use { it.write(data) }
        }
        writer.start()

        bridge.publishBinary(payload(mime = "image/png", size = data.size.toLong()), pipe[0])
        val submitted = core.awaitSubmit()
        writer.join(WAIT_MS)

        assertEquals("image/png", submitted.getParts(0).mimeType)
        assertArrayEquals(data, submitted.getParts(0).data.toByteArray())
    }

    @Test
    fun mapsThePauseArguments() {
        bridge.setPaused(0)
        assertEquals(Local.PauseMode.PAUSE_MODE_OFF, core.awaitPause().first)

        bridge.setPaused(-1)
        val indefinite = core.awaitPause()
        assertEquals(Local.PauseMode.PAUSE_MODE_INDEFINITE, indefinite.first)
        assertEquals(0L, indefinite.second)

        val until = System.currentTimeMillis() + TimeUnit.HOURS.toMillis(1)
        bridge.setPaused(until)
        val limited = core.awaitPause()
        assertEquals(Local.PauseMode.PAUSE_MODE_UNTIL, limited.first)
        assertEquals(until, limited.second)
    }

    @Test
    fun listsRemoteItemsOnly() {
        core.awaitStatus()
        core.history = listOf(
            historyItem("a", ORIGIN_PEER, "one"),
            historyItem("b", "self", "own text"),
            historyItem("c", ORIGIN_PEER, "two"),
        )
        bridge.registerListener(NoOpListener)

        val items = awaitRecent(2)
        assertEquals(listOf("a", "c"), items.map { it.id })
        assertEquals(BridgeContract.KIND_TEXT, items[0].kind)
        assertEquals("Laptop", items[0].deviceName)
        assertEquals(1, bridge.recentRemoteItems(1).size)
        assertTrue(bridge.recentRemoteItems(0).isEmpty())
        bridge.unregisterListener(NoOpListener)
    }

    @Test
    fun streamsTheContentOfAnItem() {
        core.entry = historyItem("a", ORIGIN_PEER, "remote text")

        val descriptor = bridge.openItem("a", "text/plain")
        assertNotNull(descriptor)
        val text = ParcelFileDescriptor.AutoCloseInputStream(descriptor!!).use {
            it.readBytes().decodeToString()
        }
        assertEquals("remote text", text)
    }

    @Test
    fun announcesPersonalItemsOnly() {
        core.awaitStatus()
        val seen = LinkedBlockingQueue<ClipSummary>()
        val listener = object : IUnisonBridgeListener.Stub() {
            override fun onRemoteItem(summary: ClipSummary?) {
                summary?.let(seen::put)
            }

            override fun onStateChanged(state: BridgeState?) = Unit
        }

        bridge.registerListener(listener)
        try {
            coreEvents.publish(event("friends", Local.Role.ROLE_FRIENDS).toByteArray())
            coreEvents.publish(event("personal", Local.Role.ROLE_PERSONAL).toByteArray())

            val summary = seen.poll(WAIT_MS, TimeUnit.MILLISECONDS)
            assertEquals("personal", summary?.id)
            assertTrue("an item of a friend was announced", seen.isEmpty())
            assertEquals(listOf("personal"), bridge.recentRemoteItems(10).map { it.id })
        } finally {
            bridge.unregisterListener(listener)
        }
    }

    @Test
    fun reportsTheStateOnRegister() {
        core.status = status(onlinePeers = 2)
        core.pause = Local.Pause.newBuilder()
            .setPaused(true)
            .setUntilUnixMs(PAUSE_UNTIL_MS)
            .build()

        val seen = CountDownLatch(1)
        var reported: BridgeState? = null
        val listener = object : IUnisonBridgeListener.Stub() {
            override fun onRemoteItem(summary: ClipSummary?) = Unit

            override fun onStateChanged(state: BridgeState?) {
                if (state != null && state.onlinePeers == 2) {
                    reported = state
                    seen.countDown()
                }
            }
        }

        bridge.registerListener(listener)
        try {
            assertTrue("no state was reported", seen.await(WAIT_MS, TimeUnit.MILLISECONDS))
            assertTrue(reported?.paused == true)
            assertEquals(PAUSE_UNTIL_MS, reported?.untilMs)
        } finally {
            bridge.unregisterListener(listener)
        }
    }

    private fun awaitRecent(count: Int): List<ClipSummary> {
        val deadline = System.currentTimeMillis() + WAIT_MS
        while (System.currentTimeMillis() < deadline) {
            val items = bridge.recentRemoteItems(20)
            if (items.size == count) {
                return items
            }
            Thread.sleep(POLL_MS)
        }
        return bridge.recentRemoteItems(20)
    }

    private fun payload(
        text: String? = null,
        html: String? = null,
        mime: String? = null,
        size: Long = -1,
    ): ClipPayload = ClipPayload(text, html, mime, size)

    private fun historyItem(id: String, origin: String, text: String): Local.HistoryItem {
        val representation = Local.Representation.newBuilder()
            .setMimeType(MIME_TEXT_PLAIN)
            .setSize(text.toByteArray().size.toLong())
            .setInlineBytes(ByteString.copyFromUtf8(text))
            .build()
        val item = Local.ClipItem.newBuilder()
            .setId(id)
            .setOrigin(origin)
            .setSeq(1)
            .setCreatedAtUnixMs(System.currentTimeMillis())
            .setKind(Local.Kind.KIND_TEXT)
            .addRepresentations(representation)
            .build()
        return Local.HistoryItem.newBuilder().setItem(item).setComplete(true).build()
    }

    /** Event of an item captured on another device. */
    private fun event(id: String, role: Local.Role): MobileProto.Event {
        val item = historyItem(id, ORIGIN_PEER, "text of $id").item
        val local = Local.Event.newBuilder()
            .setKind(Local.EventKind.EVENT_KIND_CLIPBOARD)
            .setDeviceId(ORIGIN_PEER)
            .setRole(role)
            .setItem(item)
            .build()
        return MobileProto.Event.newBuilder()
            .setType(MobileProto.EventType.EVENT_TYPE_CLIP_RECEIVED)
            .setEvent(local)
            .build()
    }

    private fun status(onlinePeers: Int): Local.GetStatusResponse {
        val builder = Local.GetStatusResponse.newBuilder().setDeviceId("self")
        repeat(onlinePeers) { index ->
            builder.addDevices(
                Local.Device.newBuilder()
                    .setDeviceId(ORIGIN_PEER + index)
                    .setName("Laptop")
                    .setOnline(true)
                    .build(),
            )
        }
        return builder.build()
    }

    /** Core that records what the bridge asks it to do. */
    private class FakeCore : BridgeCore {
        val submissions = LinkedBlockingQueue<MobileProto.ClipPayload>()
        private val pauses = LinkedBlockingQueue<Pair<Local.PauseMode, Long>>()
        private val statusCalls = CountDownLatch(1)

        @Volatile
        var status: Local.GetStatusResponse = Local.GetStatusResponse.newBuilder()
            .setDeviceId("self")
            .addDevices(
                Local.Device.newBuilder()
                    .setDeviceId(ORIGIN_PEER)
                    .setName("Laptop")
                    .setOnline(true)
                    .build(),
            )
            .build()

        @Volatile
        var pause: Local.Pause = Local.Pause.getDefaultInstance()

        @Volatile
        var history: List<Local.HistoryItem> = emptyList()

        @Volatile
        var entry: Local.HistoryItem? = null

        override suspend fun ensureRunning(): Boolean = true

        override suspend fun status(): Local.GetStatusResponse {
            statusCalls.countDown()
            return status
        }

        override suspend fun pause(): Local.Pause = pause

        override suspend fun setPause(mode: Local.PauseMode, untilUnixMs: Long): Local.Pause {
            pauses.put(mode to untilUnixMs)
            val next = Local.Pause.newBuilder()
                .setPaused(mode != Local.PauseMode.PAUSE_MODE_OFF)
                .setIndefinite(mode == Local.PauseMode.PAUSE_MODE_INDEFINITE)
                .setUntilUnixMs(if (mode == Local.PauseMode.PAUSE_MODE_UNTIL) untilUnixMs else 0)
                .build()
            pause = next
            return next
        }

        override suspend fun history(limit: Int): List<Local.HistoryItem> =
            history.take(if (limit > 0) limit else history.size)

        override suspend fun item(id: String): Local.HistoryItem? =
            entry?.takeIf { it.item.id == id }

        override suspend fun fetch(id: String, path: String): List<String> = emptyList()

        override suspend fun submit(payload: MobileProto.ClipPayload) {
            submissions.put(payload)
        }

        fun awaitStatus() {
            statusCalls.await(WAIT_MS, TimeUnit.MILLISECONDS)
        }

        fun awaitSubmit(): MobileProto.ClipPayload =
            submissions.poll(WAIT_MS, TimeUnit.MILLISECONDS)
                ?: throw AssertionError("the core received no content")

        fun awaitPause(): Pair<Local.PauseMode, Long> =
            pauses.poll(WAIT_MS, TimeUnit.MILLISECONDS)
                ?: throw AssertionError("the core received no pause")
    }

    private object NoOpListener : IUnisonBridgeListener.Stub() {
        override fun onRemoteItem(summary: ClipSummary?) = Unit

        override fun onStateChanged(state: BridgeState?) = Unit
    }

    private companion object {
        private const val MIME_TEXT_PLAIN = "text/plain"
        private const val MIME_TEXT_HTML = "text/html"
        private const val ORIGIN_PEER = "peer"
        private const val FOREIGN_PACKAGE = "android"
        private const val PAUSE_UNTIL_MS = 1_700_000_000_000L
        private const val WAIT_MS = 5_000L
        private const val POLL_MS = 20L
    }
}
