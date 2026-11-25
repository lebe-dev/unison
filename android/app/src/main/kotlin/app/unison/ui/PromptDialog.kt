package app.unison.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalResources
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import app.unison.R
import app.unison.history.HistoryText
import app.unison.ui.state.PendingPrompt
import unison.local.v1.Local

/**
 * Asks about the oldest question that waits: a held clipboard item, a file
 * offer or an introduction. Later puts the question away until the app is
 * opened again; it stays on the History or the Requests screen and in the
 * notification.
 */
@Composable
fun PromptDialog(
    prompts: List<PendingPrompt>,
    onAnswer: (PendingPrompt, PromptAnswer) -> Unit,
) {
    var later by rememberSaveable { mutableStateOf(listOf<String>()) }
    val waiting = prompts.filter { it.id !in later }
    val prompt = waiting.firstOrNull() ?: return
    val pair = prompt.mediated
    val device = prompt.device.ifBlank { stringResource(R.string.prompt_unknown_device) }
    val dismiss = { later = later + prompt.id }
    var discovery by rememberSaveable(prompt.id) {
        mutableStateOf(pair?.role == Local.Role.ROLE_FRIENDS)
    }

    AlertDialog(
        onDismissRequest = dismiss,
        modifier = Modifier.testTag(PROMPT_TAG),
        title = {
            Text(
                text =
                    if (pair != null) {
                        stringResource(
                            R.string.mediated_offer_title,
                            pair.peer.name.ifBlank { shortId(pair.peer.deviceId) },
                        )
                    } else {
                        stringResource(
                            if (prompt.kind == PendingPrompt.Kind.FILES) {
                                R.string.prompt_files_title
                            } else {
                                R.string.prompt_clipboard_title
                            },
                            device,
                        )
                    },
            )
        },
        text = {
            Column {
                if (pair != null) {
                    MediatedOfferBody(
                        pair = pair,
                        discovery = discovery,
                        onDiscoveryChange = { discovery = it },
                    )
                } else {
                    Text(text = question(prompt), style = MaterialTheme.typography.bodyLarge)
                    Text(
                        text = HistoryText.formatSize(prompt.sizeBytes, sizeUnits()),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                if (waiting.size > 1) {
                    Text(
                        text = stringResource(R.string.prompt_position, waiting.size),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
        },
        confirmButton = {
            if (pair != null) {
                TextButton(onClick = { onAnswer(prompt, PromptAnswer.AcceptMediated(discovery)) }) {
                    Text(text = stringResource(R.string.action_accept))
                }
            } else {
                val answer = if (prompt.kind == PendingPrompt.Kind.FILES) PromptAnswer.Accept else PromptAnswer.Apply
                TextButton(onClick = { onAnswer(prompt, answer) }) {
                    Text(
                        text =
                            stringResource(
                                if (answer == PromptAnswer.Accept) R.string.action_accept else R.string.action_apply,
                            ),
                    )
                }
            }
        },
        dismissButton = {
            Row {
                TextButton(onClick = dismiss) {
                    Text(text = stringResource(R.string.action_later))
                }
                if (pair != null) {
                    TextButton(onClick = { onAnswer(prompt, PromptAnswer.DeclineMediated) }) {
                        Text(text = stringResource(R.string.action_decline))
                    }
                } else {
                    val answer = if (prompt.kind == PendingPrompt.Kind.FILES) PromptAnswer.Decline else PromptAnswer.Skip
                    TextButton(onClick = { onAnswer(prompt, answer) }) {
                        Text(
                            text =
                                stringResource(
                                    if (answer == PromptAnswer.Decline) R.string.action_decline else R.string.action_skip,
                                ),
                        )
                    }
                }
            }
        },
    )
}

/** Details of an offer to introduce this device to another one. */
@Composable
private fun MediatedOfferBody(
    pair: Local.MediatedPair,
    discovery: Boolean,
    onDiscoveryChange: (Boolean) -> Unit,
) {
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Text(
            text =
                stringResource(
                    R.string.mediated_offer_from,
                    pair.mediator.name.ifBlank { shortId(pair.mediator.deviceId) },
                ),
            style = MaterialTheme.typography.bodyLarge,
        )
        MediatedOfferDetails(pair = pair, showSasHint = true)
        if (pair.role == Local.Role.ROLE_FRIENDS) {
            FriendsDiscoveryChoice(checked = discovery, onChange = onDiscoveryChange)
        }
    }
}

/** The question of a prompt in plain English. */
@Composable
private fun question(prompt: PendingPrompt): String {
    if (prompt.kind == PendingPrompt.Kind.CLIPBOARD) {
        val kind = LocalResources.current.getString(kindLabel(prompt.clipKind)).lowercase()
        return stringResource(R.string.prompt_clipboard_question, kind)
    }
    val files =
        if (prompt.files.size == 1) {
            prompt.files.single()
        } else {
            pluralStringResource(R.plurals.prompt_files_count, prompt.files.size, prompt.files.size)
        }
    return stringResource(R.string.prompt_files_question, files)
}

/** Name of a clipboard item kind. */
private fun kindLabel(kind: Local.Kind): Int =
    when (kind) {
        Local.Kind.KIND_IMAGE -> R.string.kind_image
        Local.Kind.KIND_FILES -> R.string.kind_files
        else -> R.string.kind_text
    }

/** Tag of the question dialog. */
internal const val PROMPT_TAG = "prompt.dialog"
