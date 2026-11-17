package app.unison.ui.screens

import android.net.Uri
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.selection.selectableGroup
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.unit.dp
import app.unison.R
import app.unison.clipboard.PauseOption
import app.unison.clipboard.PauseRequest
import app.unison.history.HistoryTtl
import app.unison.policy.PolicyConfig
import app.unison.service.OperatingMode
import app.unison.ui.ConfirmDialog
import app.unison.ui.ErrorCard
import app.unison.ui.ScreenTitle
import app.unison.ui.SectionTitle
import app.unison.ui.SettingsCard
import app.unison.ui.pauseLabel
import app.unison.ui.state.CoreState
import app.unison.ui.state.DeviceNames
import unison.local.v1.Local

/** Device name, service mode, network policy, history lifetimes and clipboard pause. */
@Composable
fun SettingsScreen(
    state: CoreState,
    deviceName: String,
    operatingMode: OperatingMode,
    policy: PolicyConfig,
    ttl: HistoryTtl,
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
    diagnosticsEnabled: Boolean,
    onDiagnosticsChange: (Boolean) -> Unit,
    stripTracking: Boolean,
    onStripTrackingChange: (Boolean) -> Unit,
    onRefresh: () -> Unit,
    modifier: Modifier = Modifier,
) {
    var leaving by remember { mutableStateOf(false) }

    Column(
        modifier = modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(horizontal = 16.dp),
    ) {
        ScreenTitle(title = stringResource(R.string.nav_settings))

        state.failure?.let { failure ->
            ErrorCard(message = stringResource(failure.message), onRetry = onRefresh)
            Spacer(modifier = Modifier.height(16.dp))
        }

        SettingsCard {
            DeviceName(deviceName = deviceName, onDeviceNameChange = onDeviceNameChange)
        }
        SettingsCard {
            ServiceMode(operatingMode = operatingMode, onOperatingModeChange = onOperatingModeChange)
        }
        SettingsCard {
            Confirmation(
                clipboard = state.requiresConfirmation,
                files = state.requiresFileConfirmation,
                onChange = onSelfConfirmationChange,
            )
        }
        SettingsCard {
            Links(
                enabled = stripTracking,
                onChange = onStripTrackingChange,
            )
        }
        SettingsCard {
            Diagnostics(
                enabled = diagnosticsEnabled,
                onChange = onDiagnosticsChange,
            )
        }
        SettingsCard {
            Pause(pause = state.status?.pause, onPause = onPause)
        }
        SettingsCard {
            DownloadsFolderSection(
                folder = downloadsFolder,
                onPick = onDownloadsFolderPick,
                onReset = onDownloadsFolderReset,
            )
        }
        SettingsCard {
            PolicySection(
                policy = policy,
                network = state.status?.network,
                onPolicyChange = onPolicyChange,
                onReset = onPolicyReset,
            )
        }
        SettingsCard {
            RelaySection(
                relay = state.relay,
                onSave = onRelaySave,
                onOff = onRelayOff,
                onRefresh = onRefreshRelay,
            )
        }
        SettingsCard {
            HistoryTtlSection(ttl = ttl, onSave = onTtlSave, onReset = onTtlReset)
        }
        SettingsCard {
            Group(onLeave = { leaving = true })
        }
        state.status?.let {
            SettingsCard {
                About(state = state)
            }
        }
        Spacer(modifier = Modifier.height(24.dp))
    }

    if (leaving) {
        ConfirmDialog(
            title = stringResource(R.string.settings_leave_title),
            body = stringResource(R.string.settings_leave_body),
            onConfirm = {
                leaving = false
                onLeaveGroup()
            },
            onDismiss = { leaving = false },
        )
    }
}

/** Name announced to the other devices. */
@Composable
private fun DeviceName(deviceName: String, onDeviceNameChange: (String) -> Unit) {
    var draft by rememberSaveable(deviceName) { mutableStateOf(deviceName) }
    val saved = draft.trim()
    val canSave = DeviceNames.valid(draft) && saved != deviceName

    SectionTitle(text = stringResource(R.string.settings_section_device))
    OutlinedTextField(
        value = draft,
        onValueChange = { draft = it },
        modifier = Modifier.fillMaxWidth(),
        label = { Text(text = stringResource(R.string.settings_name_hint)) },
        singleLine = true,
    )
    Row(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.End,
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            text = stringResource(R.string.settings_name_applies),
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.weight(1f),
        )
        TextButton(
            onClick = { onDeviceNameChange(saved) },
            enabled = canSave,
            modifier = Modifier.testTag(NAME_SAVE_TAG),
        ) {
            Text(text = stringResource(R.string.action_save))
        }
    }
}

/** How long the service keeps the core running. */
@Composable
private fun ServiceMode(
    operatingMode: OperatingMode,
    onOperatingModeChange: (OperatingMode) -> Unit,
) {
    SectionTitle(text = stringResource(R.string.settings_section_service))
    Column(modifier = Modifier.selectableGroup()) {
        OperatingMode.entries.forEach { mode ->
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .selectable(
                        selected = mode == operatingMode,
                        onClick = { onOperatingModeChange(mode) },
                        role = Role.RadioButton,
                    )
                    .padding(vertical = 8.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                RadioButton(selected = mode == operatingMode, onClick = null)
                Column(modifier = Modifier.padding(start = 8.dp)) {
                    Text(
                        text = stringResource(mode.label()),
                        style = MaterialTheme.typography.bodyLarge,
                    )
                    Text(
                        text = stringResource(mode.hint()),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
        }
    }
}

/**
 * Folder of the received files. The system picker grants access to the picked
 * folder only; the app asks for no storage permission.
 */
@Composable
private fun DownloadsFolderSection(
    folder: String?,
    onPick: (Uri) -> Unit,
    onReset: () -> Unit,
) {
    val picker = rememberLauncherForActivityResult(ActivityResultContracts.OpenDocumentTree()) { uri ->
        if (uri != null) {
            onPick(uri)
        }
    }

    SectionTitle(text = stringResource(R.string.settings_section_downloads))
    Text(
        text = folder ?: stringResource(R.string.settings_downloads_default),
        style = MaterialTheme.typography.bodyLarge,
    )
    Text(
        text = stringResource(R.string.settings_downloads_hint),
        style = MaterialTheme.typography.bodySmall,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
    )
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(top = 8.dp),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        OutlinedButton(
            onClick = { picker.launch(null) },
            modifier = Modifier.testTag(DOWNLOADS_PICK_TAG),
        ) {
            Text(text = stringResource(R.string.settings_downloads_choose))
        }
        OutlinedButton(
            onClick = onReset,
            enabled = folder != null,
            modifier = Modifier.testTag(DOWNLOADS_RESET_TAG),
        ) {
            Text(text = stringResource(R.string.settings_downloads_reset))
        }
    }
}

/** Pause of the clipboard synchronization. */
@Composable
private fun Pause(pause: Local.Pause?, onPause: (PauseRequest) -> Unit) {
    SectionTitle(text = stringResource(R.string.pause_title))
    Text(
        text = pauseLabel(pause),
        style = MaterialTheme.typography.bodyMedium,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
    )
    Column(
        modifier = Modifier.padding(top = 8.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        if (pause?.paused == true) {
            OutlinedButton(
                onClick = { onPause(PauseRequest.resume()) },
                modifier = Modifier.fillMaxWidth(),
            ) {
                Text(text = stringResource(R.string.pause_resume))
            }
        }
        PauseOption.entries.forEach { option ->
            OutlinedButton(
                onClick = { onPause(option.request(System.currentTimeMillis())) },
                modifier = Modifier.fillMaxWidth(),
            ) {
                Text(text = stringResource(option.label))
            }
        }
    }
}

/** Version and identity of the running core. */
@Composable
private fun About(state: CoreState) {
    val status = state.status ?: return
    SectionTitle(text = stringResource(R.string.settings_section_about))
    Text(
        text = stringResource(R.string.core_version, status.version),
        style = MaterialTheme.typography.bodySmall,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
    )
    Text(
        text = stringResource(R.string.settings_device_id, status.deviceId),
        style = MaterialTheme.typography.bodySmall,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        modifier = Modifier.padding(top = 4.dp),
    )
}

/** Confirmation of the clipboard items and the files of every device. */
@Composable
private fun Confirmation(
    clipboard: Boolean,
    files: Boolean,
    onChange: (Local.ConfirmationScope, Boolean) -> Unit,
) {
    SectionTitle(text = stringResource(R.string.settings_section_confirmation))
    SettingsSwitch(
        title = stringResource(R.string.settings_confirmation),
        hint = stringResource(R.string.settings_confirmation_hint),
        checked = clipboard,
        tag = CONFIRMATION_TAG,
        onChange = { onChange(Local.ConfirmationScope.CONFIRMATION_SCOPE_CLIPBOARD, it) },
    )
    SettingsSwitch(
        title = stringResource(R.string.settings_file_confirmation),
        hint = stringResource(R.string.settings_file_confirmation_hint),
        checked = files,
        tag = FILE_CONFIRMATION_TAG,
        onChange = { onChange(Local.ConfirmationScope.CONFIRMATION_SCOPE_FILES, it) },
    )
}

/** Removal of the advertising parameters from the links of the clipboard. */
@Composable
private fun Links(enabled: Boolean, onChange: (Boolean) -> Unit) {
    SectionTitle(text = stringResource(R.string.settings_section_links))
    SettingsSwitch(
        title = stringResource(R.string.settings_strip_tracking),
        hint = stringResource(R.string.settings_strip_tracking_hint),
        checked = enabled,
        tag = STRIP_TRACKING_TAG,
        onChange = onChange,
    )
}

/** Reporting of the crashes and the errors of this device to the developer. */
@Composable
private fun Diagnostics(enabled: Boolean, onChange: (Boolean) -> Unit) {
    SectionTitle(text = stringResource(R.string.settings_section_diagnostics))
    SettingsSwitch(
        title = stringResource(R.string.settings_diagnostics),
        hint = stringResource(R.string.settings_diagnostics_hint),
        checked = enabled,
        tag = DIAGNOSTICS_TAG,
        onChange = onChange,
    )
}

/** One switch of a settings section. */
@Composable
private fun SettingsSwitch(
    title: String,
    hint: String,
    checked: Boolean,
    tag: String,
    onChange: (Boolean) -> Unit,
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(bottom = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(modifier = Modifier.weight(1f)) {
            Text(text = title, style = MaterialTheme.typography.bodyLarge)
            Text(
                text = hint,
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        Switch(
            checked = checked,
            onCheckedChange = onChange,
            modifier = Modifier.testTag(tag),
        )
    }
}

/** Membership of this device in the group of Personal devices. */
@Composable
private fun Group(onLeave: () -> Unit) {
    SectionTitle(text = stringResource(R.string.settings_section_group))
    Text(
        text = stringResource(R.string.settings_leave_hint),
        style = MaterialTheme.typography.bodySmall,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
    )
    OutlinedButton(
        onClick = onLeave,
        modifier = Modifier
            .fillMaxWidth()
            .padding(top = 8.dp),
    ) {
        Text(
            text = stringResource(R.string.settings_leave_group),
            color = MaterialTheme.colorScheme.error,
        )
    }
}

private fun OperatingMode.label(): Int = when (this) {
    OperatingMode.ALWAYS -> R.string.mode_always
    OperatingMode.WIFI_ONLY -> R.string.mode_wifi_only
    OperatingMode.APP_OPEN -> R.string.mode_app_open
}

private fun OperatingMode.hint(): Int = when (this) {
    OperatingMode.ALWAYS -> R.string.mode_always_hint
    OperatingMode.WIFI_ONLY -> R.string.mode_wifi_only_hint
    OperatingMode.APP_OPEN -> R.string.mode_app_open_hint
}

/** Tag of the switch of the clipboard confirmation of this device. */
internal const val CONFIRMATION_TAG = "settings.confirmation"

/** Tag of the switch of the files confirmation of this device. */
internal const val FILE_CONFIRMATION_TAG = "settings.confirmation.files"

/** Tag of the switch of the error reporting. */
internal const val DIAGNOSTICS_TAG = "settings.diagnostics"

/** Tag of the switch of the tracking parameters removal. */
internal const val STRIP_TRACKING_TAG = "settings.strip_tracking"

/** Tag of the button that opens the folder picker for received files. */
internal const val DOWNLOADS_PICK_TAG = "settings.downloads.pick"

/** Tag of the button that brings back the default folder of received files. */
internal const val DOWNLOADS_RESET_TAG = "settings.downloads.reset"

/** Tag of the button that saves the device name. */
internal const val NAME_SAVE_TAG = "settings.name.save"
