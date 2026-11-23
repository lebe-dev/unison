package app.unison.ui.screens

import android.os.SystemClock
import androidx.compose.foundation.layout.Arrangement
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
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Surface
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
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import app.unison.R
import app.unison.ui.GroupItem
import app.unison.ui.IconTile
import app.unison.ui.ItemGroup
import app.unison.ui.NetworkIssueCard
import app.unison.ui.ScreenTitle
import app.unison.ui.SectionTitle
import app.unison.ui.groupItemColor
import app.unison.ui.pairing.PairingCode
import app.unison.ui.pairing.PairingRequestBody
import app.unison.ui.pairing.rememberWindowRemaining
import app.unison.ui.roleLabel
import app.unison.ui.state.PairingLinkOffer
import app.unison.ui.state.PairingRequest
import app.unison.ui.state.PairingResult
import app.unison.ui.state.PairingState
import kotlinx.coroutines.delay
import unison.local.v1.Local

/** How long an open window may stay without a candidate before it warns. */
private const val QUIET_WINDOW_MS = 30_000L

/**
 * Entry point of pairing: the code of this device, the camera scanner and the
 * devices that are open for pairing.
 */
@Composable
fun AddScreen(
    state: PairingState,
    onRoleChange: (Local.Role) -> Unit,
    onOpenWindow: () -> Unit,
    onExtendWindow: () -> Unit,
    onCloseWindow: () -> Unit,
    onRefreshCandidates: () -> Unit,
    onPairWith: (String) -> Unit,
    onConfirm: () -> Unit,
    onReject: () -> Unit,
    onScan: (String) -> Unit,
    onCancelScan: () -> Unit,
    onDismissResult: () -> Unit,
    modifier: Modifier = Modifier,
    onAcceptLink: () -> Unit = {},
    onDismissLink: () -> Unit = {},
    onClose: (() -> Unit)? = null,
) {
    var scanning by rememberSaveable { mutableStateOf(false) }

    if (scanning) {
        ScanScreen(
            onCode = { code ->
                scanning = false
                onScan(code)
            },
            onClose = { scanning = false },
            modifier = modifier,
        )
    } else {
        Column(
            modifier =
                modifier
                    .fillMaxSize()
                    .verticalScroll(rememberScrollState())
                    .padding(horizontal = 16.dp),
        ) {
            if (onClose != null) {
                IconButton(onClick = onClose, modifier = Modifier.padding(top = 8.dp)) {
                    Icon(
                        painter = painterResource(R.drawable.ic_close),
                        contentDescription = stringResource(R.string.action_back),
                    )
                }
            }
            ScreenTitle(title = stringResource(R.string.add_title))
            Text(
                text = stringResource(R.string.add_body),
                style = MaterialTheme.typography.bodyLarge,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(horizontal = 4.dp),
            )
            Spacer(modifier = Modifier.height(16.dp))
            OutlinedButton(
                onClick = { scanning = true },
                enabled = !state.scanning,
                modifier =
                    Modifier
                        .fillMaxWidth()
                        .heightIn(min = 56.dp),
            ) {
                Icon(
                    painter = painterResource(R.drawable.ic_scan),
                    contentDescription = null,
                    modifier = Modifier.size(20.dp),
                )
                Spacer(modifier = Modifier.width(8.dp))
                Text(text = stringResource(R.string.pairing_scan_action))
            }
            if (state.scanning) {
                Spacer(modifier = Modifier.height(12.dp))
                ScanningCard(onCancel = onCancelScan)
            }
            state.failure?.let { failure ->
                Spacer(modifier = Modifier.height(12.dp))
                FailureCard(message = stringResource(failure), onDismiss = onDismissResult)
            }
            state.result?.let { result ->
                Spacer(modifier = Modifier.height(12.dp))
                ResultCard(result = result, onDismiss = onDismissResult)
            }
            Spacer(modifier = Modifier.height(16.dp))
            if (state.window == null) {
                ClosedWindow(
                    role = state.role,
                    busy = state.busy,
                    onRoleChange = onRoleChange,
                    onOpenWindow = onOpenWindow,
                )
            } else {
                OpenWindow(state = state, onExtend = onExtendWindow, onClose = onCloseWindow)
                state.qr?.let { qr ->
                    Spacer(modifier = Modifier.height(16.dp))
                    CodeCard(uri = qr.uri)
                }
                CandidateSection(
                    candidates = state.candidates,
                    dialing = state.dialing,
                    busy = state.busy,
                    windowAddr = state.window.addr,
                    onRefresh = onRefreshCandidates,
                    onPairWith = onPairWith,
                )
            }
            Spacer(modifier = Modifier.height(24.dp))
        }
    }

    state.link?.takeIf { !state.scanning }?.let { link ->
        PairingLinkDialog(link = link, onAccept = onAcceptLink, onDismiss = onDismissLink)
    }

    state.request?.let { request ->
        ConfirmPairingDialog(
            request = request,
            busy = state.busy,
            onConfirm = onConfirm,
            onReject = onReject,
        )
    }
}

/** Role of the pair and the action that opens the window. */
@Composable
private fun ClosedWindow(
    role: Local.Role,
    busy: Boolean,
    onRoleChange: (Local.Role) -> Unit,
    onOpenWindow: () -> Unit,
) {
    SectionTitle(text = stringResource(R.string.pairing_role))
    val roles =
        listOf(
            Local.Role.ROLE_PERSONAL to R.string.pairing_role_personal,
            Local.Role.ROLE_FRIENDS to R.string.pairing_role_friends,
        )
    SingleChoiceSegmentedButtonRow(modifier = Modifier.fillMaxWidth()) {
        roles.forEachIndexed { index, (value, label) ->
            SegmentedButton(
                selected = role == value,
                onClick = { onRoleChange(value) },
                shape = SegmentedButtonDefaults.itemShape(index = index, count = roles.size),
            ) {
                Text(text = stringResource(label))
            }
        }
    }
    Spacer(modifier = Modifier.height(8.dp))
    Text(
        text =
            stringResource(
                if (role == Local.Role.ROLE_FRIENDS) {
                    R.string.pairing_role_friends_hint
                } else {
                    R.string.pairing_role_personal_hint
                },
            ),
        style = MaterialTheme.typography.bodySmall,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
    )
    Spacer(modifier = Modifier.height(16.dp))
    Button(
        onClick = onOpenWindow,
        enabled = !busy,
        modifier =
            Modifier
                .fillMaxWidth()
                .heightIn(min = 56.dp),
    ) {
        Text(text = stringResource(R.string.pairing_open_window))
    }
    Spacer(modifier = Modifier.height(8.dp))
    Text(
        text = stringResource(R.string.pairing_window_hint),
        style = MaterialTheme.typography.bodySmall,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
    )
}

/** Role, remaining time and lifetime actions of the open window. */
@Composable
private fun OpenWindow(
    state: PairingState,
    onExtend: () -> Unit,
    onClose: () -> Unit,
) {
    val window = state.window ?: return
    Surface(
        modifier = Modifier.fillMaxWidth(),
        shape = MaterialTheme.shapes.large,
        color = groupItemColor,
    ) {
        Column(modifier = Modifier.padding(16.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Column(modifier = Modifier.weight(1f)) {
                    Text(
                        text = stringResource(R.string.pairing_window_open),
                        style = MaterialTheme.typography.titleMedium,
                    )
                    roleLabel(state.role)?.let { role ->
                        Text(
                            text = role,
                            style = MaterialTheme.typography.labelMedium,
                            color = MaterialTheme.colorScheme.primary,
                        )
                    }
                }
                Text(
                    text =
                        stringResource(
                            R.string.pairing_window_closes_in,
                            rememberWindowRemaining(window.expiresAtUnixMs),
                        ),
                    style = MaterialTheme.typography.labelLarge,
                )
            }
            Row(
                modifier =
                    Modifier
                        .fillMaxWidth()
                        .padding(top = 8.dp),
                horizontalArrangement = Arrangement.End,
            ) {
                TextButton(onClick = onExtend, enabled = !state.busy) {
                    Text(text = stringResource(R.string.pairing_extend))
                }
                TextButton(onClick = onClose, enabled = !state.busy) {
                    Text(text = stringResource(R.string.pairing_close))
                }
            }
        }
    }
}

/** Code of the window for another device to scan. */
@Composable
private fun CodeCard(uri: String) {
    Surface(
        modifier = Modifier.fillMaxWidth(),
        shape = MaterialTheme.shapes.large,
        color = groupItemColor,
    ) {
        Column(
            modifier = Modifier.padding(16.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            Text(
                text = stringResource(R.string.pairing_code_title),
                style = MaterialTheme.typography.titleMedium,
            )
            Spacer(modifier = Modifier.height(8.dp))
            Text(
                text = stringResource(R.string.pairing_code_body),
                style = MaterialTheme.typography.bodySmall,
                textAlign = TextAlign.Center,
            )
            Spacer(modifier = Modifier.height(16.dp))
            PairingCode(text = uri, modifier = Modifier.size(CODE_SIZE))
        }
    }
}

/** Devices that are open for pairing and compare a code. */
@Composable
private fun CandidateSection(
    candidates: List<Local.PairingCandidate>,
    dialing: String,
    busy: Boolean,
    windowAddr: String,
    onRefresh: () -> Unit,
    onPairWith: (String) -> Unit,
) {
    // The window counts as quiet once it stayed without a candidate for a while.
    val openedAt = remember { SystemClock.elapsedRealtime() }
    var quiet by remember { mutableStateOf(false) }
    LaunchedEffect(candidates.isEmpty()) {
        quiet = false
        if (candidates.isNotEmpty()) {
            return@LaunchedEffect
        }
        delay((QUIET_WINDOW_MS - (SystemClock.elapsedRealtime() - openedAt)).coerceAtLeast(0))
        quiet = true
    }
    Row(
        modifier =
            Modifier
                .fillMaxWidth()
                .padding(top = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        SectionTitle(
            text = stringResource(R.string.pairing_nearby),
            modifier = Modifier.weight(1f),
        )
        TextButton(onClick = onRefresh, enabled = !busy) {
            Text(text = stringResource(R.string.action_refresh))
        }
    }
    if (candidates.isEmpty() && quiet) {
        val seconds = ((SystemClock.elapsedRealtime() - openedAt) / 1000).toInt()
        NetworkIssueCard(
            summary = stringResource(R.string.pairing_quiet),
            hint = stringResource(R.string.pairing_quiet_hint),
            details = stringResource(R.string.pairing_quiet_detail, seconds, windowAddr),
        )
        return
    }
    if (candidates.isEmpty()) {
        Text(
            text = stringResource(R.string.pairing_nearby_empty),
            style = MaterialTheme.typography.bodyMedium,
        )
        Text(
            text = stringResource(R.string.pairing_nearby_hint),
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        return
    }
    ItemGroup {
        candidates.forEachIndexed { index, candidate ->
            CandidateRow(
                index = index,
                count = candidates.size,
                addr = candidate.addr,
                dialing = dialing == candidate.addr,
                enabled = !busy,
                onPair = { onPairWith(candidate.addr) },
            )
        }
    }
}

/** One discovered device with the action that starts the exchange. */
@Composable
private fun CandidateRow(
    index: Int,
    count: Int,
    addr: String,
    dialing: Boolean,
    enabled: Boolean,
    onPair: () -> Unit,
) {
    GroupItem(index = index, count = count) {
        IconTile(icon = R.drawable.ic_nav_devices)
        Text(
            text = addr,
            style = MaterialTheme.typography.bodyLarge,
            modifier = Modifier.weight(1f),
        )
        FilledTonalButton(onClick = onPair, enabled = enabled && !dialing) {
            Text(
                text =
                    stringResource(
                        if (dialing) R.string.pairing_dialing else R.string.pairing_pair,
                    ),
            )
        }
    }
}

/** A scanned code that waits for the other device. */
@Composable
private fun ScanningCard(onCancel: () -> Unit) {
    Surface(
        modifier = Modifier.fillMaxWidth(),
        shape = MaterialTheme.shapes.large,
        color = groupItemColor,
    ) {
        Row(
            modifier = Modifier.padding(16.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            CircularProgressIndicator(modifier = Modifier.size(20.dp), strokeWidth = 2.dp)
            Text(
                text = stringResource(R.string.pairing_scan_waiting),
                style = MaterialTheme.typography.bodyMedium,
                modifier =
                    Modifier
                        .weight(1f)
                        .padding(start = 12.dp),
            )
            TextButton(onClick = onCancel) {
                Text(text = stringResource(R.string.action_cancel))
            }
        }
    }
}

/** Outcome of the last exchange. */
@Composable
private fun ResultCard(
    result: PairingResult,
    onDismiss: () -> Unit,
) {
    if (!result.paired && result.unreachable) {
        NetworkIssueCard(
            summary = stringResource(R.string.pairing_unreachable),
            hint = stringResource(R.string.pairing_unreachable_hint),
            details = result.error,
            onDismiss = onDismiss,
        )
        return
    }
    val message =
        when {
            result.paired && result.name.isNotEmpty() -> stringResource(R.string.pairing_paired, result.name)
            result.paired -> stringResource(R.string.pairing_paired_generic)
            result.rejected -> stringResource(R.string.pairing_rejected)
            result.error.isNotEmpty() -> stringResource(R.string.pairing_failed_with, result.error)
            else -> stringResource(R.string.pairing_failed)
        }
    Card(
        modifier = Modifier.fillMaxWidth(),
        colors =
            CardDefaults.cardColors(
                containerColor =
                    if (result.paired) {
                        MaterialTheme.colorScheme.secondaryContainer
                    } else {
                        MaterialTheme.colorScheme.errorContainer
                    },
            ),
    ) {
        Row(
            modifier = Modifier.padding(16.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                text = message,
                style = MaterialTheme.typography.bodyMedium,
                modifier = Modifier.weight(1f),
            )
            TextButton(onClick = onDismiss) {
                Text(text = stringResource(R.string.action_dismiss))
            }
        }
    }
}

/** Failure of the last call with a way to hide it. */
@Composable
private fun FailureCard(
    message: String,
    onDismiss: () -> Unit,
) {
    Card(
        modifier = Modifier.fillMaxWidth(),
        colors =
            CardDefaults.cardColors(
                containerColor = MaterialTheme.colorScheme.errorContainer,
            ),
    ) {
        Row(
            modifier = Modifier.padding(16.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                text = message,
                style = MaterialTheme.typography.bodyMedium,
                modifier = Modifier.weight(1f),
            )
            TextButton(onClick = onDismiss) {
                Text(text = stringResource(R.string.action_dismiss))
            }
        }
    }
}

/** Asks the user to add the device that requested the pair. */
@Composable
private fun ConfirmPairingDialog(
    request: PairingRequest,
    busy: Boolean,
    onConfirm: () -> Unit,
    onReject: () -> Unit,
) {
    val name = request.name.ifEmpty { stringResource(R.string.pairing_unknown_device) }
    val role = roleLabel(request.role)
    AlertDialog(
        onDismissRequest = onReject,
        title = {
            Text(
                text =
                    if (role == null) {
                        stringResource(R.string.pairing_add_title_plain, name)
                    } else {
                        stringResource(R.string.pairing_add_title, name, role)
                    },
            )
        },
        text = { PairingRequestBody(request) },
        confirmButton = {
            TextButton(onClick = onConfirm, enabled = !busy) {
                Text(text = stringResource(R.string.pairing_confirm))
            }
        },
        dismissButton = {
            TextButton(onClick = onReject, enabled = !busy) {
                Text(text = stringResource(R.string.action_cancel))
            }
        },
    )
}

/**
 * Asks before pairing with the device of a link. Any app or web page can open
 * such a link, so the user decides whether the device joins, and in which role.
 */
@Composable
private fun PairingLinkDialog(
    link: PairingLinkOffer,
    onAccept: () -> Unit,
    onDismiss: () -> Unit,
) {
    AlertDialog(
        onDismissRequest = onDismiss,
        modifier = Modifier.testTag(PAIRING_LINK_TAG),
        title = { Text(text = stringResource(R.string.pairing_link_title)) },
        text = {
            Text(
                text =
                    stringResource(
                        when (link.role) {
                            Local.Role.ROLE_FRIENDS -> R.string.pairing_link_body_friends
                            else -> R.string.pairing_link_body_personal
                        },
                    ),
            )
        },
        confirmButton = {
            TextButton(onClick = onAccept) {
                Text(text = stringResource(R.string.pairing_pair))
            }
        },
        dismissButton = {
            TextButton(onClick = onDismiss) {
                Text(text = stringResource(R.string.action_cancel))
            }
        },
    )
}

/** Test tag of the dialog that asks about a pairing link. */
const val PAIRING_LINK_TAG = "pairing-link"

private val CODE_SIZE = 220.dp
