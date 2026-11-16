package app.unison.ui.state

import app.unison.R
import app.unison.clipboard.PauseRequest
import app.unison.core.CoreApi
import app.unison.core.CoreException
import app.unison.history.HistorySaver
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import unison.local.v1.Local
import unison.mobile.v1.Mobile

@OptIn(kotlinx.coroutines.ExperimentalCoroutinesApi::class)
class AppStateTest {

    private class FakeCore : CoreApi {
        var response: Local.GetStatusResponse = sampleStatus()
        var failure: Exception? = null
        var pauseFailure: Exception? = null
        var statusCalls = 0
        val pauses = mutableListOf<Pair<Local.PauseMode, Long>>()
        val names = mutableListOf<String>()
        var history: List<Local.HistoryItem> = emptyList()
        var historyFailure: Exception? = null
        val historyCalls = mutableListOf<Pair<Local.Kind, String>>()
        val copied = mutableListOf<String>()
        val deleted = mutableListOf<String>()

        override suspend fun status(): Local.GetStatusResponse {
            statusCalls++
            failure?.let { throw it }
            return response
        }

        override suspend fun setDeviceName(name: String): Local.Settings {
            names += name
            return Local.Settings.getDefaultInstance()
        }

        override suspend fun setPause(mode: Local.PauseMode, untilUnixMs: Long): Local.Pause {
            pauseFailure?.let { throw it }
            pauses += mode to untilUnixMs
            return Local.Pause.newBuilder().setPaused(true).build()
        }

        override suspend fun listHistory(
            kind: Local.Kind,
            deviceId: String,
            limit: Int,
        ): Local.ListHistoryResponse {
            historyCalls += kind to deviceId
            historyFailure?.let { throw it }
            return Local.ListHistoryResponse.newBuilder().addAllItems(history).build()
        }

        override suspend fun copyHistoryItem(id: String) {
            copied += id
        }

        override suspend fun deleteHistoryItem(id: String) {
            deleted += id
        }

        override suspend fun fetchHistoryItem(id: String, path: String): Local.FetchHistoryItemResponse =
            Local.FetchHistoryItemResponse.getDefaultInstance()

        override suspend fun listTransfers(): Local.ListTransfersResponse =
            Local.ListTransfersResponse.getDefaultInstance()

        override suspend fun cancelTransfer(id: String) = Unit
    }

    private class FakeSaver(private var saved: Int = 1) : HistorySaver {
        var failure: Exception? = null
        val ids = mutableListOf<String>()

        override suspend fun save(id: String): Int {
            ids += id
            failure?.let { throw it }
            return saved
        }
    }

    private val events = MutableSharedFlow<Mobile.Event>()

    @Test
    fun refreshPublishesTheStatus() = runTest {
        val core = FakeCore()
        val state = appState(core)

        state.refresh()
        advanceUntilIdle()

        assertEquals("Pixel", state.state.value.status?.deviceName)
        assertFalse(state.state.value.loading)
        assertNull(state.state.value.failure)
    }

    @Test
    fun stoppedCoreIsReported() = runTest {
        val core = FakeCore()
        core.failure = CoreException(CoreException.CODE_FAILED_PRECONDITION, "core is not running")
        val state = appState(core)

        state.refresh()
        advanceUntilIdle()

        assertEquals(CoreFailure.STOPPED, state.state.value.failure)
        assertNull(state.state.value.status)
    }

    @Test
    fun otherFailureKeepsTheLastStatus() = runTest {
        val core = FakeCore()
        val state = appState(core)
        state.refresh()
        advanceUntilIdle()

        core.failure = IllegalStateException("timeout")
        state.refresh()
        advanceUntilIdle()

        assertEquals(CoreFailure.UNAVAILABLE, state.state.value.failure)
        assertEquals("Pixel", state.state.value.status?.deviceName)
    }

    @Test
    fun peerEventReloadsTheStatus() = runTest {
        val core = FakeCore()
        val state = appState(core)
        state.refresh()
        advanceUntilIdle()
        val before = core.statusCalls

        events.emit(
            Mobile.Event.newBuilder()
                .setType(Mobile.EventType.EVENT_TYPE_PEER_ONLINE)
                .build(),
        )
        advanceUntilIdle()

        assertTrue(core.statusCalls > before)
    }

    @Test
    fun pauseIsSentAndReloaded() = runTest {
        val core = FakeCore()
        val state = appState(core)

        state.setPause(PauseRequest(Local.PauseMode.PAUSE_MODE_UNTIL, 42L))
        advanceUntilIdle()

        assertEquals(listOf(Local.PauseMode.PAUSE_MODE_UNTIL to 42L), core.pauses)
        assertEquals(1, core.statusCalls)
    }

    @Test
    fun failedPauseEmitsNotice() = runTest {
        val core = FakeCore()
        core.pauseFailure = IllegalStateException("core is busy")
        val state = appState(core)
        val notices = mutableListOf<Int>()
        val collector = launch { state.notices.collect { notices += it } }
        runCurrent()

        state.setPause(PauseRequest(Local.PauseMode.PAUSE_MODE_OFF, 0L))
        advanceUntilIdle()

        assertEquals(listOf(R.string.pause_failed), notices)
        collector.cancel()
    }

    @Test
    fun deviceNameIsTrimmed() = runTest {
        val core = FakeCore()
        val state = appState(core)

        state.setDeviceName("  Pixel  ")
        advanceUntilIdle()

        assertEquals(listOf("Pixel"), core.names)
    }

    @Test
    fun historyIsLoaded() = runTest {
        val core = FakeCore()
        core.history = listOf(entry("1", Local.Kind.KIND_TEXT))
        val state = appState(core)

        state.refreshHistory()
        advanceUntilIdle()

        assertEquals(1, state.history.value.items.size)
        assertTrue(state.history.value.loaded)
        assertFalse(state.history.value.isEmpty)
    }

    @Test
    fun emptyHistoryIsExplained() = runTest {
        val core = FakeCore()
        val state = appState(core)

        state.refreshHistory()
        advanceUntilIdle()

        assertTrue(state.history.value.isEmpty)
    }

    @Test
    fun filtersAreSentAndReloadTheList() = runTest {
        val core = FakeCore()
        val state = appState(core)

        state.refreshHistory()
        advanceUntilIdle()
        state.setHistoryKind(Local.Kind.KIND_IMAGE)
        advanceUntilIdle()
        state.setHistoryDevice("abc")
        advanceUntilIdle()

        assertEquals(
            listOf(
                Local.Kind.KIND_UNSPECIFIED to "",
                Local.Kind.KIND_IMAGE to "",
                Local.Kind.KIND_IMAGE to "abc",
            ),
            core.historyCalls,
        )
    }

    @Test
    fun failedHistoryLoadIsReported() = runTest {
        val core = FakeCore()
        core.historyFailure = IllegalStateException("core is busy")
        val state = appState(core)

        state.refreshHistory()
        advanceUntilIdle()

        assertEquals(CoreFailure.UNAVAILABLE, state.history.value.failure)
    }

    @Test
    fun copyCallsTheCore() = runTest {
        val core = FakeCore()
        val state = appState(core)
        val notices = mutableListOf<Int>()
        val collector = launch { state.notices.collect { notices += it } }
        runCurrent()

        state.copyHistoryItem("item")
        advanceUntilIdle()

        assertEquals(listOf("item"), core.copied)
        assertTrue(R.string.history_copied in notices)
        collector.cancel()
    }

    @Test
    fun downloadSavesThePayloads() = runTest {
        val core = FakeCore()
        val saver = FakeSaver(saved = 2)
        val state = appState(core, saver)
        val notices = mutableListOf<Int>()
        val collector = launch { state.notices.collect { notices += it } }
        runCurrent()

        state.downloadHistoryItem("item")
        advanceUntilIdle()

        assertEquals(listOf("item"), saver.ids)
        assertTrue(R.string.history_downloaded in notices)
        assertEquals(emptySet<String>(), state.history.value.busy)
        collector.cancel()
    }

    @Test
    fun failedDownloadEmitsNotice() = runTest {
        val core = FakeCore()
        val saver = FakeSaver()
        saver.failure = IllegalStateException("no space")
        val state = appState(core, saver)
        val notices = mutableListOf<Int>()
        val collector = launch { state.notices.collect { notices += it } }
        runCurrent()

        state.downloadHistoryItem("item")
        advanceUntilIdle()

        assertTrue(notices.isNotEmpty())
        assertEquals(R.string.history_download_failed, notices.last())
        collector.cancel()
    }

    @Test
    fun deleteCallsTheCore() = runTest {
        val core = FakeCore()
        val state = appState(core)

        state.deleteHistoryItem("item")
        advanceUntilIdle()

        assertEquals(listOf("item"), core.deleted)
    }

    @Test
    fun clipEventReloadsTheHistory() = runTest {
        val core = FakeCore()
        val state = appState(core)
        state.refreshHistory()
        advanceUntilIdle()
        val before = core.historyCalls.size

        events.emit(
            Mobile.Event.newBuilder()
                .setType(Mobile.EventType.EVENT_TYPE_CLIP_RECEIVED)
                .setEvent(
                    Local.Event.newBuilder()
                        .setKind(Local.EventKind.EVENT_KIND_CLIP_DOWNLOAD)
                        .build(),
                )
                .build(),
        )
        advanceUntilIdle()

        assertTrue(core.historyCalls.size > before)
    }

    private fun TestScope.appState(core: CoreApi, saver: HistorySaver = FakeSaver()): AppState =
        AppState(core, events, saver, CoroutineScope(UnconfinedTestDispatcher(testScheduler)))

    private companion object {
        fun sampleStatus(): Local.GetStatusResponse = Local.GetStatusResponse.newBuilder()
            .setDeviceName("Pixel")
            .setRunning(true)
            .addDevices(
                Local.Device.newBuilder()
                    .setName("Mac")
                    .setRole(Local.Role.ROLE_PERSONAL)
                    .setOnline(true)
                    .build(),
            )
            .build()

        fun entry(id: String, kind: Local.Kind): Local.HistoryItem = Local.HistoryItem.newBuilder()
            .setItem(
                Local.ClipItem.newBuilder()
                    .setId(id)
                    .setKind(kind)
                    .build(),
            )
            .setComplete(true)
            .build()
    }
}
