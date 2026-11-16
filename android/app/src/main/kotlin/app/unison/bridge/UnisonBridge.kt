package app.unison.bridge

import android.content.Context
import android.os.Binder
import android.os.ParcelFileDescriptor
import android.os.RemoteCallbackList
import android.os.SystemClock
import android.util.Log
import app.unison.core.coreEvents
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import java.io.File
import unison.local.v1.Local
import unison.mobile.v1.Mobile as MobileProto

/** Server side of the bridge contract with the keyboard. */
class UnisonBridge(
    context: Context,
    private val verifier: CallerVerifier,
    private val core: BridgeCore,
    private val scope: CoroutineScope,
    private val callerUid: () -> Int = { Binder.getCallingUid() },
) : IUnisonBridge.Stub() {

    private val listeners = RemoteCallbackList<IUnisonBridgeListener>()
    private val content = BridgeContent(core, scope, File(context.cacheDir, CACHE_DIR))
    private val refresh = Mutex()

    @Volatile
    private var state = BridgeState(false, 0, 0)

    @Volatile
    private var recent: List<ClipSummary> = emptyList()

    @Volatile
    private var ownDeviceId = ""

    @Volatile
    private var deviceNames: Map<String, String> = emptyMap()

    private var pollJob: Job? = null

    @Volatile
    private var lastRefreshMs = 0L

    private val eventsJob: Job = scope.launch {
        coreEvents.events.collect(::onCoreEvent)
    }

    init {
        scope.launch { refreshAll() }
    }

    override fun protocolVersion(): Int {
        checkCaller()
        return BridgeContract.PROTOCOL_VERSION
    }

    override fun publishText(payload: ClipPayload?) {
        checkCaller()
        val item = payload?.let(::textPayload) ?: return
        submit(item)
    }

    override fun publishBinary(meta: ClipPayload?, data: ParcelFileDescriptor?) {
        checkCaller()
        if (data == null) {
            return
        }
        val mime = meta?.mime?.takeIf { it.isNotEmpty() } ?: DEFAULT_BINARY_MIME
        scope.launch(Dispatchers.IO) {
            val guard = scope.launch {
                delay(READ_TIMEOUT_MS)
                Log.w(TAG, "the caller did not finish the content in time")
                data.closeQuietly(TAG)
            }
            val bytes = try {
                ParcelFileDescriptor.AutoCloseInputStream(data).use {
                    BridgeBinary.read(it, MAX_BINARY_BYTES)
                }
            } catch (e: Exception) {
                Log.w(TAG, "reading the content failed: ${e.message}")
                return@launch
            } finally {
                guard.cancel()
            }
            if (bytes == null) {
                Log.w(TAG, "binary content is larger than $MAX_BINARY_BYTES bytes")
                return@launch
            }
            submit(binaryPayload(mime, bytes))
        }
    }

    override fun registerListener(listener: IUnisonBridgeListener?) {
        checkCaller()
        if (listener == null) {
            return
        }
        listeners.register(listener)
        sendState(listener)
        scope.launch { refreshAll() }
        startPolling()
    }

    override fun unregisterListener(listener: IUnisonBridgeListener?) {
        checkCaller()
        if (listener == null) {
            return
        }
        listeners.unregister(listener)
        if (listeners.registeredCallbackCount == 0) {
            stopPolling()
        }
    }

    override fun recentRemoteItems(limit: Int): MutableList<ClipSummary> {
        checkCaller()
        if (limit <= 0) {
            return mutableListOf()
        }
        return recent.take(limit.coerceAtMost(MAX_RECENT_ITEMS)).toMutableList()
    }

    override fun openItem(itemId: String?, mime: String?): ParcelFileDescriptor? {
        checkCaller()
        if (itemId.isNullOrEmpty()) {
            return null
        }
        return content.open(itemId, mime.orEmpty())
    }

    override fun setPaused(untilEpochMs: Long) {
        checkCaller()
        scope.launch {
            try {
                val pause = when {
                    untilEpochMs > 0 -> core.setPause(Local.PauseMode.PAUSE_MODE_UNTIL, untilEpochMs)
                    untilEpochMs == 0L -> core.setPause(Local.PauseMode.PAUSE_MODE_OFF, 0)
                    else -> core.setPause(Local.PauseMode.PAUSE_MODE_INDEFINITE, 0)
                }
                updateState(BridgeState(pause.paused, untilOf(pause), state.onlinePeers))
            } catch (e: Exception) {
                Log.w(TAG, "pause request failed: ${e.message}")
            }
        }
    }

    /** Stops the background work of the bridge. */
    fun close() {
        stopPolling()
        eventsJob.cancel()
        listeners.kill()
    }

    private fun checkCaller() {
        verifier.verify(callerUid())
    }

    private fun submit(payload: MobileProto.ClipPayload) {
        scope.launch {
            try {
                core.submit(payload)
            } catch (e: Exception) {
                Log.w(TAG, "submit failed: ${e.message}")
                return@launch
            }
            refreshAfterAction()
        }
    }

    /** Refreshes the cache after a user action, at most once per interval. */
    private suspend fun refreshAfterAction() {
        val now = SystemClock.elapsedRealtime()
        if (now - lastRefreshMs < ACTION_REFRESH_MS) {
            return
        }
        lastRefreshMs = now
        refreshAll()
    }

    /** Text and HTML parts of a payload, null when it carries no text. */
    private fun textPayload(source: ClipPayload): MobileProto.ClipPayload? {
        val builder = MobileProto.ClipPayload.newBuilder()
        var present = false
        val text = source.text
        if (!text.isNullOrEmpty()) {
            builder.addParts(part(MIME_TEXT_PLAIN, text.toByteArray()))
            present = true
        }
        val html = source.html
        if (!html.isNullOrEmpty()) {
            builder.addParts(part(MIME_TEXT_HTML, html.toByteArray()))
            present = true
        }
        return if (present) builder.build() else null
    }

    private fun binaryPayload(mime: String, bytes: ByteArray): MobileProto.ClipPayload =
        MobileProto.ClipPayload.newBuilder()
            .setMimeType(mime)
            .setData(com.google.protobuf.ByteString.copyFrom(bytes))
            .addParts(part(mime, bytes))
            .build()

    private fun part(mime: String, bytes: ByteArray): MobileProto.ClipPart =
        MobileProto.ClipPart.newBuilder()
            .setMimeType(mime)
            .setData(com.google.protobuf.ByteString.copyFrom(bytes))
            .build()

    /** Reacts to events that change what the keyboard shows. */
    private fun onCoreEvent(event: MobileProto.Event) {
        if (!event.hasEvent()) {
            return
        }
        when (event.event.kind) {
            Local.EventKind.EVENT_KIND_CLIPBOARD,
            Local.EventKind.EVENT_KIND_CLIP_DOWNLOAD,
            Local.EventKind.EVENT_KIND_CLIP_APPLY -> onRemoteItem(event.event)
            Local.EventKind.EVENT_KIND_PEER_ONLINE,
            Local.EventKind.EVENT_KIND_PEER_OFFLINE -> scope.launch { refreshState() }
            else -> Unit
        }
    }

    /** Announces an item that came from another device. */
    private fun onRemoteItem(event: Local.Event) {
        val item = event.item
        if (item.id.isEmpty() || item.origin.isEmpty() || item.origin == ownDeviceId) {
            return
        }
        if (event.role != Local.Role.ROLE_UNSPECIFIED && event.role != Local.Role.ROLE_PERSONAL) {
            return
        }
        scope.launch {
            addRecent(BridgeSummaries.of(item, deviceName(event.deviceId.ifEmpty { item.origin })))
            dispatchItem(recent.firstOrNull { it.id == item.id } ?: return@launch)
        }
    }

    private fun addRecent(summary: ClipSummary) {
        recent = (listOf(summary) + recent.filter { it.id != summary.id }).take(MAX_RECENT_ITEMS)
    }

    private fun deviceName(deviceId: String): String = deviceNames[deviceId].orEmpty()

    private suspend fun refreshAll() {
        refresh.withLock {
            val status = loadStatus() ?: return
            recent = loadRecent(status.deviceId)
        }
    }

    private suspend fun refreshState() {
        refresh.withLock {
            loadStatus()
        }
    }

    /** Reads the status and the pause, then publishes the state. */
    private suspend fun loadStatus(): Local.GetStatusResponse? = try {
        val status = core.status()
        ownDeviceId = status.deviceId
        deviceNames = status.devicesList.associate { it.deviceId to it.name }
        val pause = try {
            core.pause()
        } catch (e: Exception) {
            Log.d(TAG, "pause state is not available: ${e.message}")
            null
        }
        updateState(
            BridgeState(
                pause?.paused == true,
                untilOf(pause),
                status.devicesList.count { it.online },
            ),
        )
        status
    } catch (e: Exception) {
        Log.d(TAG, "core is not available: ${e.message}")
        null
    }

    /** Items of other devices, newest first. */
    private suspend fun loadRecent(ownId: String): List<ClipSummary> {
        val entries = loadHistory() ?: return recent
        return entries
            .map { it.item }
            .filter { it.id.isNotEmpty() && !it.deleted && it.origin.isNotEmpty() && it.origin != ownId }
            .map { BridgeSummaries.of(it, deviceName(it.origin)) }
            .take(MAX_RECENT_ITEMS)
    }

    /** Reads the history with a smaller limit when one response is too large. */
    private suspend fun loadHistory(): List<Local.HistoryItem>? {
        for (limit in HISTORY_LIMITS) {
            try {
                return core.history(limit)
            } catch (e: Exception) {
                Log.d(TAG, "history of $limit entries is not available: ${e.message}")
            }
        }
        return null
    }

    private fun updateState(next: BridgeState) {
        val previous = state
        state = next
        if (previous.paused == next.paused &&
            previous.untilMs == next.untilMs &&
            previous.onlinePeers == next.onlinePeers
        ) {
            return
        }
        dispatchState(next)
    }

    private fun dispatchState(value: BridgeState) {
        val count = listeners.beginBroadcast()
        try {
            for (index in 0 until count) {
                try {
                    listeners.getBroadcastItem(index).onStateChanged(value)
                } catch (e: Exception) {
                    Log.w(TAG, "state delivery failed: ${e.message}")
                }
            }
        } finally {
            listeners.finishBroadcast()
        }
    }

    private fun dispatchItem(summary: ClipSummary) {
        val count = listeners.beginBroadcast()
        try {
            for (index in 0 until count) {
                try {
                    listeners.getBroadcastItem(index).onRemoteItem(summary)
                } catch (e: Exception) {
                    Log.w(TAG, "item delivery failed: ${e.message}")
                }
            }
        } finally {
            listeners.finishBroadcast()
        }
    }

    private fun sendState(listener: IUnisonBridgeListener) {
        try {
            listener.onStateChanged(state)
        } catch (e: Exception) {
            Log.w(TAG, "state delivery failed: ${e.message}")
        }
    }

    /** Polls the pause and the peer count while a listener is registered. */
    private fun startPolling() {
        if (pollJob?.isActive == true) {
            return
        }
        pollJob = scope.launch {
            while (true) {
                delay(POLL_MS)
                refreshState()
            }
        }
    }

    private fun stopPolling() {
        pollJob?.cancel()
        pollJob = null
    }

    private fun untilOf(pause: Local.Pause?): Long =
        if (pause != null && pause.paused) pause.untilUnixMs else 0

    private companion object {
        private const val TAG = "UnisonBridge"

        /** Directory of the temporary payloads inside the cache. */
        private const val CACHE_DIR = "bridge"

        /** How many items the keyboard may ask for at once. */
        private const val MAX_RECENT_ITEMS = 20

        /** Entry counts the history is read with, largest first. */
        private val HISTORY_LIMITS = intArrayOf(20, 5, 1)

        /** Largest binary payload accepted through the pipe. */
        private const val MAX_BINARY_BYTES = 16 * 1024 * 1024

        /** How long the caller may keep the write end open. */
        private const val READ_TIMEOUT_MS = 30_000L

        /** Time between two reads of the pause and the peer count. */
        private const val POLL_MS = 30_000L

        /** Shortest gap between two cache refreshes caused by a user action. */
        private const val ACTION_REFRESH_MS = 5_000L

        private const val MIME_TEXT_PLAIN = "text/plain"
        private const val MIME_TEXT_HTML = "text/html"
        private const val DEFAULT_BINARY_MIME = "application/octet-stream"
    }
}
