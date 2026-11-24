package app.unison

import android.Manifest
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
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
import app.unison.core.CoreException
import app.unison.core.LocalAddresses
import app.unison.core.RunningCore
import app.unison.core.coreEvents
import app.unison.diagnostics.Diagnostics
import app.unison.diagnostics.Journal
import app.unison.history.HistoryDownloads
import app.unison.history.HistoryThumbnailCache
import app.unison.history.HistoryThumbnails
import app.unison.history.HistoryTtlDefaults
import app.unison.history.HistoryTtlProto
import app.unison.pairing.PairingLink
import app.unison.policy.PolicyConfig
import app.unison.policy.PolicyDefaults
import app.unison.policy.PolicyProto
import app.unison.service.DeviceNames
import app.unison.service.NotificationSettings
import app.unison.service.OperatingMode
import app.unison.service.UnisonService
import app.unison.service.UnisonSettings
import app.unison.settingsfile.SettingsFile
import app.unison.storage.DownloadsFolder
import app.unison.ui.Destination
import app.unison.ui.PromptAnswer
import app.unison.ui.UnisonRoot
import app.unison.ui.screens.DashboardPanel
import app.unison.ui.screens.OnboardingScreen
import app.unison.ui.state.AppState
import app.unison.ui.state.DashboardLimit
import app.unison.ui.theme.UnisonTheme
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import unison.local.v1.Local
import java.io.ByteArrayOutputStream
import java.io.IOException
import kotlin.coroutines.cancellation.CancellationException

/** Largest settings document the app reads, the same bound as the core. */
private const val MAX_SETTINGS_DOCUMENT = 512 shl 10

/** Size of one read of a settings document. */
private const val SETTINGS_READ_CHUNK = 8192

/** Single screen of the app: devices, pairing, requests, history and settings. */
class MainActivity : ComponentActivity() {
    private lateinit var appState: AppState
    private lateinit var core: RunningCore
    private lateinit var thumbnails: HistoryThumbnailCache
    private var onboarded by mutableStateOf(false)
    private var deviceName by mutableStateOf("")
    private var operatingMode by mutableStateOf(OperatingMode.DEFAULT)
    private var dashboardLimit by mutableStateOf(DashboardLimit.DEFAULT)
    private var downloadsFolder by mutableStateOf<String?>(null)
    private var policy by mutableStateOf(PolicyDefaults.config)
    private var historyTtl by mutableStateOf(HistoryTtlDefaults.values)
    private var diagnosticsEnabled by mutableStateOf(false)
    private var notificationSettings by mutableStateOf(NotificationSettings())
    private var pending by mutableStateOf<ClipboardDraft?>(null)
    private val requestedScreen = MutableStateFlow<Destination?>(null)
    private var requestsPoll: Job? = null
    private var pairingLink: String? = null

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
        dashboardLimit = UnisonSettings.dashboardLimit(this)
        thumbnails = HistoryThumbnails.cache(core::historyThumbnail)
        appState =
            AppState(
                core = core,
                events = coreEvents.events,
                saver = HistoryDownloads(this),
                localAddresses = LocalAddresses(this)::ipv4,
                dashboardLimit = dashboardLimit,
            )
        onboarded = UnisonSettings.onboarded(this)
        deviceName = UnisonSettings.deviceName(this)
        operatingMode = UnisonSettings.operatingMode(this)
        downloadsFolder = DownloadsFolder.current(this)?.let(DownloadsFolder::label)
        policy = PolicyDefaults.config
        historyTtl = HistoryTtlDefaults.values
        diagnosticsEnabled = UnisonSettings.sentryEnabled(this)
        notificationSettings = NotificationSettings.load(this)
        requestedScreen.value = Destination.ofScreen(intent?.getStringExtra(EXTRA_SCREEN))
        // A recreated activity has already taken the code of its intent; a code
        // that still waits for the user is kept across the recreation.
        if (savedInstanceState == null) {
            takePairingLink(intent)
        } else {
            pairingLink = savedInstanceState.getString(STATE_PAIRING_LINK)
            offerPairingLink()
        }
        setContent {
            UnisonTheme {
                val state by appState.state.collectAsState()
                val history by appState.history.collectAsState()
                val dashboard by appState.dashboard.collectAsState()
                val pairing by appState.pairing.collectAsState()
                val requests by appState.requests.collectAsState()
                val held by HeldItems.shared.ids.collectAsState()
                val prompts by appState.prompts.collectAsState()
                if (onboarded) {
                    UnisonRoot(
                        state = state,
                        history = history,
                        dashboard =
                            DashboardPanel(
                                state = dashboard,
                                thumbnail = thumbnails::thumbnail,
                                onRefresh = appState::refreshDashboard,
                                onCopy = { appState.copyHistoryItem(it.item.id) },
                                onApply = { appState.applyHistoryItem(it.item.id) },
                                onDownload = { appState.downloadHistoryItem(it.item.id) },
                                onDelete = { appState.deleteHistoryItem(it.item.id) },
                            ),
                        dashboardLimit = dashboardLimit,
                        pairing = pairing,
                        requests = requests,
                        held = held,
                        prompts = prompts,
                        onAnswerPrompt = { prompt, answer ->
                            when (answer) {
                                PromptAnswer.APPLY -> appState.applyHistoryItem(prompt.id)
                                PromptAnswer.SKIP -> appState.skipHeldItem(prompt.id)
                                PromptAnswer.ACCEPT -> appState.decideRequest(prompt.id, accept = true)
                                PromptAnswer.DECLINE -> appState.decideRequest(prompt.id, accept = false)
                            }
                        },
                        deviceName = deviceName,
                        operatingMode = operatingMode,
                        policy = policy,
                        ttl = historyTtl,
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
                        onTtlSave = ::saveHistoryTtl,
                        onTtlReset = ::resetHistoryTtl,
                        onDashboardLimitChange = ::saveDashboardLimit,
                        downloadsFolder = downloadsFolder,
                        onDownloadsFolderPick = ::saveDownloadsFolder,
                        onDownloadsFolderReset = ::resetDownloadsFolder,
                        onPause = appState::setPause,
                        onRelaySave = appState::setRelay,
                        onRelayOff = appState::relayOff,
                        onRefreshRelay = appState::refreshRelay,
                        onRelayCheck = appState::testRelay,
                        onSelfConfirmationChange = appState::setSelfConfirmation,
                        onAllowRemoteSettingsChange = appState::setAllowRemoteSettings,
                        onLeaveGroup = appState::leaveGroup,
                        diagnosticsEnabled = diagnosticsEnabled,
                        onDiagnosticsChange = ::saveDiagnostics,
                        notifications = notificationSettings,
                        onNotificationsChange = ::saveNotificationSettings,
                        journalEnabled = state.journalEnabled,
                        onJournalChange = appState::setJournalEnabled,
                        onExportSettings = ::exportSettings,
                        onImportSettings = ::importSettings,
                        stripTracking = state.stripTracking,
                        onStripTrackingChange = appState::setStripTracking,
                        onSetDeviceConfirmation = { device, scope, side, require ->
                            appState.setDeviceConfirmation(device.deviceId, scope, side, require)
                        },
                        onSetDeviceRemoteSettings = { device, allow ->
                            appState.setDeviceRemoteSettings(device.deviceId, allow)
                        },
                        onRenameDevice = { device, name -> appState.renameDevice(device.deviceId, name) },
                        onRevokeDevice = { device -> appState.revokeDevice(device.deviceId) },
                        onRefreshHistory = appState::refreshHistory,
                        onHistoryKindChange = appState::setHistoryKind,
                        onHistoryDeviceChange = appState::setHistoryDevice,
                        onCopyHistoryItem = { appState.copyHistoryItem(it.item.id) },
                        onApplyHistoryItem = { appState.applyHistoryItem(it.item.id) },
                        onDownloadHistoryItem = { appState.downloadHistoryItem(it.item.id) },
                        onDeleteHistoryItem = { appState.deleteHistoryItem(it.item.id) },
                        onClearHistory = appState::clearHistory,
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
                        onAcceptPairingLink = appState::acceptPairingLink,
                        onDismissPairingLink = appState::dismissPairingLink,
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
        appState.setVisible(true)
        if (onboarded) {
            UnisonService.start(this)
            refresh()
            appState.refreshHistory()
            appState.refreshDashboard()
            appState.refreshRequests()
            startRequestsPoll()
        }
    }

    override fun onStop() {
        requestsPoll?.cancel()
        requestsPoll = null
        appState.setVisible(false)
        // A rotation stops and recreates the activity; the core keeps running through it.
        if (operatingMode == OperatingMode.APP_OPEN && !isChangingConfigurations) {
            UnisonService.stop(this)
        }
        super.onStop()
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        requestedScreen.value = Destination.ofScreen(intent.getStringExtra(EXTRA_SCREEN))
        takePairingLink(intent)
    }

    override fun onWindowFocusChanged(hasFocus: Boolean) {
        super.onWindowFocusChanged(hasFocus)
        if (!hasFocus) {
            return
        }
        refresh()
        if (onboarded) {
            appState.refreshHistory()
            appState.refreshDashboard()
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
        appState.refreshDashboard()
        appState.refreshRequests()
        startRequestsPoll()
        offerPairingLink()
    }

    /** Keeps the pairing code of a link, such as one opened by the system camera. */
    private fun takePairingLink(intent: Intent?) {
        val code = PairingLink.code(intent?.action, intent?.dataString) ?: return
        pairingLink = code
        requestedScreen.value = Destination.ADD
        offerPairingLink()
    }

    /**
     * Asks the user about the device of a kept link once the first run screen is
     * done. The link may come from any app or web page, so it never pairs on its
     * own.
     */
    private fun offerPairingLink() {
        if (!onboarded) {
            return
        }
        val code = pairingLink ?: return
        pairingLink = null
        appState.offerPairingLink(code)
    }

    override fun onSaveInstanceState(outState: Bundle) {
        super.onSaveInstanceState(outState)
        val waiting =
            pairingLink ?: appState.pairing.value.link
                ?.uri
        if (waiting != null) {
            outState.putString(STATE_PAIRING_LINK, waiting)
        }
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

    /** Reloads the status, the settings and the clipboard offer. */
    private fun refresh() {
        if (!onboarded) {
            return
        }
        appState.refresh()
        refreshSettings()
        refreshPending()
    }

    /** Reloads the policy table and the history lifetimes of the core. */
    private fun refreshSettings() {
        lifecycleScope.launch {
            try {
                val settings = core.settings()
                policy = PolicyProto.table(settings)
                historyTtl = HistoryTtlProto.values(settings)
                applyDiagnosticsConsent(settings.sentryEnabled)
            } catch (e: CancellationException) {
                throw e
            } catch (_: Exception) {
                // The values keep their last state while the core is unavailable.
            }
        }
    }

    /** Keeps the preference and the state of the switch in step with the core. */
    private fun applyDiagnosticsConsent(enabled: Boolean) {
        diagnosticsEnabled = enabled
        if (UnisonSettings.sentryEnabled(this) != enabled) {
            Diagnostics.setConsent(this, enabled)
        }
    }

    /** Stores the consent at once and mirrors it into the core. */
    private fun saveDiagnostics(enabled: Boolean) {
        Diagnostics.setConsent(this, enabled)
        diagnosticsEnabled = enabled
        appState.setSentryEnabled(enabled)
    }

    /** Stores the notification preferences; the service reads them at the next post. */
    private fun saveNotificationSettings(settings: NotificationSettings) {
        NotificationSettings.save(this, settings)
        notificationSettings = settings
        UnisonService.start(this)
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
        val fitted = DeviceNames.fit(name)
        UnisonSettings.setDeviceName(this, fitted)
        deviceName = fitted
        appState.setDeviceName(fitted)
    }

    private fun saveDownloadsFolder(tree: Uri) {
        if (!DownloadsFolder.choose(this, tree)) {
            Toast.makeText(this, R.string.settings_downloads_failed, Toast.LENGTH_LONG).show()
            return
        }
        downloadsFolder = DownloadsFolder.label(tree)
    }

    private fun resetDownloadsFolder() {
        DownloadsFolder.reset(this)
        downloadsFolder = null
    }

    /** Writes the settings document to the file the user picked. */
    private fun exportSettings(uri: Uri) {
        lifecycleScope.launch {
            try {
                val response = core.exportSettings(SettingsFile.appSection(operatingMode, notificationSettings))
                withContext(Dispatchers.IO) {
                    val stream =
                        contentResolver.openOutputStream(uri)
                            ?: throw IOException("the document cannot be written")
                    stream.use { it.write(response.document.toByteArray()) }
                }
                Toast.makeText(this@MainActivity, R.string.settings_file_exported, Toast.LENGTH_SHORT).show()
            } catch (e: CancellationException) {
                throw e
            } catch (e: IOException) {
                reportSettingsFileFailure(R.string.settings_file_export_failed, e)
            } catch (e: SecurityException) {
                reportSettingsFileFailure(R.string.settings_file_export_failed, e)
            } catch (e: CoreException) {
                reportSettingsFileFailure(R.string.settings_file_export_failed, e)
            }
        }
    }

    /** Applies the settings document of the file the user picked. */
    private fun importSettings(uri: Uri) {
        lifecycleScope.launch {
            try {
                val document = withContext(Dispatchers.IO) { readSettingsDocument(uri) }
                val response = core.importSettings(document)
                val section = SettingsFile.appSectionOf(document.decodeToString())
                section.operatingMode?.let(::saveOperatingMode)
                section.notifications?.let { patch -> saveNotificationSettings(patch.applyTo(notificationSettings)) }
                refresh()
                Toast.makeText(this@MainActivity, importNotice(response), Toast.LENGTH_LONG).show()
            } catch (e: CancellationException) {
                throw e
            } catch (e: IOException) {
                reportSettingsFileFailure(R.string.settings_file_import_failed, e)
            } catch (e: SecurityException) {
                reportSettingsFileFailure(R.string.settings_file_import_failed, e)
            } catch (e: CoreException) {
                reportSettingsFileFailure(R.string.settings_file_import_failed, e)
            }
        }
    }

    /** reportSettingsFileFailure writes the failure to the log and tells the user. */
    private fun reportSettingsFileFailure(
        message: Int,
        error: Exception,
    ) {
        Journal.warn("the settings file failed", "error" to error.message)
        Toast.makeText(this, message, Toast.LENGTH_LONG).show()
    }

    /** readSettingsDocument returns the document of the picked file. */
    private fun readSettingsDocument(uri: Uri): ByteArray {
        val stream =
            contentResolver.openInputStream(uri)
                ?: throw IOException("the document cannot be read")
        return stream.use { input ->
            val chunks = ByteArrayOutputStream()
            val buffer = ByteArray(SETTINGS_READ_CHUNK)
            while (true) {
                val read = input.read(buffer)
                if (read < 0) {
                    break
                }
                chunks.write(buffer, 0, read)
                if (chunks.size() > MAX_SETTINGS_DOCUMENT) {
                    throw IOException("the document is too large")
                }
            }
            chunks.toByteArray()
        }
    }

    /** importNotice tells what the import applied and what it had to skip. */
    private fun importNotice(response: Local.ImportSettingsResponse): String =
        buildList {
            add(getString(R.string.settings_file_imported))
            if (response.fromNewerVersion) {
                add(getString(R.string.settings_file_newer))
            }
            if (response.ignoredCount > 0) {
                add(getString(R.string.settings_file_ignored, response.ignoredList.joinToString(", ")))
            }
        }.joinToString(" ")

    private fun saveOperatingMode(mode: OperatingMode) {
        UnisonSettings.setOperatingMode(this, mode)
        operatingMode = mode
        if (mode != OperatingMode.APP_OPEN) {
            UnisonService.start(this)
        }
    }

    /** Stores the number of items each tab of the dashboard shows. */
    private fun saveDashboardLimit(limit: Int) {
        val normalized = DashboardLimit.normalize(limit)
        UnisonSettings.setDashboardLimit(this, normalized)
        dashboardLimit = normalized
        appState.setDashboardLimit(normalized)
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
                refreshSettings()
            }
        }
    }

    /** Stores the lifetime of one kind of the history. */
    private fun saveHistoryTtl(
        kind: Local.Kind,
        seconds: Long,
    ) {
        lifecycleScope.launch {
            try {
                historyTtl = HistoryTtlProto.values(core.setHistoryTtl(kind, seconds))
                Toast.makeText(this@MainActivity, R.string.history_ttl_saved, Toast.LENGTH_SHORT).show()
            } catch (e: CancellationException) {
                throw e
            } catch (_: Exception) {
                Toast
                    .makeText(this@MainActivity, R.string.history_ttl_save_failed, Toast.LENGTH_SHORT)
                    .show()
                refreshSettings()
            }
        }
    }

    /** Puts the default lifetimes back. */
    private fun resetHistoryTtl() {
        lifecycleScope.launch {
            try {
                historyTtl = HistoryTtlProto.values(core.resetHistoryTtl())
                Toast
                    .makeText(this@MainActivity, R.string.history_ttl_reset_done, Toast.LENGTH_SHORT)
                    .show()
            } catch (e: CancellationException) {
                throw e
            } catch (_: Exception) {
                Toast
                    .makeText(this@MainActivity, R.string.history_ttl_save_failed, Toast.LENGTH_SHORT)
                    .show()
                refreshSettings()
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
        requestsPoll =
            lifecycleScope.launch {
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

        /** Saved state key of a pairing link that waits for the user. */
        private const val STATE_PAIRING_LINK = "app.unison.state.PAIRING_LINK"
    }
}
