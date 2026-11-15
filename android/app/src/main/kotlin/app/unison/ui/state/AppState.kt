package app.unison.ui.state

import app.unison.R
import app.unison.clipboard.PauseRequest
import app.unison.core.CoreApi
import app.unison.core.CoreException
import kotlin.coroutines.cancellation.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.channels.BufferOverflow
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import unison.mobile.v1.Mobile

/** State of the screens over the core and the changes they request. */
class AppState(
    private val core: CoreApi,
    events: Flow<Mobile.Event>,
    private val scope: CoroutineScope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate),
) {
    private val mutableState = MutableStateFlow(CoreState())

    /** What the screens render. */
    val state: StateFlow<CoreState> = mutableState.asStateFlow()

    private val mutableNotices = MutableSharedFlow<Int>(
        extraBufferCapacity = NOTICE_BUFFER,
        onBufferOverflow = BufferOverflow.DROP_OLDEST,
    )

    /** One-off messages for the snackbar, as string resources. */
    val notices: SharedFlow<Int> = mutableNotices.asSharedFlow()

    private var load: Job? = null

    init {
        scope.launch {
            events.collect { event ->
                if (event.changesDevices()) {
                    refresh()
                }
            }
        }
    }

    /** Reloads the status of the core. */
    fun refresh() {
        load?.cancel()
        load = scope.launch {
            mutableState.update { it.copy(loading = it.status == null, failure = null) }
            try {
                mutableState.value = CoreState(loading = false, status = core.status())
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                mutableState.update { it.copy(loading = false, failure = failureOf(e)) }
            }
        }
    }

    /** Changes the pause of the clipboard synchronization. */
    fun setPause(request: PauseRequest) {
        scope.launch {
            try {
                core.setPause(request.mode, request.untilUnixMs)
                refresh()
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                mutableNotices.tryEmit(R.string.pause_failed)
            }
        }
    }

    /** Stores the device name used from the next start. */
    fun setDeviceName(name: String) {
        scope.launch {
            try {
                core.setDeviceName(name.trim())
                mutableNotices.tryEmit(R.string.settings_name_saved)
                refresh()
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                mutableNotices.tryEmit(R.string.settings_name_failed)
            }
        }
    }

    /** Stops the state updates. */
    fun close() {
        scope.cancel()
    }

    private fun failureOf(e: Exception): CoreFailure =
        if (e is CoreException && e.isNotRunning) CoreFailure.STOPPED else CoreFailure.UNAVAILABLE

    /** Events that change the list of devices in the status. */
    private fun Mobile.Event.changesDevices(): Boolean = when (type) {
        Mobile.EventType.EVENT_TYPE_PEER_ONLINE,
        Mobile.EventType.EVENT_TYPE_PEER_OFFLINE,
        Mobile.EventType.EVENT_TYPE_PAIRING_PAIRED,
        -> true
        else -> false
    }

    private companion object {
        const val NOTICE_BUFFER = 4
    }
}
