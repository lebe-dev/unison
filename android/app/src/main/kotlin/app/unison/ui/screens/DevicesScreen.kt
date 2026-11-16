package app.unison.ui.screens

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Card
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import app.unison.R
import app.unison.clipboard.ClipboardDraft
import app.unison.ui.ConfirmDialog
import app.unison.ui.ErrorCard
import app.unison.ui.ScreenTitle
import app.unison.ui.SectionTitle
import app.unison.ui.networkLabel
import app.unison.ui.pathLabel
import app.unison.ui.pauseLabel
import app.unison.ui.platformLabel
import app.unison.ui.state.CoreState
import unison.local.v1.Local

/** Paired devices and the state of this device. */
@Composable
fun DevicesScreen(
    state: CoreState,
    clipboardOffer: ClipboardDraft?,
    onRefresh: () -> Unit,
    onSendClipboard: (ClipboardDraft) -> Unit,
    onDismissClipboard: () -> Unit,
    onSetConfirmation: (Local.Device, Boolean) -> Unit,
    onRevoke: (Local.Device) -> Unit,
    modifier: Modifier = Modifier,
) {
    var revokeTarget by remember { mutableStateOf<Local.Device?>(null) }

    Column(
        modifier = modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(horizontal = 16.dp),
    ) {
        ScreenTitle(title = stringResource(R.string.nav_devices)) {
            TextButton(onClick = onRefresh) {
                Text(text = stringResource(R.string.action_refresh))
            }
        }

        state.failure?.let { failure ->
            ErrorCard(message = stringResource(failure.message), onRetry = onRefresh)
            Spacer(modifier = Modifier.height(16.dp))
        }

        if (state.loading) {
            Box(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(vertical = 32.dp),
                contentAlignment = Alignment.Center,
            ) {
                CircularProgressIndicator()
            }
        }

        if (clipboardOffer != null) {
            ClipboardOffer(
                draft = clipboardOffer,
                onSend = onSendClipboard,
                onDismiss = onDismissClipboard,
            )
            Spacer(modifier = Modifier.height(16.dp))
        }

        state.status?.let { status ->
            ThisDevice(status = status)
            DeviceSection(
                title = stringResource(R.string.devices_section_personal),
                empty = stringResource(R.string.devices_empty_personal),
                devices = state.devices(Local.Role.ROLE_PERSONAL),
                onSetConfirmation = onSetConfirmation,
                onRemove = { revokeTarget = it },
            )
            DeviceSection(
                title = stringResource(R.string.devices_section_friends),
                empty = stringResource(R.string.devices_empty_friends),
                devices = state.devices(Local.Role.ROLE_FRIENDS),
                onSetConfirmation = onSetConfirmation,
                onRemove = { revokeTarget = it },
            )
            Spacer(modifier = Modifier.height(24.dp))
        }
    }

    revokeTarget?.let { device ->
        ConfirmDialog(
            title = stringResource(
                R.string.devices_remove_title,
                device.name.ifEmpty { device.deviceId.take(SHORT_ID_LENGTH) },
            ),
            body = stringResource(R.string.devices_remove_body),
            onConfirm = {
                revokeTarget = null
                onRevoke(device)
            },
            onDismiss = { revokeTarget = null },
        )
    }
}

/** Name, network and clipboard state of this device. */
@Composable
private fun ThisDevice(status: Local.GetStatusResponse) {
    Card(modifier = Modifier.fillMaxWidth()) {
        Column(modifier = Modifier.padding(16.dp)) {
            Text(
                text = stringResource(R.string.devices_this_device),
                style = MaterialTheme.typography.labelMedium,
                color = MaterialTheme.colorScheme.primary,
            )
            Text(
                text = status.deviceName,
                style = MaterialTheme.typography.titleMedium,
            )
            Spacer(modifier = Modifier.height(4.dp))
            Text(
                text = stringResource(
                    R.string.device_summary,
                    networkLabel(status.network),
                    pauseLabel(status.pause),
                ),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}

/** One role of the devices with its rows. */
@Composable
private fun DeviceSection(
    title: String,
    empty: String,
    devices: List<Local.Device>,
    onSetConfirmation: (Local.Device, Boolean) -> Unit,
    onRemove: (Local.Device) -> Unit,
) {
    SectionTitle(text = title)
    if (devices.isEmpty()) {
        Text(
            text = empty,
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        return
    }
    Column {
        devices.forEachIndexed { index, device ->
            DeviceRow(
                device = device,
                onSetConfirmation = onSetConfirmation,
                onRemove = onRemove,
            )
            if (index != devices.lastIndex) {
                HorizontalDivider()
            }
        }
    }
}

/** One paired device with its live connection state and its settings. */
@Composable
private fun DeviceRow(
    device: Local.Device,
    onSetConfirmation: (Local.Device, Boolean) -> Unit,
    onRemove: (Local.Device) -> Unit,
) {
    Column(modifier = Modifier.fillMaxWidth()) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(top = 12.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            Box(
                modifier = Modifier
                    .size(10.dp)
                    .clip(CircleShape)
                    .background(
                        if (device.online) {
                            MaterialTheme.colorScheme.primary
                        } else {
                            MaterialTheme.colorScheme.outline
                        },
                    ),
            )
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = device.name.ifEmpty { device.deviceId.take(SHORT_ID_LENGTH) },
                    style = MaterialTheme.typography.titleMedium,
                )
                val path = pathLabel(device.path)
                Text(
                    text = if (path != null && device.online) {
                        stringResource(R.string.device_detail, platformLabel(device.platform), path)
                    } else {
                        platformLabel(device.platform)
                    },
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            Text(
                text = stringResource(
                    if (device.online) R.string.device_online else R.string.device_offline,
                ),
                style = MaterialTheme.typography.labelMedium,
                color = if (device.online) {
                    MaterialTheme.colorScheme.primary
                } else {
                    MaterialTheme.colorScheme.onSurfaceVariant
                },
            )
        }

        if (device.role == Local.Role.ROLE_PERSONAL) {
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(top = 4.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(
                    text = stringResource(R.string.devices_confirmation),
                    style = MaterialTheme.typography.bodyMedium,
                    modifier = Modifier.weight(1f),
                )
                Switch(
                    checked = device.requireConfirmation,
                    onCheckedChange = { onSetConfirmation(device, it) },
                )
            }
        }

        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.End,
        ) {
            TextButton(onClick = { onRemove(device) }) {
                Text(
                    text = stringResource(R.string.action_remove),
                    color = MaterialTheme.colorScheme.error,
                )
            }
        }
    }
}

/** Offer to send the clipboard that changed since the last synchronization. */
@Composable
private fun ClipboardOffer(
    draft: ClipboardDraft,
    onSend: (ClipboardDraft) -> Unit,
    onDismiss: () -> Unit,
) {
    Card(modifier = Modifier.fillMaxWidth()) {
        Column(modifier = Modifier.padding(16.dp)) {
            Text(
                text = stringResource(R.string.clipboard_offer_title),
                style = MaterialTheme.typography.titleMedium,
            )
            val preview = draft.preview()
            if (preview.isNotEmpty()) {
                Spacer(modifier = Modifier.height(8.dp))
                Text(text = preview, style = MaterialTheme.typography.bodyMedium, maxLines = 3)
            }
            Spacer(modifier = Modifier.height(8.dp))
            Text(
                text = stringResource(R.string.clipboard_offer_body),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(top = 8.dp),
                horizontalArrangement = Arrangement.End,
            ) {
                TextButton(onClick = onDismiss) {
                    Text(text = stringResource(R.string.action_dismiss))
                }
                TextButton(onClick = { onSend(draft) }) {
                    Text(text = stringResource(R.string.action_send))
                }
            }
        }
    }
}

/** Characters of the device ID shown when the name is empty. */
private const val SHORT_ID_LENGTH = 8
