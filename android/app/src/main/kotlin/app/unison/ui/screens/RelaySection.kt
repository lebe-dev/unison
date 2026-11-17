package app.unison.ui.screens

import androidx.annotation.StringRes
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import app.unison.R
import app.unison.ui.SectionTitle
import kotlinx.coroutines.delay
import unison.local.v1.Local

/** How often the section asks the core for the state of the relay client. */
private const val RELAY_POLL_MS = 5_000L

/** Relay address, token and the state of the connection. */
@Composable
fun RelaySection(
    relay: Local.Relay?,
    onSave: (url: String, token: String?, filesOnRequest: Boolean?) -> Unit,
    onOff: () -> Unit,
    onRefresh: () -> Unit,
    checking: Boolean,
    check: Local.TestRelayResponse?,
    onCheck: (url: String, token: String?) -> Unit,
) {
    var url by rememberSaveable(relay?.url) { mutableStateOf(relay?.url.orEmpty()) }
    // The token stays in memory only, so it never lands in a saved state.
    var token by remember { mutableStateOf("") }
    var filesOnRequest by rememberSaveable(relay?.filesOnRequest) {
        mutableStateOf(relay?.filesOnRequest ?: true)
    }
    val address = url.trim()
    val tokenStored = relay?.tokenSet == true
    val canSave = address.isNotEmpty() && (token.isNotEmpty() || tokenStored)

    LaunchedEffect(Unit) {
        while (true) {
            delay(RELAY_POLL_MS)
            onRefresh()
        }
    }

    SectionTitle(text = stringResource(R.string.relay_title))
    Text(
        text = relayStateText(relay),
        style = MaterialTheme.typography.bodyMedium,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
    )

    OutlinedTextField(
        value = url,
        onValueChange = { url = it },
        modifier = Modifier
            .fillMaxWidth()
            .padding(top = 8.dp)
            .testTag("relay_url"),
        label = { Text(text = stringResource(R.string.relay_url_label)) },
        singleLine = true,
    )
    Text(
        text = stringResource(R.string.relay_url_hint),
        style = MaterialTheme.typography.bodySmall,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
    )

    OutlinedTextField(
        value = token,
        onValueChange = { token = it },
        modifier = Modifier
            .fillMaxWidth()
            .padding(top = 8.dp)
            .testTag("relay_token"),
        label = { Text(text = stringResource(R.string.relay_token_label)) },
        visualTransformation = PasswordVisualTransformation(),
        singleLine = true,
    )
    Text(
        text = stringResource(
            if (tokenStored) R.string.relay_token_stored else R.string.relay_token_missing,
        ),
        style = MaterialTheme.typography.bodySmall,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
    )

    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(top = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(modifier = Modifier.weight(1f)) {
            Text(
                text = stringResource(R.string.relay_files),
                style = MaterialTheme.typography.bodyLarge,
            )
            Text(
                text = stringResource(R.string.relay_files_hint),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        Switch(
            checked = filesOnRequest,
            onCheckedChange = { filesOnRequest = it },
            modifier = Modifier.testTag("relay_files"),
        )
    }

    when {
        checking -> Text(
            text = stringResource(R.string.relay_checking),
            modifier = Modifier.padding(top = 8.dp),
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        check != null -> RelayCheckResult(check)
    }

    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        OutlinedButton(
            onClick = { onCheck(address, token.trim().takeIf { it.isNotEmpty() }) },
            modifier = Modifier
                .fillMaxWidth()
                .padding(top = 8.dp)
                .testTag("relay_check"),
            enabled = canSave && !checking,
        ) {
            Text(text = stringResource(R.string.relay_check))
        }
        TextButton(
            onClick = {
                val value = token.trim()
                onSave(address, value.takeIf { it.isNotEmpty() }, filesOnRequest)
                token = ""
            },
            enabled = canSave,
        ) {
            Text(text = stringResource(R.string.action_save))
        }
        OutlinedButton(
            onClick = onOff,
            modifier = Modifier.fillMaxWidth(),
            enabled = relay?.url?.isNotEmpty() == true,
        ) {
            Text(text = stringResource(R.string.relay_off))
        }
    }
}

/** State of the relay client in one line. */
@Composable
private fun relayStateText(relay: Local.Relay?): String {
    if (relay == null) {
        return stringResource(R.string.relay_state_unknown)
    }
    if (relay.url.isEmpty()) {
        return stringResource(R.string.relay_state_off)
    }
    if (!relay.connected) {
        return stringResource(R.string.relay_state_waiting)
    }
    return when (relay.transport) {
        Local.RelayTransport.RELAY_TRANSPORT_QUIC -> stringResource(R.string.relay_state_quic)
        Local.RelayTransport.RELAY_TRANSPORT_WSS -> stringResource(R.string.relay_state_wss)
        else -> stringResource(R.string.relay_state_connected)
    }
}

/** Outcome of a relay check: a summary and one line per transport. */
@Composable
private fun RelayCheckResult(check: Local.TestRelayResponse) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(top = 8.dp)
            .testTag("relay_check_result"),
        verticalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        Text(
            text = stringResource(relayCheckSummary(check)),
            style = MaterialTheme.typography.bodyMedium,
        )
        RelayCheckRow(label = stringResource(R.string.relay_check_quic), result = check.quic)
        RelayCheckRow(label = stringResource(R.string.relay_check_wss), result = check.websocket)
    }
}

/** Outcome of the check of one transport, with the technical detail of a failure. */
@Composable
private fun RelayCheckRow(label: String, result: Local.RelayCheck) {
    Column {
        Text(
            text = label,
            style = MaterialTheme.typography.labelLarge,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Text(
            text = if (result.ok) {
                stringResource(R.string.relay_check_ok, result.latencyMs)
            } else {
                stringResource(relayFailureText(result.failure))
            },
            style = MaterialTheme.typography.bodyMedium,
            color = if (result.ok) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.error,
        )
        if (!result.ok && result.detail.isNotEmpty()) {
            Text(
                text = result.detail,
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}

/** One line on what the check means for the devices. */
@StringRes
private fun relayCheckSummary(check: Local.TestRelayResponse): Int {
    val quic = check.quic.ok
    val wss = check.websocket.ok
    return when {
        quic && wss -> R.string.relay_check_both
        wss -> R.string.relay_check_wss_only
        quic -> R.string.relay_check_quic_only
        else -> R.string.relay_check_none
    }
}

/** Plain explanation of the reason a check failed. */
@StringRes
private fun relayFailureText(failure: Local.RelayCheckFailure): Int = when (failure) {
    Local.RelayCheckFailure.RELAY_CHECK_FAILURE_ADDRESS -> R.string.relay_fail_address
    Local.RelayCheckFailure.RELAY_CHECK_FAILURE_TIMEOUT -> R.string.relay_fail_timeout
    Local.RelayCheckFailure.RELAY_CHECK_FAILURE_REFUSED -> R.string.relay_fail_refused
    Local.RelayCheckFailure.RELAY_CHECK_FAILURE_CERTIFICATE -> R.string.relay_fail_certificate
    Local.RelayCheckFailure.RELAY_CHECK_FAILURE_NOT_RELAY -> R.string.relay_fail_not_relay
    Local.RelayCheckFailure.RELAY_CHECK_FAILURE_UNAVAILABLE -> R.string.relay_fail_unavailable
    Local.RelayCheckFailure.RELAY_CHECK_FAILURE_UNAUTHORIZED -> R.string.relay_fail_unauthorized
    Local.RelayCheckFailure.RELAY_CHECK_FAILURE_QUOTA_EXCEEDED -> R.string.relay_fail_quota
    Local.RelayCheckFailure.RELAY_CHECK_FAILURE_DATAGRAMS_TOO_SMALL -> R.string.relay_fail_datagrams
    else -> R.string.relay_fail_other
}
