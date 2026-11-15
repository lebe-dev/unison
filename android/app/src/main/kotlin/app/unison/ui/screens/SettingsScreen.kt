package app.unison.ui.screens

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
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.unit.dp
import app.unison.R
import app.unison.clipboard.PauseOption
import app.unison.clipboard.PauseRequest
import app.unison.service.OperatingMode
import app.unison.ui.ErrorCard
import app.unison.ui.ScreenTitle
import app.unison.ui.SectionTitle
import app.unison.ui.pauseLabel
import app.unison.ui.state.CoreState
import app.unison.ui.state.DeviceNames
import unison.local.v1.Local

/** Device name, service mode and clipboard pause. */
@Composable
fun SettingsScreen(
    state: CoreState,
    deviceName: String,
    operatingMode: OperatingMode,
    onDeviceNameChange: (String) -> Unit,
    onOperatingModeChange: (OperatingMode) -> Unit,
    onPause: (PauseRequest) -> Unit,
    onRefresh: () -> Unit,
    modifier: Modifier = Modifier,
) {
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

        DeviceName(deviceName = deviceName, onDeviceNameChange = onDeviceNameChange)
        ServiceMode(operatingMode = operatingMode, onOperatingModeChange = onOperatingModeChange)
        Pause(pause = state.status?.pause, onPause = onPause)
        About(state = state)
        Spacer(modifier = Modifier.height(24.dp))
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
        TextButton(onClick = { onDeviceNameChange(saved) }, enabled = canSave) {
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
