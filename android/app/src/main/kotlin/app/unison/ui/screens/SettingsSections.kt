package app.unison.ui.screens

import android.net.Uri
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.selection.selectableGroup
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
import app.unison.service.OperatingMode
import app.unison.ui.SectionTitle
import app.unison.ui.pauseLabel
import app.unison.ui.state.CoreState
import app.unison.ui.state.DeviceNames
import unison.local.v1.Local

/** Name announced to the other devices. */
@Composable
internal fun DeviceName(
    deviceName: String,
    onDeviceNameChange: (String) -> Unit,
) {
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
internal fun ServiceMode(
    operatingMode: OperatingMode,
    onOperatingModeChange: (OperatingMode) -> Unit,
) {
    SectionTitle(text = stringResource(R.string.settings_section_service))
    Column(modifier = Modifier.selectableGroup()) {
        OperatingMode.entries.forEach { mode ->
            Row(
                modifier =
                    Modifier
                        .fillMaxWidth()
                        .selectable(
                            selected = mode == operatingMode,
                            onClick = { onOperatingModeChange(mode) },
                            role = Role.RadioButton,
                        ).padding(vertical = 8.dp),
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
internal fun DownloadsFolderSection(
    folder: String?,
    onPick: (Uri) -> Unit,
    onReset: () -> Unit,
) {
    val picker =
        rememberLauncherForActivityResult(ActivityResultContracts.OpenDocumentTree()) { uri ->
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
        modifier =
            Modifier
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
internal fun Pause(
    pause: Local.Pause?,
    onPause: (PauseRequest) -> Unit,
) {
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
internal fun About(state: CoreState) {
    val status = state.status ?: return
    SectionTitle(text = stringResource(R.string.settings_section_version))
    Text(
        text = stringResource(R.string.core_version, status.version),
        style = MaterialTheme.typography.bodyLarge,
    )
    Text(
        text = stringResource(R.string.settings_device_id, status.deviceId),
        style = MaterialTheme.typography.bodySmall,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        modifier = Modifier.padding(top = 4.dp),
    )
}

/** Confirmation of the clipboard items of every device. */
@Composable
internal fun ClipboardConfirmation(
    required: Boolean,
    onChange: (Local.ConfirmationScope, Boolean) -> Unit,
) {
    SectionTitle(text = stringResource(R.string.settings_section_confirmation))
    SettingsSwitch(
        title = stringResource(R.string.settings_confirmation),
        hint = stringResource(R.string.settings_confirmation_hint),
        checked = required,
        tag = CONFIRMATION_TAG,
        onChange = { onChange(Local.ConfirmationScope.CONFIRMATION_SCOPE_CLIPBOARD, it) },
    )
}

/** Confirmation of the files of every device. */
@Composable
internal fun FileConfirmation(
    required: Boolean,
    onChange: (Local.ConfirmationScope, Boolean) -> Unit,
) {
    SectionTitle(text = stringResource(R.string.settings_section_confirmation))
    SettingsSwitch(
        title = stringResource(R.string.settings_file_confirmation),
        hint = stringResource(R.string.settings_file_confirmation_hint),
        checked = required,
        tag = FILE_CONFIRMATION_TAG,
        onChange = { onChange(Local.ConfirmationScope.CONFIRMATION_SCOPE_FILES, it) },
    )
}

/** Removal of the advertising parameters from the links of the clipboard. */
@Composable
internal fun Links(
    enabled: Boolean,
    onChange: (Boolean) -> Unit,
) {
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
internal fun Diagnostics(
    enabled: Boolean,
    onChange: (Boolean) -> Unit,
) {
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
internal fun SettingsSwitch(
    title: String,
    hint: String,
    checked: Boolean,
    tag: String,
    onChange: (Boolean) -> Unit,
) {
    Row(
        modifier =
            Modifier
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
internal fun Group(onLeave: () -> Unit) {
    SectionTitle(text = stringResource(R.string.settings_section_group))
    Text(
        text = stringResource(R.string.settings_leave_hint),
        style = MaterialTheme.typography.bodySmall,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
    )
    OutlinedButton(
        onClick = onLeave,
        modifier =
            Modifier
                .fillMaxWidth()
                .padding(top = 8.dp),
    ) {
        Text(
            text = stringResource(R.string.settings_leave_group),
            color = MaterialTheme.colorScheme.error,
        )
    }
}

internal fun OperatingMode.label(): Int =
    when (this) {
        OperatingMode.ALWAYS -> R.string.mode_always
        OperatingMode.WIFI_ONLY -> R.string.mode_wifi_only
        OperatingMode.APP_OPEN -> R.string.mode_app_open
    }

private fun OperatingMode.hint(): Int =
    when (this) {
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
