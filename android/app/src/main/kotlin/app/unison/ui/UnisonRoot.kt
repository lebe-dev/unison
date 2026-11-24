package app.unison.ui

import android.net.Uri
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Badge
import androidx.compose.material3.BadgedBox
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalResources
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.res.stringResource
import app.unison.clipboard.ClipboardDraft
import app.unison.clipboard.PauseRequest
import app.unison.history.HistoryTtl
import app.unison.policy.PolicyConfig
import app.unison.service.NotificationSettings
import app.unison.service.OperatingMode
import app.unison.ui.screens.AddScreen
import app.unison.ui.screens.DashboardPanel
import app.unison.ui.screens.DashboardScreen
import app.unison.ui.screens.DevicesScreen
import app.unison.ui.screens.HistoryScreen
import app.unison.ui.screens.RequestsScreen
import app.unison.ui.screens.SettingsScreen
import app.unison.ui.state.CoreState
import app.unison.ui.state.HistoryState
import app.unison.ui.state.MediatedState
import app.unison.ui.state.PairingState
import app.unison.ui.state.PendingPrompt
import app.unison.ui.state.RequestsState
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.StateFlow
import unison.local.v1.Local

/** Bottom navigation and the screens above it. */
@Composable
fun UnisonRoot(
    state: CoreState,
    history: HistoryState,
    dashboard: DashboardPanel,
    dashboardLimit: Int,
    updates: UpdatePanel,
    pairing: PairingState,
    requests: RequestsState,
    mediated: MediatedState,
    held: Set<String>,
    prompts: List<PendingPrompt>,
    onAnswerPrompt: (PendingPrompt, PromptAnswer) -> Unit,
    deviceName: String,
    operatingMode: OperatingMode,
    policy: PolicyConfig,
    ttl: HistoryTtl,
    clipboardOffer: ClipboardDraft?,
    notices: Flow<Int>,
    requestedScreen: StateFlow<Destination?>,
    onScreenShown: () -> Unit,
    onRefresh: () -> Unit,
    onSendClipboard: (ClipboardDraft) -> Unit,
    onDismissClipboard: () -> Unit,
    onDeviceNameChange: (String) -> Unit,
    onOperatingModeChange: (OperatingMode) -> Unit,
    onPolicyChange: (PolicyConfig) -> Unit,
    onPolicyReset: () -> Unit,
    onTtlSave: (Local.Kind, Long) -> Unit,
    onTtlReset: () -> Unit,
    onDashboardLimitChange: (Int) -> Unit,
    downloadsFolder: String?,
    onDownloadsFolderPick: (Uri) -> Unit,
    onDownloadsFolderReset: () -> Unit,
    onPause: (PauseRequest) -> Unit,
    onRelaySave: (url: String, token: String?, filesOnRequest: Boolean?) -> Unit,
    onRelayOff: () -> Unit,
    onRefreshRelay: () -> Unit,
    onRelayCheck: (url: String, token: String?) -> Unit,
    onSelfConfirmationChange: (Local.ConfirmationScope, Boolean) -> Unit,
    onAllowRemoteSettingsChange: (Boolean) -> Unit,
    onLeaveGroup: () -> Unit,
    diagnosticsEnabled: Boolean,
    onDiagnosticsChange: (Boolean) -> Unit,
    notifications: NotificationSettings,
    onNotificationsChange: (NotificationSettings) -> Unit,
    journalEnabled: Boolean,
    onJournalChange: (Boolean) -> Unit,
    onExportSettings: (Uri) -> Unit,
    onImportSettings: (Uri) -> Unit,
    stripTracking: Boolean,
    onStripTrackingChange: (Boolean) -> Unit,
    onSetDeviceConfirmation: (Local.Device, Local.ConfirmationScope, Local.ConfirmationSide, Boolean) -> Unit,
    onSetDeviceRemoteSettings: (Local.Device, Boolean) -> Unit,
    onRenameDevice: (Local.Device, String) -> Unit,
    onRevokeDevice: (Local.Device) -> Unit,
    onRefreshHistory: () -> Unit,
    onHistoryKindChange: (Local.Kind) -> Unit,
    onHistoryDeviceChange: (String) -> Unit,
    onCopyHistoryItem: (Local.HistoryItem) -> Unit,
    onApplyHistoryItem: (Local.HistoryItem) -> Unit,
    onDownloadHistoryItem: (Local.HistoryItem) -> Unit,
    onDeleteHistoryItem: (Local.HistoryItem) -> Unit,
    onClearHistory: () -> Unit,
    onRefreshRequests: () -> Unit,
    onAcceptRequest: (Local.ShareRequest) -> Unit,
    onDeclineRequest: (Local.ShareRequest) -> Unit,
    onFriendsDiscoveryChange: (Boolean) -> Unit,
    onIntroduceDevices: (peerA: String, peerB: String, role: Local.Role) -> Unit,
    onCancelMediated: (Local.MediatedPair) -> Unit,
    onAcceptMediated: (Local.MediatedPair, Boolean) -> Unit,
    onDeclineMediated: (Local.MediatedPair) -> Unit,
    onRefreshMediated: () -> Unit,
    onPairingRoleChange: (Local.Role) -> Unit,
    onOpenPairingWindow: () -> Unit,
    onExtendPairingWindow: () -> Unit,
    onClosePairingWindow: () -> Unit,
    onRefreshPairingCandidates: () -> Unit,
    onPairWith: (String) -> Unit,
    onConfirmPairing: () -> Unit,
    onRejectPairing: () -> Unit,
    onScanPairingCode: (String) -> Unit,
    onAcceptPairingLink: () -> Unit,
    onDismissPairingLink: () -> Unit,
    onCancelScan: () -> Unit,
    onDismissPairingResult: () -> Unit,
) {
    var destination by rememberSaveable { mutableStateOf(Destination.DEFAULT) }
    val snackbar = remember { SnackbarHostState() }
    val resources = LocalResources.current
    LaunchedEffect(notices) {
        notices.collect { message -> snackbar.showSnackbar(resources.getString(message)) }
    }
    LaunchedEffect(requestedScreen) {
        requestedScreen.collect { screen ->
            if (screen != null) {
                destination = screen
                onScreenShown()
            }
        }
    }

    if (destination == Destination.ADD) {
        BackHandler { destination = Destination.DEVICES }
    }

    Scaffold(
        containerColor = screenColor,
        snackbarHost = { SnackbarHost(snackbar) },
        bottomBar = {
            if (destination.inBar) {
                BottomBar(
                    destination = destination,
                    pending = requests.pending + mediated.pending,
                    onSelect = { destination = it },
                )
            }
        },
    ) { padding ->
        val content = Modifier.padding(padding)
        when (destination) {
            Destination.DASHBOARD ->
                DashboardDestination(
                    panel = dashboard,
                    held = held,
                    busy = history.busy,
                    modifier = content,
                )
            Destination.DEVICES ->
                DevicesScreen(
                    state = state,
                    clipboardOffer = clipboardOffer,
                    onRefresh = onRefresh,
                    onSendClipboard = onSendClipboard,
                    onDismissClipboard = onDismissClipboard,
                    onSetConfirmation = onSetDeviceConfirmation,
                    onSetRemoteSettings = onSetDeviceRemoteSettings,
                    onRename = onRenameDevice,
                    onRevoke = onRevokeDevice,
                    modifier = content,
                    onPause = onPause,
                    onAdd = { destination = Destination.ADD },
                    mediated = mediated,
                    onIntroduce = onIntroduceDevices,
                    onCancelMediated = onCancelMediated,
                )
            Destination.ADD ->
                AddScreen(
                    state = pairing,
                    onRoleChange = onPairingRoleChange,
                    onOpenWindow = onOpenPairingWindow,
                    onExtendWindow = onExtendPairingWindow,
                    onCloseWindow = onClosePairingWindow,
                    onRefreshCandidates = onRefreshPairingCandidates,
                    onPairWith = onPairWith,
                    onConfirm = onConfirmPairing,
                    onReject = onRejectPairing,
                    onScan = onScanPairingCode,
                    onAcceptLink = onAcceptPairingLink,
                    onDismissLink = onDismissPairingLink,
                    onCancelScan = onCancelScan,
                    onDismissResult = onDismissPairingResult,
                    modifier = content,
                    onClose = { destination = Destination.DEVICES },
                )
            Destination.REQUESTS -> {
                LaunchedEffect(Unit) {
                    onRefreshRequests()
                    onRefreshMediated()
                }
                RequestsScreen(
                    requests = requests,
                    mediated = mediated,
                    onRefresh = {
                        onRefreshRequests()
                        onRefreshMediated()
                    },
                    onAccept = onAcceptRequest,
                    onDecline = onDeclineRequest,
                    onAcceptMediated = onAcceptMediated,
                    onDeclineMediated = onDeclineMediated,
                    modifier = content,
                )
            }
            Destination.HISTORY ->
                HistoryScreen(
                    history = history,
                    status = state.status,
                    held = held,
                    onRefresh = onRefreshHistory,
                    onKindChange = onHistoryKindChange,
                    onDeviceChange = onHistoryDeviceChange,
                    onCopy = onCopyHistoryItem,
                    onApply = onApplyHistoryItem,
                    onDownload = onDownloadHistoryItem,
                    onDelete = onDeleteHistoryItem,
                    onClear = onClearHistory,
                    modifier = content,
                )
            Destination.SETTINGS ->
                SettingsScreen(
                    state = state,
                    deviceName = deviceName,
                    operatingMode = operatingMode,
                    policy = policy,
                    ttl = ttl,
                    dashboardLimit = dashboardLimit,
                    onDeviceNameChange = onDeviceNameChange,
                    onOperatingModeChange = onOperatingModeChange,
                    onDashboardLimitChange = onDashboardLimitChange,
                    onPolicyChange = onPolicyChange,
                    onPolicyReset = onPolicyReset,
                    onTtlSave = onTtlSave,
                    onTtlReset = onTtlReset,
                    downloadsFolder = downloadsFolder,
                    onDownloadsFolderPick = onDownloadsFolderPick,
                    onDownloadsFolderReset = onDownloadsFolderReset,
                    onPause = onPause,
                    onRelaySave = onRelaySave,
                    onRelayOff = onRelayOff,
                    onRefreshRelay = onRefreshRelay,
                    onRelayCheck = onRelayCheck,
                    onSelfConfirmationChange = onSelfConfirmationChange,
                    onAllowRemoteSettingsChange = onAllowRemoteSettingsChange,
                    onFriendsDiscoveryChange = onFriendsDiscoveryChange,
                    onLeaveGroup = onLeaveGroup,
                    diagnosticsEnabled = diagnosticsEnabled,
                    onDiagnosticsChange = onDiagnosticsChange,
                    notifications = notifications,
                    onNotificationsChange = onNotificationsChange,
                    journalEnabled = journalEnabled,
                    onJournalChange = onJournalChange,
                    onExportSettings = onExportSettings,
                    onImportSettings = onImportSettings,
                    stripTracking = stripTracking,
                    onStripTrackingChange = onStripTrackingChange,
                    updates = updates,
                    onRefresh = onRefresh,
                    modifier = content,
                )
        }
    }

    PromptDialog(prompts = prompts, onAnswer = onAnswerPrompt)
    UpdateDialog(panel = updates)
}

/** Bar of the top level destinations. */
@Composable
private fun BottomBar(
    destination: Destination,
    pending: Int,
    onSelect: (Destination) -> Unit,
) {
    NavigationBar(containerColor = MaterialTheme.colorScheme.surfaceContainerHigh) {
        Destination.entries.filter { it.inBar }.forEach { item ->
            NavigationBarItem(
                selected = item == destination,
                onClick = { onSelect(item) },
                icon = { DestinationIcon(item = item, pending = pending) },
                label = { Text(text = stringResource(item.label)) },
            )
        }
    }
}

/** The dashboard, reloaded whenever the destination opens. */
@Composable
private fun DashboardDestination(
    panel: DashboardPanel,
    held: Set<String>,
    busy: Set<String>,
    modifier: Modifier,
) {
    LaunchedEffect(Unit) { panel.onRefresh() }
    DashboardScreen(panel = panel, held = held, busy = busy, modifier = modifier)
}

/** Icon of a destination with the number of requests that wait for an answer. */
@Composable
private fun DestinationIcon(
    item: Destination,
    pending: Int,
) {
    if (item != Destination.REQUESTS || pending <= 0) {
        Icon(painter = painterResource(item.icon), contentDescription = null)
        return
    }
    BadgedBox(badge = { Badge { Text(text = pending.toString()) } }) {
        Icon(painter = painterResource(item.icon), contentDescription = null)
    }
}
