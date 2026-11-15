package app.unison.ui.state

import app.unison.R
import app.unison.clipboard.PauseRequest
import app.unison.core.CoreApi
import app.unison.core.CoreException
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

    private fun TestScope.appState(core: CoreApi): AppState =
        AppState(core, events, CoroutineScope(UnconfinedTestDispatcher(testScheduler)))

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
    }
}
