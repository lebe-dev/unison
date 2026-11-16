package app.unison

import android.Manifest
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
import app.unison.core.RunningCore
import app.unison.core.coreEvents
import app.unison.history.HistoryDownloads
import app.unison.service.OperatingMode
import app.unison.service.UnisonService
import app.unison.service.UnisonSettings
import app.unison.ui.UnisonRoot
import app.unison.ui.screens.OnboardingScreen
import app.unison.ui.state.AppState
import app.unison.ui.theme.UnisonTheme
import kotlinx.coroutines.launch

/** Single screen of the app: devices, pairing, history and settings. */
class MainActivity : ComponentActivity() {
    private lateinit var appState: AppState
    private var onboarded by mutableStateOf(false)
    private var deviceName by mutableStateOf("")
    private var operatingMode by mutableStateOf(OperatingMode.DEFAULT)
    private var pending by mutableStateOf<ClipboardDraft?>(null)

    private val notificationPermission =
        registerForActivityResult(ActivityResultContracts.RequestPermission()) { finishOnboarding() }

    private val localNetworkPermission =
        registerForActivityResult(ActivityResultContracts.RequestPermission()) {
            requestNotificationOrFinish()
        }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        appState = AppState(RunningCore(this), coreEvents.events, HistoryDownloads(this))
        onboarded = UnisonSettings.onboarded(this)
        deviceName = UnisonSettings.deviceName(this)
        operatingMode = UnisonSettings.operatingMode(this)
        setContent {
            UnisonTheme {
                val state by appState.state.collectAsState()
                val history by appState.history.collectAsState()
                if (onboarded) {
                    UnisonRoot(
                        state = state,
                        history = history,
                        deviceName = deviceName,
                        operatingMode = operatingMode,
                        clipboardOffer = pending,
                        notices = appState.notices,
                        onRefresh = ::refresh,
                        onSendClipboard = ::send,
                        onDismissClipboard = { pending = null },
                        onDeviceNameChange = ::saveDeviceName,
                        onOperatingModeChange = ::saveOperatingMode,
                        onPause = appState::setPause,
                        onRefreshHistory = appState::refreshHistory,
                        onHistoryKindChange = appState::setHistoryKind,
                        onHistoryDeviceChange = appState::setHistoryDevice,
                        onCopyHistoryItem = { appState.copyHistoryItem(it.item.id) },
                        onDownloadHistoryItem = { appState.downloadHistoryItem(it.item.id) },
                        onDeleteHistoryItem = { appState.deleteHistoryItem(it.item.id) },
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
            appState.refresh()
            appState.refreshHistory()
        }
    }

    override fun onStop() {
        if (operatingMode == OperatingMode.APP_OPEN) {
            UnisonService.stop(this)
        }
        super.onStop()
    }

    override fun onWindowFocusChanged(hasFocus: Boolean) {
        super.onWindowFocusChanged(hasFocus)
        if (!hasFocus) {
            return
        }
        refresh()
        if (onboarded) {
            appState.refreshHistory()
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
        appState.refresh()
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

    /** Reloads the status and the clipboard offer. */
    private fun refresh() {
        if (!onboarded) {
            return
        }
        appState.refresh()
        refreshPending()
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
}
