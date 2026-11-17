package app.unison.ui

import android.net.Uri
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Badge
import androidx.compose.material3.BadgedBox
import androidx.activity.compose.BackHandler
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
import app.unison.service.OperatingMode
import app.unison.ui.screens.AddScreen
import app.unison.ui.screens.DevicesScreen
import app.unison.ui.screens.HistoryScreen
import app.unison.ui.screens.RequestsScreen
import app.unison.ui.screens.SettingsScreen
import app.unison.ui.state.CoreState
import app.unison.ui.state.HistoryState
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
    pairing: PairingState,
    requests: RequestsState,
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
    downloadsFolder: String?,
    onDownloadsFolderPick: (Uri) -> Unit,
    onDownloadsFolderReset: () -> Unit,
    onPause: (PauseRequest) -> Unit,
    onRelaySave: (url: String, token: String?, filesOnRequest: Boolean?) -> Unit,
    onRelayOff: () -> Unit,
    onRefreshRelay: () -> Unit,
    onSelfConfirmationChange: (Local.ConfirmationScope, Boolean) -> Unit,
    onLeaveGroup: () -> Unit,
    onSetDeviceConfirmation: (Local.Device, Local.ConfirmationScope, Local.ConfirmationSide, Boolean) -> Unit,
    onRenameDevice: (Local.Device, String) -> Unit,
    onRevokeDevice: (Local.Device) -> Unit,
    onRefreshHistory: () -> Unit,
    onHistoryKindChange: (Local.Kind) -> Unit,
    onHistoryDeviceChange: (String) -> Unit,
    onCopyHistoryItem: (Local.HistoryItem) -> Unit,
    onApplyHistoryItem: (Local.HistoryItem) -> Unit,
    onDownloadHistoryItem: (Local.HistoryItem) -> Unit,
    onDeleteHistoryItem: (Local.HistoryItem) -> Unit,
    onRefreshRequests: () -> Unit,
    onAcceptRequest: (Local.ShareRequest) -> Unit,
    onDeclineRequest: (Local.ShareRequest) -> Unit,
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
                NavigationBar(containerColor = MaterialTheme.colorScheme.surfaceContainerHigh) {
                    Destination.entries.filter { it.inBar }.forEach { item ->
                        NavigationBarItem(
                            selected = item == destination,
                            onClick = { destination = item },
                            icon = {
                                DestinationIcon(item = item, pending = requests.pending)
                            },
                            label = { Text(text = stringResource(item.label)) },
                        )
                    }
                }
            }
        },
    ) { padding ->
        val content = Modifier.padding(padding)
        when (destination) {
            Destination.DEVICES -> DevicesScreen(
                state = state,
                clipboardOffer = clipboardOffer,
                onRefresh = onRefresh,
                onSendClipboard = onSendClipboard,
                onDismissClipboard = onDismissClipboard,
                onSetConfirmation = onSetDeviceConfirmation,
                onRename = onRenameDevice,
                onRevoke = onRevokeDevice,
                modifier = content,
                onPause = onPause,
                onAdd = { destination = Destination.ADD },
            )
            Destination.ADD -> AddScreen(
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
                LaunchedEffect(Unit) { onRefreshRequests() }
                RequestsScreen(
                    requests = requests,
                    onRefresh = onRefreshRequests,
                    onAccept = onAcceptRequest,
                    onDecline = onDeclineRequest,
                    modifier = content,
                )
            }
            Destination.HISTORY -> HistoryScreen(
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
                modifier = content,
            )
            Destination.SETTINGS -> SettingsScreen(
                state = state,
                deviceName = deviceName,
                operatingMode = operatingMode,
                policy = policy,
                ttl = ttl,
                onDeviceNameChange = onDeviceNameChange,
                onOperatingModeChange = onOperatingModeChange,
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
                onSelfConfirmationChange = onSelfConfirmationChange,
                onLeaveGroup = onLeaveGroup,
                onRefresh = onRefresh,
                modifier = content,
            )
        }
    }

    PromptDialog(prompts = prompts, onAnswer = onAnswerPrompt)
}

/** Icon of a destination with the number of requests that wait for an answer. */
@Composable
private fun DestinationIcon(item: Destination, pending: Int) {
    if (item != Destination.REQUESTS || pending <= 0) {
        Icon(painter = painterResource(item.icon), contentDescription = null)
        return
    }
    BadgedBox(badge = { Badge { Text(text = pending.toString()) } }) {
        Icon(painter = painterResource(item.icon), contentDescription = null)
    }
}
