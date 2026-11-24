package app.unison.ui.screens

import android.content.Intent
import android.text.format.DateFormat
import androidx.activity.compose.BackHandler
import androidx.annotation.DrawableRes
import androidx.compose.foundation.background
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.selection.selectableGroup
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExtendedFloatingActionButton
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LocalContentColor
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import app.unison.R
import app.unison.clipboard.ClipboardDraft
import app.unison.clipboard.PauseOption
import app.unison.clipboard.PauseRequest
import app.unison.clipboard.SendClipboardActivity
import app.unison.ui.ConfirmDialog
import app.unison.ui.ErrorCard
import app.unison.ui.GroupItem
import app.unison.ui.IconTile
import app.unison.ui.ItemGroup
import app.unison.ui.NetworkIssueCard
import app.unison.ui.ScreenTitle
import app.unison.ui.SectionTitle
import app.unison.ui.groupItemColor
import app.unison.ui.groupShape
import app.unison.ui.mediatedStateLabel
import app.unison.ui.networkLabel
import app.unison.ui.pairing.SasCode
import app.unison.ui.pathLabel
import app.unison.ui.pauseLabel
import app.unison.ui.platformLabel
import app.unison.ui.roleLabel
import app.unison.ui.state.CoreState
import app.unison.ui.state.DeviceNames
import app.unison.ui.state.Introductions
import app.unison.ui.state.MediatedState
import app.unison.ui.state.NetworkIssues
import unison.local.v1.Local
import java.util.Date
import androidx.compose.ui.semantics.Role as SemanticsRole

/** Paired devices and the state of this device; a tap on a device opens its page. */
@Composable
fun DevicesScreen(
    state: CoreState,
    clipboardOffer: ClipboardDraft?,
    onRefresh: () -> Unit,
    onSendClipboard: (ClipboardDraft) -> Unit,
    onDismissClipboard: () -> Unit,
    onSetConfirmation: (Local.Device, Local.ConfirmationScope, Local.ConfirmationSide, Boolean) -> Unit,
    onSetRemoteSettings: (Local.Device, Boolean) -> Unit,
    onRename: (Local.Device, String) -> Unit,
    onRevoke: (Local.Device) -> Unit,
    modifier: Modifier = Modifier,
    onPause: (PauseRequest) -> Unit = {},
    onAdd: () -> Unit = {},
    mediated: MediatedState = MediatedState(),
    onIntroduce: (peerA: String, peerB: String, role: Local.Role) -> Unit = { _, _, _ -> },
    onCancelMediated: (Local.MediatedPair) -> Unit = {},
) {
    var openKey by rememberSaveable { mutableStateOf<String?>(null) }
    var revokeTarget by remember { mutableStateOf<Local.Device?>(null) }
    var renameTarget by remember { mutableStateOf<Local.Device?>(null) }
    var renameDraft by remember { mutableStateOf("") }
    var introduceTarget by remember { mutableStateOf<Local.Device?>(null) }
    val startRename = { device: Local.Device ->
        renameTarget = device
        renameDraft = device.name
    }
    val open = openKey?.let { key -> state.status?.devicesList?.firstOrNull { keyOf(it) == key } }

    if (open != null) {
        BackHandler { openKey = null }
        DeviceDetail(
            device = open,
            onBack = { openKey = null },
            onSetConfirmation = onSetConfirmation,
            onSetRemoteSettings = onSetRemoteSettings,
            onRename = startRename,
            onRemove = { revokeTarget = it },
            onIntroduce = { introduceTarget = it },
            modifier = modifier,
        )
    } else {
        Box(modifier = modifier.fillMaxSize()) {
            DeviceList(
                state = state,
                clipboardOffer = clipboardOffer,
                onRefresh = onRefresh,
                onSendClipboard = onSendClipboard,
                onDismissClipboard = onDismissClipboard,
                onPause = onPause,
                onOpen = { openKey = keyOf(it) },
                mediated = mediated,
                onCancelMediated = onCancelMediated,
            )
            ExtendedFloatingActionButton(
                onClick = onAdd,
                icon = { Icon(painter = painterResource(R.drawable.ic_nav_add), contentDescription = null) },
                text = { Text(text = stringResource(R.string.devices_add)) },
                modifier =
                    Modifier
                        .align(Alignment.BottomEnd)
                        .padding(16.dp),
            )
        }
    }

    revokeTarget?.let { device ->
        ConfirmDialog(
            title = stringResource(R.string.devices_remove_title, displayName(device)),
            body = stringResource(R.string.devices_remove_body),
            onConfirm = {
                revokeTarget = null
                openKey = null
                onRevoke(device)
            },
            onDismiss = { revokeTarget = null },
        )
    }

    renameTarget?.let { device ->
        val name = renameDraft.trim()
        AlertDialog(
            onDismissRequest = { renameTarget = null },
            title = { Text(text = stringResource(R.string.devices_rename_title, displayName(device))) },
            text = {
                OutlinedTextField(
                    value = renameDraft,
                    onValueChange = { renameDraft = it },
                    modifier = Modifier.fillMaxWidth(),
                    label = { Text(text = stringResource(R.string.devices_rename_hint)) },
                    singleLine = true,
                )
            },
            confirmButton = {
                TextButton(
                    onClick = {
                        renameTarget = null
                        onRename(device, name)
                    },
                    enabled = DeviceNames.valid(name) && name != device.name,
                ) {
                    Text(text = stringResource(R.string.action_save))
                }
            },
            dismissButton = {
                TextButton(onClick = { renameTarget = null }) {
                    Text(text = stringResource(R.string.action_cancel))
                }
            },
        )
    }

    introduceTarget?.let { target ->
        IntroduceDialog(
            target = target,
            candidates =
                state
                    .introducibleDevices()
                    .filter {
                        Introductions.canIntroduce(it) &&
                            it.deviceId.isNotEmpty() &&
                            it.deviceId != target.deviceId
                    },
            starting = mediated.starting,
            onStart = { peer, role ->
                introduceTarget = null
                onIntroduce(target.deviceId, peer.deviceId, role)
            },
            onDismiss = { introduceTarget = null },
        )
    }
}

/** This device on top, then the paired devices by role. */
@Composable
private fun DeviceList(
    state: CoreState,
    clipboardOffer: ClipboardDraft?,
    onRefresh: () -> Unit,
    onSendClipboard: (ClipboardDraft) -> Unit,
    onDismissClipboard: () -> Unit,
    onPause: (PauseRequest) -> Unit,
    onOpen: (Local.Device) -> Unit,
    mediated: MediatedState,
    onCancelMediated: (Local.MediatedPair) -> Unit,
) {
    Column(
        modifier =
            Modifier
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
                modifier =
                    Modifier
                        .fillMaxWidth()
                        .padding(vertical = 32.dp),
                contentAlignment = Alignment.Center,
            ) {
                CircularProgressIndicator()
            }
        }

        mediated.outgoing.forEach { pair ->
            IntroduceProgressCard(
                pair = pair,
                canceling = pair.sessionId in mediated.canceling,
                onCancel = { onCancelMediated(pair) },
            )
            Spacer(modifier = Modifier.height(16.dp))
        }

        state.status?.let { status ->
            ThisDevice(status = status, onPause = onPause)
            Spacer(modifier = Modifier.height(16.dp))
        }

        if (clipboardOffer != null) {
            ClipboardOffer(
                draft = clipboardOffer,
                onSend = onSendClipboard,
                onDismiss = onDismissClipboard,
            )
        }

        state.status?.let {
            DeviceSection(
                title = stringResource(R.string.devices_section_personal),
                empty = stringResource(R.string.devices_empty_personal),
                devices = state.devices(Local.Role.ROLE_PERSONAL),
                onOpen = onOpen,
            )
            DeviceSection(
                title = stringResource(R.string.devices_section_friends),
                empty = stringResource(R.string.devices_empty_friends),
                devices = state.devices(Local.Role.ROLE_FRIENDS),
                onOpen = onOpen,
            )
        }
        // Room for the floating button over the last row.
        Spacer(modifier = Modifier.height(FAB_ROOM))
    }
}

/** Name, network and clipboard state of this device with its two quick actions. */
@Composable
private fun ThisDevice(
    status: Local.GetStatusResponse,
    onPause: (PauseRequest) -> Unit,
) {
    val context = LocalContext.current
    CompositionLocalProvider(LocalContentColor provides MaterialTheme.colorScheme.onPrimaryContainer) {
        Column(
            modifier =
                Modifier
                    .fillMaxWidth()
                    .clip(MaterialTheme.shapes.extraLarge)
                    .background(MaterialTheme.colorScheme.primaryContainer)
                    .padding(20.dp),
            verticalArrangement = Arrangement.spacedBy(16.dp),
        ) {
            Row(
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(16.dp),
            ) {
                IconTile(
                    icon = R.drawable.ic_device_phone,
                    container = MaterialTheme.colorScheme.primary,
                    content = MaterialTheme.colorScheme.onPrimary,
                    size = 56.dp,
                )
                Column {
                    Text(
                        text = stringResource(R.string.devices_this_device),
                        style = MaterialTheme.typography.labelLarge,
                    )
                    Text(text = status.deviceName, style = MaterialTheme.typography.titleLarge)
                    Text(
                        text =
                            stringResource(
                                R.string.device_summary,
                                networkLabel(status.network),
                                pauseLabel(status.pause),
                            ),
                        style = MaterialTheme.typography.bodyMedium,
                    )
                }
            }
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Button(
                    onClick = { context.startActivity(Intent(context, SendClipboardActivity::class.java)) },
                    modifier =
                        Modifier
                            .weight(1f)
                            .heightIn(min = 48.dp),
                ) {
                    Icon(
                        painter = painterResource(R.drawable.ic_send),
                        contentDescription = null,
                        modifier = Modifier.size(18.dp),
                    )
                    Spacer(modifier = Modifier.width(8.dp))
                    Text(text = stringResource(R.string.send_clipboard), maxLines = 1)
                }
                PauseButton(pause = status.pause, onPause = onPause)
            }
        }
    }
}

/** Pause of the clipboard: a menu of durations, or resume while paused. */
@Composable
private fun PauseButton(
    pause: Local.Pause?,
    onPause: (PauseRequest) -> Unit,
) {
    if (pause?.paused == true) {
        OutlinedButton(
            onClick = { onPause(PauseRequest.resume()) },
            modifier = Modifier.heightIn(min = 48.dp),
        ) {
            Text(text = stringResource(R.string.pause_resume))
        }
        return
    }
    var expanded by remember { mutableStateOf(false) }
    Box {
        OutlinedButton(
            onClick = { expanded = true },
            modifier = Modifier.heightIn(min = 48.dp),
        ) {
            Icon(
                painter = painterResource(R.drawable.ic_pause),
                contentDescription = null,
                modifier = Modifier.size(18.dp),
            )
            Spacer(modifier = Modifier.width(8.dp))
            Text(text = stringResource(R.string.action_pause))
        }
        DropdownMenu(expanded = expanded, onDismissRequest = { expanded = false }) {
            PauseOption.entries.forEach { option ->
                DropdownMenuItem(
                    text = { Text(text = stringResource(option.label)) },
                    onClick = {
                        expanded = false
                        onPause(option.request(System.currentTimeMillis()))
                    },
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
    Column(
        modifier =
            Modifier
                .fillMaxWidth()
                .clip(MaterialTheme.shapes.large)
                .background(groupItemColor)
                .padding(start = 16.dp, end = 16.dp, top = 16.dp, bottom = 8.dp),
    ) {
        Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            Icon(
                painter = painterResource(R.drawable.ic_copy),
                contentDescription = null,
                tint = MaterialTheme.colorScheme.primary,
            )
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = stringResource(R.string.clipboard_offer_title),
                    style = MaterialTheme.typography.titleMedium,
                )
                val preview = draft.preview()
                if (preview.isNotEmpty()) {
                    Text(
                        text = preview,
                        style = MaterialTheme.typography.bodyMedium,
                        maxLines = 3,
                        overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.padding(top = 4.dp),
                    )
                }
                Text(
                    text = stringResource(R.string.clipboard_offer_body),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.padding(top = 4.dp),
                )
            }
        }
        Row(
            modifier =
                Modifier
                    .fillMaxWidth()
                    .padding(top = 8.dp),
            horizontalArrangement = Arrangement.spacedBy(8.dp, Alignment.End),
        ) {
            TextButton(onClick = onDismiss) {
                Text(text = stringResource(R.string.action_dismiss))
            }
            FilledTonalButton(onClick = { onSend(draft) }) {
                Text(text = stringResource(R.string.action_send))
            }
        }
    }
}

/** Warning inside a device row when the device speaks a newer protocol. */
@Composable
private fun InlineProtocolWarning(device: Local.Device) {
    Column(
        modifier =
            Modifier
                .fillMaxWidth()
                .padding(top = 6.dp)
                .clip(MaterialTheme.shapes.small)
                .background(MaterialTheme.colorScheme.errorContainer)
                .padding(horizontal = 10.dp, vertical = 6.dp),
    ) {
        Text(
            text = stringResource(R.string.device_protocol_newer_summary, displayName(device)),
            style = MaterialTheme.typography.titleSmall,
            color = MaterialTheme.colorScheme.onErrorContainer,
        )
        Text(
            text = stringResource(R.string.device_protocol_newer_hint),
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onErrorContainer,
            modifier = Modifier.padding(top = 2.dp),
        )
    }
}

/** One role of the devices with its rows. */
@Composable
private fun DeviceSection(
    title: String,
    empty: String,
    devices: List<Local.Device>,
    onOpen: (Local.Device) -> Unit,
) {
    SectionTitle(text = title)
    if (devices.isEmpty()) {
        Text(
            text = empty,
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.padding(start = 4.dp),
        )
        return
    }
    ItemGroup {
        devices.forEachIndexed { index, device ->
            GroupItem(index = index, count = devices.size, onClick = { onOpen(device) }) {
                DeviceTile(device = device, size = 44.dp)
                Column(modifier = Modifier.weight(1f)) {
                    Text(text = displayName(device), style = MaterialTheme.typography.titleMedium)
                    Text(
                        text = stringResource(R.string.device_row_summary, statusLabel(device), detailLabel(device)),
                        style = MaterialTheme.typography.bodyMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                    if (NetworkIssues.isNewerProtocol(device)) {
                        InlineProtocolWarning(device = device)
                    }
                }
                Icon(
                    painter = painterResource(R.drawable.ic_chevron_right),
                    contentDescription = null,
                    tint = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }
}

/** Page of one paired device: its state, the confirmation flags, rename and removal. */
@Composable
private fun DeviceDetail(
    device: Local.Device,
    onBack: () -> Unit,
    onSetConfirmation: (Local.Device, Local.ConfirmationScope, Local.ConfirmationSide, Boolean) -> Unit,
    onSetRemoteSettings: (Local.Device, Boolean) -> Unit,
    onRename: (Local.Device) -> Unit,
    onRemove: (Local.Device) -> Unit,
    onIntroduce: (Local.Device) -> Unit,
    modifier: Modifier = Modifier,
) {
    Column(
        modifier =
            modifier
                .fillMaxSize()
                .verticalScroll(rememberScrollState())
                .padding(horizontal = 16.dp),
    ) {
        Row(
            modifier =
                Modifier
                    .fillMaxWidth()
                    .padding(top = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            IconButton(onClick = onBack) {
                Icon(
                    painter = painterResource(R.drawable.ic_arrow_back),
                    contentDescription = stringResource(R.string.action_back),
                )
            }
            Spacer(modifier = Modifier.weight(1f))
            IconButton(onClick = { onRename(device) }) {
                Icon(
                    painter = painterResource(R.drawable.ic_edit),
                    contentDescription = stringResource(R.string.devices_rename),
                )
            }
        }

        Column(
            modifier = Modifier.fillMaxWidth(),
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            IconTile(
                icon = platformIcon(device.platform),
                container = MaterialTheme.colorScheme.primaryContainer,
                content = MaterialTheme.colorScheme.onPrimaryContainer,
                size = 88.dp,
            )
            Text(text = displayName(device), style = MaterialTheme.typography.headlineMedium)
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                DetailChip(text = statusLabel(device), dot = statusColor(device), highlighted = true)
                roleLabel(device.role)?.let { DetailChip(text = it) }
                DetailChip(text = detailLabel(device))
            }
        }

        if (NetworkIssues.isUnreachable(device)) {
            val timeFormat = DateFormat.getTimeFormat(LocalContext.current)
            val since =
                stringResource(
                    R.string.device_unreachable_since,
                    timeFormat.format(Date(device.unreachableSinceUnixMs)),
                )
            Spacer(modifier = Modifier.height(16.dp))
            NetworkIssueCard(
                summary = stringResource(R.string.device_unreachable_summary, displayName(device)),
                hint = stringResource(R.string.device_unreachable_hint),
                details = NetworkIssues.deviceDetails(device) { since },
            )
        }

        if (NetworkIssues.isNewerProtocol(device)) {
            Spacer(modifier = Modifier.height(16.dp))
            NetworkIssueCard(
                summary = stringResource(R.string.device_protocol_newer_summary, displayName(device)),
                hint = stringResource(R.string.device_protocol_newer_hint),
                details = "",
            )
        }

        if (device.role == Local.Role.ROLE_PERSONAL) {
            Spacer(modifier = Modifier.height(8.dp))
            ConfirmationFlags(
                device = device,
                onSet = onSetConfirmation,
                onSetRemoteSettings = onSetRemoteSettings,
            )
        }

        Spacer(modifier = Modifier.height(24.dp))
        val canIntroduce = Introductions.canIntroduce(device) && device.deviceId.isNotEmpty()
        val actions = if (canIntroduce) 3 else 2
        ItemGroup {
            if (canIntroduce) {
                GroupItem(index = 0, count = actions, onClick = { onIntroduce(device) }) {
                    Icon(
                        painter = painterResource(R.drawable.ic_nav_devices),
                        contentDescription = null,
                        tint = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                    Text(
                        text = stringResource(R.string.devices_introduce_action),
                        style = MaterialTheme.typography.bodyLarge,
                        modifier = Modifier.weight(1f),
                    )
                }
            }
            GroupItem(index = actions - 2, count = actions, onClick = { onRename(device) }) {
                Icon(
                    painter = painterResource(R.drawable.ic_edit),
                    contentDescription = null,
                    tint = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                Text(
                    text = stringResource(R.string.devices_rename_action),
                    style = MaterialTheme.typography.bodyLarge,
                    modifier = Modifier.weight(1f),
                )
            }
            GroupItem(index = actions - 1, count = actions, onClick = { onRemove(device) }) {
                Icon(
                    painter = painterResource(R.drawable.ic_delete),
                    contentDescription = null,
                    tint = MaterialTheme.colorScheme.error,
                )
                Text(
                    text = stringResource(R.string.devices_remove),
                    style = MaterialTheme.typography.bodyLarge,
                    color = MaterialTheme.colorScheme.error,
                    modifier = Modifier.weight(1f),
                )
            }
        }
        Spacer(modifier = Modifier.height(24.dp))
    }
}

/** Small label of the device page, with a status dot when [dot] is set. */
@Composable
private fun DetailChip(
    text: String,
    dot: Color? = null,
    highlighted: Boolean = false,
) {
    Row(
        modifier =
            Modifier
                .clip(MaterialTheme.shapes.small)
                .background(
                    if (highlighted) {
                        MaterialTheme.colorScheme.secondaryContainer
                    } else {
                        MaterialTheme.colorScheme.surfaceContainerHighest
                    },
                ).padding(horizontal = 12.dp, vertical = 6.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        if (dot != null) {
            Box(
                modifier =
                    Modifier
                        .size(8.dp)
                        .clip(CircleShape)
                        .background(dot),
            )
        }
        Text(text = text, style = MaterialTheme.typography.labelLarge)
    }
}

/**
 * The four confirmation flags of a Personal device: this device and the device
 * each ask, or not, before they take the clipboard and the files of the other.
 * The flags of the device change only while it is online. A separate switch
 * below them lets the device change these flags from its side.
 */
@Composable
private fun ConfirmationFlags(
    device: Local.Device,
    onSet: (Local.Device, Local.ConfirmationScope, Local.ConfirmationSide, Boolean) -> Unit,
    onSetRemoteSettings: (Local.Device, Boolean) -> Unit,
) {
    val name = device.name.ifBlank { stringResource(R.string.prompt_unknown_device) }
    val local = { scope: Local.ConfirmationScope, require: Boolean ->
        onSet(device, scope, Local.ConfirmationSide.CONFIRMATION_SIDE_LOCAL, require)
    }
    val peer = { scope: Local.ConfirmationScope, require: Boolean ->
        onSet(device, scope, Local.ConfirmationSide.CONFIRMATION_SIDE_PEER, require)
    }
    SectionTitle(text = stringResource(R.string.devices_confirmation))
    ItemGroup {
        FlagHeader(index = 0, text = stringResource(R.string.devices_confirmation_here, name))
        FlagRow(
            index = 1,
            label = stringResource(R.string.devices_confirmation_clipboard),
            checked = device.requireConfirmation,
            description = stringResource(R.string.devices_confirm_local_clipboard, name),
            enabled = true,
            onChange = { local(Local.ConfirmationScope.CONFIRMATION_SCOPE_CLIPBOARD, it) },
        )
        FlagRow(
            index = 2,
            label = stringResource(R.string.devices_confirmation_files),
            checked = device.requireFileConfirmation,
            description = stringResource(R.string.devices_confirm_local_files, name),
            enabled = true,
            onChange = { local(Local.ConfirmationScope.CONFIRMATION_SCOPE_FILES, it) },
        )
        FlagHeader(index = 3, text = stringResource(R.string.devices_confirmation_there, name))
        FlagRow(
            index = 4,
            label = stringResource(R.string.devices_confirmation_clipboard),
            checked = device.peerRequireConfirmation,
            description = stringResource(R.string.devices_confirm_peer_clipboard, name),
            enabled = device.online,
            onChange = { peer(Local.ConfirmationScope.CONFIRMATION_SCOPE_CLIPBOARD, it) },
        )
        FlagRow(
            index = 5,
            label = stringResource(R.string.devices_confirmation_files),
            checked = device.peerRequireFileConfirmation,
            description = stringResource(R.string.devices_confirm_peer_files, name),
            enabled = device.online,
            onChange = { peer(Local.ConfirmationScope.CONFIRMATION_SCOPE_FILES, it) },
        )
    }
    if (!device.online) {
        Text(
            text = stringResource(R.string.devices_confirmation_offline_hint, name),
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.padding(start = 4.dp, top = 8.dp),
        )
    }
    Spacer(modifier = Modifier.height(8.dp))
    ItemGroup {
        FlagRow(
            index = 0,
            count = 1,
            label = stringResource(R.string.devices_allow_remote, name),
            checked = device.allowRemoteSettings,
            description = stringResource(R.string.devices_allow_remote_hint, name),
            enabled = true,
            onChange = { onSetRemoteSettings(device, it) },
        )
    }
}

/** Heading of one side of the confirmation flags, inside the group. */
@Composable
private fun FlagHeader(
    index: Int,
    text: String,
) {
    Text(
        text = text,
        style = MaterialTheme.typography.labelLarge,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        modifier =
            Modifier
                .fillMaxWidth()
                .clip(groupShape(index, FLAG_ROWS))
                .background(groupItemColor)
                .padding(start = 16.dp, end = 16.dp, top = 14.dp, bottom = 6.dp),
    )
}

/** One switch of the confirmation flags. */
@Composable
private fun FlagRow(
    index: Int,
    label: String,
    checked: Boolean,
    description: String,
    enabled: Boolean,
    onChange: (Boolean) -> Unit,
    count: Int = FLAG_ROWS,
) {
    GroupItem(index = index, count = count) {
        Text(
            text = label,
            style = MaterialTheme.typography.bodyLarge,
            modifier = Modifier.weight(1f),
        )
        Switch(
            checked = checked,
            enabled = enabled,
            onCheckedChange = onChange,
            modifier = Modifier.semantics { contentDescription = description },
        )
    }
}

/**
 * Picks the second device and the role of an introduction. Only an online
 * device that speaks the feature can be introduced, and a chain with a Friends
 * device is limited to Friends.
 */
@Composable
private fun IntroduceDialog(
    target: Local.Device,
    candidates: List<Local.Device>,
    starting: Boolean,
    onStart: (Local.Device, Local.Role) -> Unit,
    onDismiss: () -> Unit,
) {
    var peerKey by rememberSaveable { mutableStateOf(candidates.firstOrNull()?.deviceId.orEmpty()) }
    val peer = candidates.firstOrNull { it.deviceId == peerKey }
    var roleName by rememberSaveable {
        mutableStateOf(
            candidates.firstOrNull()?.let { Introductions.defaultRole(target, it) }?.name
                ?: Local.Role.ROLE_PERSONAL.name,
        )
    }
    LaunchedEffect(peerKey) {
        val chosen = candidates.firstOrNull { it.deviceId == peerKey }
        if (chosen != null) {
            roleName = Introductions.defaultRole(target, chosen).name
        }
    }
    val roles = peer?.let { Introductions.roles(target, it) } ?: listOf(Local.Role.ROLE_PERSONAL)
    val role = roles.firstOrNull { it.name == roleName } ?: roles.first()

    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(text = stringResource(R.string.devices_introduce_title, displayName(target))) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                if (candidates.isEmpty()) {
                    Text(
                        text = stringResource(R.string.devices_introduce_empty),
                        style = MaterialTheme.typography.bodyMedium,
                    )
                    return@Column
                }
                Text(
                    text = stringResource(R.string.devices_introduce_hint),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                SectionTitle(text = stringResource(R.string.devices_introduce_pick))
                Column(modifier = Modifier.selectableGroup()) {
                    candidates.forEach { candidate ->
                        Row(
                            modifier =
                                Modifier
                                    .fillMaxWidth()
                                    .selectable(
                                        selected = candidate.deviceId == peerKey,
                                        onClick = { peerKey = candidate.deviceId },
                                        role = SemanticsRole.RadioButton,
                                    ).padding(vertical = 6.dp),
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            RadioButton(selected = candidate.deviceId == peerKey, onClick = null)
                            Column(modifier = Modifier.padding(start = 8.dp)) {
                                Text(
                                    text = displayName(candidate),
                                    style = MaterialTheme.typography.bodyLarge,
                                )
                                Text(
                                    text =
                                        stringResource(
                                            R.string.device_row_summary,
                                            statusLabel(candidate),
                                            detailLabel(candidate),
                                        ),
                                    style = MaterialTheme.typography.bodySmall,
                                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                                )
                            }
                        }
                    }
                }
                SectionTitle(text = stringResource(R.string.devices_introduce_role))
                Column(modifier = Modifier.selectableGroup()) {
                    roles.forEach { option ->
                        Row(
                            modifier =
                                Modifier
                                    .fillMaxWidth()
                                    .selectable(
                                        selected = option == role,
                                        onClick = { roleName = option.name },
                                        role = SemanticsRole.RadioButton,
                                    ).padding(vertical = 6.dp),
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            RadioButton(selected = option == role, onClick = null)
                            Text(
                                text = roleLabel(option).orEmpty(),
                                style = MaterialTheme.typography.bodyLarge,
                                modifier = Modifier.padding(start = 8.dp),
                            )
                        }
                    }
                }
                if (roles.size == 1 && roles.single() == Local.Role.ROLE_FRIENDS) {
                    Text(
                        text = stringResource(R.string.devices_introduce_role_locked),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
        },
        confirmButton = {
            TextButton(
                onClick = { peer?.let { onStart(it, role) } },
                enabled = peer != null && !starting,
            ) {
                Text(text = stringResource(R.string.devices_introduce_start))
            }
        },
        dismissButton = {
            TextButton(onClick = onDismiss) {
                Text(text = stringResource(R.string.action_cancel))
            }
        },
    )
}

/** Progress of an introduction this device runs, with the code and a way to cancel. */
@Composable
private fun IntroduceProgressCard(
    pair: Local.MediatedPair,
    canceling: Boolean,
    onCancel: () -> Unit,
) {
    val first = pair.peerA.name.ifBlank { pair.peerA.deviceId.take(SHORT_ID_LENGTH) }
    val second = pair.peerB.name.ifBlank { pair.peerB.deviceId.take(SHORT_ID_LENGTH) }
    val running =
        pair.state == Local.MediatedPairState.MEDIATED_PAIR_STATE_WAITING ||
            pair.state == Local.MediatedPairState.MEDIATED_PAIR_STATE_READY ||
            pair.state == Local.MediatedPairState.MEDIATED_PAIR_STATE_AWAITING_FINISH
    Column(
        modifier =
            Modifier
                .fillMaxWidth()
                .clip(MaterialTheme.shapes.large)
                .background(groupItemColor)
                .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Row(
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            if (running) {
                CircularProgressIndicator(modifier = Modifier.size(20.dp), strokeWidth = 2.dp)
            } else {
                Icon(
                    painter = painterResource(R.drawable.ic_nav_devices),
                    contentDescription = null,
                    tint = MaterialTheme.colorScheme.primary,
                )
            }
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = stringResource(R.string.devices_introduce_progress_title, first, second),
                    style = MaterialTheme.typography.titleMedium,
                )
                Text(
                    text = mediatedStateLabel(pair.state),
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
        if (pair.hasSas()) {
            Text(
                text = stringResource(R.string.devices_introduce_sas_hint),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            SasCode(sas = pair.sas)
        }
        if (running) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.End,
            ) {
                TextButton(onClick = onCancel, enabled = !canceling) {
                    Text(text = stringResource(R.string.action_cancel))
                }
            }
        }
    }
}

/** Platform icon of a device in a tile with its status dot. */
@Composable
private fun DeviceTile(
    device: Local.Device,
    size: Dp,
) {
    val friends = device.role == Local.Role.ROLE_FRIENDS
    IconTile(
        icon = platformIcon(device.platform),
        container =
            if (friends) {
                MaterialTheme.colorScheme.tertiaryContainer
            } else {
                MaterialTheme.colorScheme.surfaceContainerHighest
            },
        content =
            if (friends) {
                MaterialTheme.colorScheme.onTertiaryContainer
            } else {
                MaterialTheme.colorScheme.onSurfaceVariant
            },
        size = size,
        badge = statusColor(device),
    )
}

/** Online, offline, or not answering although it should. */
@Composable
private fun statusLabel(device: Local.Device): String =
    stringResource(
        when {
            device.online -> R.string.device_online
            NetworkIssues.isUnreachable(device) -> R.string.devices_not_answering
            else -> R.string.device_offline
        },
    )

/** Platform of a device, with the network path while it is online. */
@Composable
private fun detailLabel(device: Local.Device): String {
    val path = pathLabel(device.path)
    if (path == null || !device.online) {
        return platformLabel(device.platform)
    }
    return stringResource(R.string.device_detail, platformLabel(device.platform), path)
}

/** Color of the status dot: green online, red not answering, grey offline. */
@Composable
private fun statusColor(device: Local.Device): Color =
    when {
        device.online -> if (isSystemInDarkTheme()) Color(0xFF81C784) else Color(0xFF2E7D32)
        NetworkIssues.isUnreachable(device) -> MaterialTheme.colorScheme.error
        else -> MaterialTheme.colorScheme.outline
    }

/** Icon of the operating system of a device. */
@DrawableRes
private fun platformIcon(platform: Local.Platform): Int =
    when (platform) {
        Local.Platform.PLATFORM_MACOS -> R.drawable.ic_device_laptop
        Local.Platform.PLATFORM_WINDOWS -> R.drawable.ic_device_desktop
        Local.Platform.PLATFORM_ANDROID -> R.drawable.ic_device_phone
        else -> R.drawable.ic_nav_devices
    }

/** Name of a device, or the start of its ID when the name is empty. */
private fun displayName(device: Local.Device): String = device.name.ifEmpty { device.deviceId.take(SHORT_ID_LENGTH) }

/** Key of a device that survives a configuration change. */
private fun keyOf(device: Local.Device): String = device.deviceId.ifEmpty { device.name }

/** Characters of the device ID shown when the name is empty. */
private const val SHORT_ID_LENGTH = 8

/** Rows in the group of confirmation flags: two headings and four switches. */
private const val FLAG_ROWS = 6

/** Space under the list so the floating button does not cover the last row. */
private val FAB_ROOM = 96.dp
