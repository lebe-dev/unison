package app.unison

import android.Manifest
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import android.os.Bundle
import android.widget.Toast
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.core.content.ContextCompat
import androidx.lifecycle.lifecycleScope
import app.unison.clipboard.ClipboardDraft
import app.unison.clipboard.ClipboardRead
import app.unison.clipboard.ClipboardReader
import app.unison.clipboard.ClipboardSender
import app.unison.clipboard.HeldItems
import app.unison.core.LocalAddresses
import app.unison.core.RunningCore
import app.unison.core.coreEvents
import app.unison.history.HistoryDownloads
import app.unison.policy.PolicyConfig
import app.unison.policy.PolicyDefaults
import app.unison.policy.PolicyProto
import app.unison.service.OperatingMode
import app.unison.service.UnisonService
import app.unison.service.UnisonSettings
import app.unison.ui.Destination
import app.unison.ui.UnisonRoot
import app.unison.ui.screens.OnboardingScreen
import app.unison.ui.state.AppState
import app.unison.ui.theme.UnisonTheme
import kotlin.coroutines.cancellation.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.launch

/** Single screen of the app: devices, pairing, requests, history and settings. */
class MainActivity : ComponentActivity() {
    private lateinit var appState: AppState
    private lateinit var core: RunningCore
    private var onboarded by mutableStateOf(false)
    private var deviceName by mutableStateOf("")
    private var operatingMode by mutableStateOf(OperatingMode.DEFAULT)
    private var policy by mutableStateOf(PolicyDefaults.config)
    private var pending by mutableStateOf<ClipboardDraft?>(null)
    private val requestedScreen = MutableStateFlow<Destination?>(null)
    private var requestsPoll: Job? = null

    private val notificationPermission =
        registerForActivityResult(ActivityResultContracts.RequestPermission()) { finishOnboarding() }

    private val localNetworkPermission =
        registerForActivityResult(ActivityResultContracts.RequestPermission()) {
            requestNotificationOrFinish()
        }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        core = RunningCore(this)
        appState = AppState(
            core = core,
            events = coreEvents.events,
            saver = HistoryDownloads(this),
            localAddresses = LocalAddresses(this)::ipv4,
        )
        onboarded = UnisonSettings.onboarded(this)
        deviceName = UnisonSettings.deviceName(this)
        operatingMode = UnisonSettings.operatingMode(this)
        policy = PolicyDefaults.config
        requestedScreen.value = Destination.ofScreen(intent?.getStringExtra(EXTRA_SCREEN))
        setContent {
            UnisonTheme {
                val state by appState.state.collectAsState()
                val history by appState.history.collectAsState()
                val pairing by appState.pairing.collectAsState()
                val requests by appState.requests.collectAsState()
                val held by HeldItems.shared.ids.collectAsState()
                if (onboarded) {
                    UnisonRoot(
                        state = state,
                        history = history,
                        pairing = pairing,
                        requests = requests,
                        held = held,
                        deviceName = deviceName,
                        operatingMode = operatingMode,
                        policy = policy,
                        clipboardOffer = pending,
                        notices = appState.notices,
                        requestedScreen = requestedScreen,
                        onScreenShown = { requestedScreen.value = null },
                        onRefresh = ::refresh,
                        onSendClipboard = ::send,
                        onDismissClipboard = { pending = null },
                        onDeviceNameChange = ::saveDeviceName,
                        onOperatingModeChange = ::saveOperatingMode,
                        onPolicyChange = ::savePolicy,
                        onPolicyReset = ::resetPolicy,
                        onPause = appState::setPause,
                        onSelfConfirmationChange = appState::setSelfConfirmation,
                        onLeaveGroup = appState::leaveGroup,
                        onSetDeviceConfirmation = { device, require ->
                            appState.setDeviceConfirmation(device.deviceId, require)
                        },
                        onRevokeDevice = { device -> appState.revokeDevice(device.deviceId) },
                        onRefreshHistory = appState::refreshHistory,
                        onHistoryKindChange = appState::setHistoryKind,
                        onHistoryDeviceChange = appState::setHistoryDevice,
                        onCopyHistoryItem = { appState.copyHistoryItem(it.item.id) },
                        onApplyHistoryItem = { appState.applyHistoryItem(it.item.id) },
                        onDownloadHistoryItem = { appState.downloadHistoryItem(it.item.id) },
                        onDeleteHistoryItem = { appState.deleteHistoryItem(it.item.id) },
                        onRefreshRequests = appState::refreshRequests,
                        onAcceptRequest = { appState.decideRequest(it.id, accept = true) },
                        onDeclineRequest = { appState.decideRequest(it.id, accept = false) },
                        onPairingRoleChange = appState::setPairingRole,
                        onOpenPairingWindow = appState::openPairingWindow,
                        onExtendPairingWindow = appState::extendPairingWindow,
                        onClosePairingWindow = appState::closePairingWindow,
                        onRefreshPairingCandidates = appState::refreshPairingCandidates,
                        onPairWith = appState::startPairing,
                        onConfirmPairing = appState::confirmPairing,
                        onRejectPairing = appState::rejectPairing,
                        onScanPairingCode = appState::scanPairingCode,
                        onCancelScan = appState::cancelScan,
                        onDismissPairingResult = appState::dismissPairingResult,
                    )
                } else {
                    OnboardingScreen(onEnable = ::enable)
                }
            }
        }
    }

    override fun onStart() {
        super.onStart()
        if (onboarded) {
            UnisonService.start(this)
            refresh()
            appState.refreshHistory()
            appState.refreshRequests()
            startRequestsPoll()
        }
    }

    override fun onStop() {
        requestsPoll?.cancel()
        requestsPoll = null
        if (operatingMode == OperatingMode.APP_OPEN) {
            UnisonService.stop(this)
        }
        super.onStop()
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        requestedScreen.value = Destination.ofScreen(intent.getStringExtra(EXTRA_SCREEN))
    }

    override fun onWindowFocusChanged(hasFocus: Boolean) {
        super.onWindowFocusChanged(hasFocus)
        if (!hasFocus) {
            return
        }
        refresh()
        if (onboarded) {
            appState.refreshHistory()
            appState.refreshRequests()
        }
    }

    override fun onDestroy() {
        if (::appState.isInitialized) {
            appState.close()
        }
        super.onDestroy()
    }

    /** Starts the core from the first run screen. */
    private fun enable() {
        if (needsLocalNetworkPermission()) {
            localNetworkPermission.launch(Manifest.permission.ACCESS_LOCAL_NETWORK)
            return
        }
        requestNotificationOrFinish()
    }

    private fun requestNotificationOrFinish() {
        if (needsNotificationPermission()) {
            notificationPermission.launch(Manifest.permission.POST_NOTIFICATIONS)
            return
        }
        finishOnboarding()
    }

    private fun finishOnboarding() {
        UnisonSettings.setOnboarded(this, true)
        onboarded = true
        UnisonService.start(this)
        refresh()
        appState.refreshHistory()
        appState.refreshRequests()
        startRequestsPoll()
    }

    private fun needsNotificationPermission(): Boolean =
        Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU &&
            ContextCompat.checkSelfPermission(this, Manifest.permission.POST_NOTIFICATIONS) !=
            PackageManager.PERMISSION_GRANTED

    /** Local network access is a runtime permission from Android 17 on. */
    private fun needsLocalNetworkPermission(): Boolean =
        Build.VERSION.SDK_INT >= Build.VERSION_CODES.CINNAMON_BUN &&
            ContextCompat.checkSelfPermission(this, Manifest.permission.ACCESS_LOCAL_NETWORK) !=
            PackageManager.PERMISSION_GRANTED

    /** Reloads the status, the network policy and the clipboard offer. */
    private fun refresh() {
        if (!onboarded) {
            return
        }
        appState.refresh()
        refreshPolicy()
        refreshPending()
    }

    /** Reloads the policy table of the core. */
    private fun refreshPolicy() {
        lifecycleScope.launch {
            try {
                policy = PolicyProto.table(core.settings())
            } catch (e: CancellationException) {
                throw e
            } catch (_: Exception) {
                // The table keeps its last value while the core is unavailable.
            }
        }
    }

    /** Offers the clipboard when it holds content that was not synchronized before. */
    private fun refreshPending() {
        val draft = (ClipboardReader.read(this) as? ClipboardRead.Draft)?.draft
        pending = draft?.takeIf { !it.isEmpty && ClipboardSender.isNew(this, it) }
    }

    private fun send(draft: ClipboardDraft) {
        lifecycleScope.launch {
            val outcome = ClipboardSender.send(this@MainActivity, draft)
            Toast.makeText(this@MainActivity, outcome.message, Toast.LENGTH_SHORT).show()
            refresh()
        }
    }

    private fun saveDeviceName(name: String) {
        val trimmed = name.trim()
        UnisonSettings.setDeviceName(this, trimmed)
        deviceName = trimmed
        appState.setDeviceName(trimmed)
    }

    private fun saveOperatingMode(mode: OperatingMode) {
        UnisonSettings.setOperatingMode(this, mode)
        operatingMode = mode
        if (mode != OperatingMode.APP_OPEN) {
            UnisonService.start(this)
        }
    }

    /** Stores the network policy in the core. */
    private fun savePolicy(config: PolicyConfig) {
        policy = config
        lifecycleScope.launch {
            try {
                val stored = PolicyProto.table(core.updatePolicy(config))
                // A newer change may already be on its way, keep what it shows.
                if (policy == config) {
                    policy = stored
                }
            } catch (e: CancellationException) {
                throw e
            } catch (_: Exception) {
                Toast.makeText(this@MainActivity, R.string.policy_save_failed, Toast.LENGTH_SHORT).show()
                refreshPolicy()
            }
        }
    }

    /** Puts the default table back. */
    private fun resetPolicy() {
        lifecycleScope.launch {
            try {
                policy = PolicyProto.table(core.resetPolicy())
                Toast.makeText(this@MainActivity, R.string.policy_reset_done, Toast.LENGTH_SHORT).show()
            } catch (e: CancellationException) {
                throw e
            } catch (_: Exception) {
                Toast.makeText(this@MainActivity, R.string.policy_save_failed, Toast.LENGTH_SHORT).show()
            }
        }
    }

    /** Follows the offers while the app is on screen, they expire on their own. */
    private fun startRequestsPoll() {
        if (requestsPoll != null) {
            return
        }
        requestsPoll = lifecycleScope.launch {
            while (true) {
                delay(REQUESTS_POLL_MS)
                appState.refreshRequests()
            }
        }
    }

    companion object {
        /** Screen a notification asks to open. */
        const val EXTRA_SCREEN = "app.unison.extra.SCREEN"

        /** Interval of the offers while the app is on screen. */
        private const val REQUESTS_POLL_MS = 15_000L
    }
}
