package app.unison.ui.screens

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.selection.selectableGroup
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
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
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import app.unison.R
import app.unison.ui.SectionTitle
import app.unison.ui.groupItemColor
import app.unison.ui.mediatedStateLabel
import app.unison.ui.pairing.SasCode
import app.unison.ui.roleLabel
import app.unison.ui.shortId
import app.unison.ui.state.Introductions
import app.unison.ui.state.isActive
import unison.local.v1.Local
import androidx.compose.ui.semantics.Role as SemanticsRole

/**
 * Picks the second device and the role of an introduction. Only an online
 * device that speaks the feature can be introduced, and a chain with a Friends
 * device is limited to Friends.
 */
@Composable
internal fun IntroduceDialog(
    target: Local.Device,
    candidates: List<Local.Device>,
    starting: Boolean,
    deviceSummary: @Composable (Local.Device) -> String,
    onStart: (Local.Device, Local.Role) -> Unit,
    onDismiss: () -> Unit,
) {
    var peerKey by rememberSaveable { mutableStateOf(candidates.firstOrNull()?.deviceId.orEmpty()) }
    val peer = candidates.firstOrNull { it.deviceId == peerKey } ?: candidates.firstOrNull()
    var roleName by rememberSaveable {
        mutableStateOf(
            peer?.let { Introductions.defaultRole(target, it) }?.name
                ?: Local.Role.ROLE_PERSONAL.name,
        )
    }
    val roles = peer?.let { Introductions.roles(target, it) } ?: listOf(Local.Role.ROLE_PERSONAL)
    val role = roles.firstOrNull { it.name == roleName } ?: roles.first()

    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(text = stringResource(R.string.devices_introduce_title, deviceDisplayName(target))) },
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
                CandidateRadioList(
                    candidates = candidates,
                    selectedId = peer?.deviceId.orEmpty(),
                    deviceSummary = deviceSummary,
                    onSelect = { candidate ->
                        peerKey = candidate.deviceId
                        roleName = Introductions.defaultRole(target, candidate).name
                    },
                )
                SectionTitle(text = stringResource(R.string.devices_introduce_role))
                RoleRadioList(
                    roles = roles,
                    selected = role,
                    onSelect = { roleName = it.name },
                )
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

@Composable
private fun CandidateRadioList(
    candidates: List<Local.Device>,
    selectedId: String,
    deviceSummary: @Composable (Local.Device) -> String,
    onSelect: (Local.Device) -> Unit,
) {
    Column(modifier = Modifier.selectableGroup()) {
        candidates.forEach { candidate ->
            val selected = candidate.deviceId == selectedId
            Row(
                modifier =
                    Modifier
                        .fillMaxWidth()
                        .selectable(
                            selected = selected,
                            onClick = { onSelect(candidate) },
                            role = SemanticsRole.RadioButton,
                        ).padding(vertical = 6.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                RadioButton(selected = selected, onClick = null)
                Column(modifier = Modifier.padding(start = 8.dp)) {
                    Text(
                        text = deviceDisplayName(candidate),
                        style = MaterialTheme.typography.bodyLarge,
                    )
                    Text(
                        text = deviceSummary(candidate),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
        }
    }
}

@Composable
private fun RoleRadioList(
    roles: List<Local.Role>,
    selected: Local.Role,
    onSelect: (Local.Role) -> Unit,
) {
    Column(modifier = Modifier.selectableGroup()) {
        roles.forEach { option ->
            Row(
                modifier =
                    Modifier
                        .fillMaxWidth()
                        .selectable(
                            selected = option == selected,
                            onClick = { onSelect(option) },
                            role = SemanticsRole.RadioButton,
                        ).padding(vertical = 6.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                RadioButton(selected = option == selected, onClick = null)
                Text(
                    text = roleLabel(option).orEmpty(),
                    style = MaterialTheme.typography.bodyLarge,
                    modifier = Modifier.padding(start = 8.dp),
                )
            }
        }
    }
}

/** Progress of an introduction this device runs, with the code and a way to cancel. */
@Composable
internal fun IntroduceProgressCard(
    pair: Local.MediatedPair,
    canceling: Boolean,
    onCancel: () -> Unit,
) {
    val first = pair.peerA.name.ifBlank { shortId(pair.peerA.deviceId) }
    val second = pair.peerB.name.ifBlank { shortId(pair.peerB.deviceId) }
    val running = pair.isActive
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

private fun deviceDisplayName(device: Local.Device): String = device.name.ifEmpty { shortId(device.deviceId) }
