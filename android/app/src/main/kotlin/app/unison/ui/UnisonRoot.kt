package app.unison.ui

import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Icon
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
import app.unison.service.OperatingMode
import app.unison.ui.screens.AddScreen
import app.unison.ui.screens.DevicesScreen
import app.unison.ui.screens.HistoryScreen
import app.unison.ui.screens.SettingsScreen
import app.unison.ui.state.CoreState
import app.unison.ui.state.HistoryState
import kotlinx.coroutines.flow.Flow
import unison.local.v1.Local

/** Bottom navigation and the screens above it. */
@Composable
fun UnisonRoot(
    state: CoreState,
    history: HistoryState,
    deviceName: String,
    operatingMode: OperatingMode,
    clipboardOffer: ClipboardDraft?,
    notices: Flow<Int>,
    onRefresh: () -> Unit,
    onSendClipboard: (ClipboardDraft) -> Unit,
    onDismissClipboard: () -> Unit,
    onDeviceNameChange: (String) -> Unit,
    onOperatingModeChange: (OperatingMode) -> Unit,
    onPause: (PauseRequest) -> Unit,
    onRefreshHistory: () -> Unit,
    onHistoryKindChange: (Local.Kind) -> Unit,
    onHistoryDeviceChange: (String) -> Unit,
    onCopyHistoryItem: (Local.HistoryItem) -> Unit,
    onDownloadHistoryItem: (Local.HistoryItem) -> Unit,
    onDeleteHistoryItem: (Local.HistoryItem) -> Unit,
) {
    var destination by rememberSaveable { mutableStateOf(Destination.DEFAULT) }
    val snackbar = remember { SnackbarHostState() }
    val resources = LocalResources.current
    LaunchedEffect(notices) {
        notices.collect { message -> snackbar.showSnackbar(resources.getString(message)) }
    }

    Scaffold(
        snackbarHost = { SnackbarHost(snackbar) },
        bottomBar = {
            NavigationBar {
                Destination.entries.forEach { item ->
                    NavigationBarItem(
                        selected = item == destination,
                        onClick = { destination = item },
                        icon = { Icon(painter = painterResource(item.icon), contentDescription = null) },
                        label = { Text(text = stringResource(item.label)) },
                    )
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
                modifier = content,
            )
            Destination.ADD -> AddScreen(modifier = content)
            Destination.HISTORY -> HistoryScreen(
                history = history,
                status = state.status,
                onRefresh = onRefreshHistory,
                onKindChange = onHistoryKindChange,
                onDeviceChange = onHistoryDeviceChange,
                onCopy = onCopyHistoryItem,
                onDownload = onDownloadHistoryItem,
                onDelete = onDeleteHistoryItem,
                modifier = content,
            )
            Destination.SETTINGS -> SettingsScreen(
                state = state,
                deviceName = deviceName,
                operatingMode = operatingMode,
                onDeviceNameChange = onDeviceNameChange,
                onOperatingModeChange = onOperatingModeChange,
                onPause = onPause,
                onRefresh = onRefresh,
                modifier = content,
            )
        }
    }
}
