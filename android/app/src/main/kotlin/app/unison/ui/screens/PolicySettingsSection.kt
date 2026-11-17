package app.unison.ui.screens

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalLifecycleOwner
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import app.unison.R
import app.unison.network.BackgroundData
import app.unison.policy.PolicyCondition
import app.unison.policy.PolicyConfig
import app.unison.policy.PolicyKind
import app.unison.policy.PolicyMode
import app.unison.policy.PolicyRow
import app.unison.policy.PolicySize
import app.unison.policy.conditionOf
import app.unison.ui.SectionTitle
import unison.local.v1.Local

/** The network policy: what arrives by itself on every kind of network. */
@Composable
fun PolicySection(
    policy: PolicyConfig,
    network: Local.NetworkState?,
    onPolicyChange: (PolicyConfig) -> Unit,
    onReset: () -> Unit,
    modifier: Modifier = Modifier,
) {
    SectionTitle(text = stringResource(R.string.policy_section), modifier = modifier)
    Text(
        text = stringResource(R.string.policy_now, conditionLabel(conditionOf(network))),
        style = MaterialTheme.typography.bodyMedium,
    )
    BackgroundDataNotice()
    Text(
        text = stringResource(R.string.policy_hint),
        style = MaterialTheme.typography.bodySmall,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        modifier = Modifier.padding(top = 4.dp),
    )
    for (condition in PolicyCondition.entries) {
        ConditionRow(
            condition = condition,
            row = policy[condition],
            onRowChange = { row -> onPolicyChange(policy.with(condition, row)) },
        )
    }
    RelayLimit(
        bytes = policy.relayMaxSizeBytes,
        onBytes = { onPolicyChange(policy.copy(relayMaxSizeBytes = it)) },
    )
    OutlinedButton(
        onClick = onReset,
        modifier = Modifier
            .fillMaxWidth()
            .padding(top = 16.dp),
    ) {
        Text(text = stringResource(R.string.policy_reset))
    }
}

/**
 * Warns that the system restricts the background data of the app and leads to
 * the page that lifts it. The status is read again when the screen comes back.
 */
@Composable
private fun BackgroundDataNotice() {
    val context = LocalContext.current
    var restricted by remember { mutableStateOf(BackgroundData.restricted(BackgroundData.status(context))) }
    val owner = LocalLifecycleOwner.current
    DisposableEffect(owner) {
        val observer = LifecycleEventObserver { _, event ->
            if (event == Lifecycle.Event.ON_RESUME) {
                restricted = BackgroundData.restricted(BackgroundData.status(context))
            }
        }
        owner.lifecycle.addObserver(observer)
        onDispose { owner.lifecycle.removeObserver(observer) }
    }
    if (!restricted) {
        return
    }

    Text(
        text = stringResource(R.string.policy_background_restricted),
        style = MaterialTheme.typography.bodySmall,
        color = MaterialTheme.colorScheme.error,
        modifier = Modifier.padding(top = 8.dp),
    )
    OutlinedButton(
        onClick = { BackgroundData.openSettings(context) },
        modifier = Modifier
            .padding(top = 8.dp)
            .testTag(BACKGROUND_DATA_TAG),
    ) {
        Text(text = stringResource(R.string.policy_background_open))
    }
}

/** Tag of the button that opens the background data settings of the app. */
internal const val BACKGROUND_DATA_TAG = "settings.policy.background_data"

/** One condition of the table with its cells and the relay permission. */
@Composable
private fun ConditionRow(
    condition: PolicyCondition,
    row: PolicyRow,
    onRowChange: (PolicyRow) -> Unit,
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(top = 16.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            text = stringResource(condition.label()),
            style = MaterialTheme.typography.titleSmall,
            modifier = Modifier.weight(1f),
        )
        Text(
            text = stringResource(R.string.policy_relay),
            style = MaterialTheme.typography.bodyMedium,
            modifier = Modifier.padding(end = 8.dp),
        )
        Switch(
            checked = row.relay,
            onCheckedChange = { onRowChange(row.copy(relay = it)) },
        )
    }
    for (kind in PolicyKind.entries) {
        CellRow(
            kind = kind,
            mode = row[kind].mode,
            maxSizeBytes = row[kind].maxSizeBytes,
            onMode = { mode -> onRowChange(row.with(kind, row[kind].copy(mode = mode))) },
            onMaxSize = { bytes -> onRowChange(row.with(kind, row[kind].copy(maxSizeBytes = bytes))) },
        )
    }
}

/** One cell: the outcome and the eager limit. */
@Composable
private fun CellRow(
    kind: PolicyKind,
    mode: PolicyMode,
    maxSizeBytes: Long,
    onMode: (PolicyMode) -> Unit,
    onMaxSize: (Long) -> Unit,
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(top = 4.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Text(
            text = stringResource(kind.label()),
            style = MaterialTheme.typography.bodyMedium,
            modifier = Modifier.weight(1f),
        )
        ModePicker(mode = mode, onMode = onMode)
        SizeField(
            bytes = maxSizeBytes,
            enabled = mode == PolicyMode.AUTO,
            onBytes = onMaxSize,
        )
    }
}

/** Outcome of a cell. */
@Composable
private fun ModePicker(mode: PolicyMode, onMode: (PolicyMode) -> Unit) {
    var expanded by remember { mutableStateOf(false) }
    Box {
        TextButton(onClick = { expanded = true }) {
            Text(text = stringResource(mode.label()))
        }
        DropdownMenu(expanded = expanded, onDismissRequest = { expanded = false }) {
            for (option in PolicyMode.entries) {
                DropdownMenuItem(
                    text = { Text(text = stringResource(option.label())) },
                    onClick = {
                        expanded = false
                        onMode(option)
                    },
                )
            }
        }
    }
}

/** Eager limit in megabytes, used only by the automatic outcome. */
@Composable
private fun SizeField(bytes: Long, enabled: Boolean, onBytes: (Long) -> Unit) {
    var draft by rememberSaveable(bytes) { mutableStateOf(PolicySize.toMegabytes(bytes).toString()) }
    OutlinedTextField(
        value = draft,
        onValueChange = { input ->
            if (input.length > MAX_DIGITS || !input.all { it.isDigit() }) {
                return@OutlinedTextField
            }
            draft = input
            input.toLongOrNull()?.takeIf { it > 0 }?.let { onBytes(PolicySize.ofMegabytes(it)) }
        },
        enabled = enabled,
        singleLine = true,
        label = { Text(text = stringResource(R.string.policy_limit)) },
        suffix = { Text(text = stringResource(R.string.policy_megabytes)) },
        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
        modifier = Modifier.width(SIZE_FIELD_WIDTH),
    )
}

/** Limit of the items fetched over the relay. */
@Composable
private fun RelayLimit(bytes: Long, onBytes: (Long) -> Unit) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(top = 16.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Text(
            text = stringResource(R.string.policy_relay_limit),
            style = MaterialTheme.typography.bodyMedium,
            modifier = Modifier.weight(1f),
        )
        SizeField(bytes = bytes, enabled = true, onBytes = onBytes)
    }
}

/** Label of the row the core uses now, the offline row when there is no network. */
@Composable
private fun conditionLabel(condition: PolicyCondition?): String =
    stringResource(condition?.label() ?: R.string.policy_condition_offline)

private fun PolicyCondition.label(): Int = when (this) {
    PolicyCondition.UNMETERED -> R.string.policy_condition_unmetered
    PolicyCondition.METERED -> R.string.policy_condition_metered
    PolicyCondition.ROAMING -> R.string.policy_condition_roaming
    PolicyCondition.DATA_SAVER -> R.string.policy_condition_data_saver
}

private fun PolicyKind.label(): Int = when (this) {
    PolicyKind.TEXT -> R.string.kind_text
    PolicyKind.IMAGE -> R.string.kind_image
    PolicyKind.FILES -> R.string.kind_files
}

private fun PolicyMode.label(): Int = when (this) {
    PolicyMode.REJECT -> R.string.policy_mode_reject
    PolicyMode.METADATA -> R.string.policy_mode_metadata
    PolicyMode.CONFIRM -> R.string.policy_mode_confirm
    PolicyMode.AUTO -> R.string.policy_mode_auto
}

private const val MAX_DIGITS = 5
private val SIZE_FIELD_WIDTH = 132.dp
