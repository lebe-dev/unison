package app.unison.ui

import androidx.compose.foundation.layout.Column
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.foundation.layout.Row
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.platform.LocalResources
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import app.unison.R
import app.unison.history.HistoryText
import app.unison.ui.state.PendingPrompt
import unison.local.v1.Local

/** Answer to a question of [PromptDialog]. */
enum class PromptAnswer {
    /** Writes a held clipboard item to the clipboard. */
    APPLY,

    /** Lets a held clipboard item go; it stays in the history. */
    SKIP,

    /** Accepts a file offer. */
    ACCEPT,

    /** Declines a file offer. */
    DECLINE,
}

/**
 * Asks about the oldest question that waits: a held clipboard item or a file
 * offer. Later puts the question away until the app is opened again; it stays
 * on the History or the Requests screen and in the notification.
 */
@Composable
fun PromptDialog(prompts: List<PendingPrompt>, onAnswer: (PendingPrompt, PromptAnswer) -> Unit) {
    var later by rememberSaveable { mutableStateOf(listOf<String>()) }
    val waiting = prompts.filter { it.id !in later }
    val prompt = waiting.firstOrNull() ?: return
    val device = prompt.device.ifBlank { stringResource(R.string.prompt_unknown_device) }
    val dismiss = { later = later + prompt.id }

    AlertDialog(
        onDismissRequest = dismiss,
        modifier = Modifier.testTag(PROMPT_TAG),
        title = {
            Text(
                text = stringResource(
                    if (prompt.kind == PendingPrompt.Kind.FILES) R.string.prompt_files_title else R.string.prompt_clipboard_title,
                    device,
                ),
            )
        },
        text = {
            Column {
                Text(text = question(prompt), style = MaterialTheme.typography.bodyLarge)
                Text(
                    text = HistoryText.formatSize(prompt.sizeBytes, sizeUnits()),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
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
            val answer = if (prompt.kind == PendingPrompt.Kind.FILES) PromptAnswer.ACCEPT else PromptAnswer.APPLY
            TextButton(onClick = { onAnswer(prompt, answer) }) {
                Text(
                    text = stringResource(
                        if (answer == PromptAnswer.ACCEPT) R.string.action_accept else R.string.action_apply,
                    ),
                )
            }
        },
        dismissButton = {
            Row {
                TextButton(onClick = dismiss) {
                    Text(text = stringResource(R.string.action_later))
                }
                val answer = if (prompt.kind == PendingPrompt.Kind.FILES) PromptAnswer.DECLINE else PromptAnswer.SKIP
                TextButton(onClick = { onAnswer(prompt, answer) }) {
                    Text(
                        text = stringResource(
                            if (answer == PromptAnswer.DECLINE) R.string.action_decline else R.string.action_skip,
                        ),
                    )
                }
            }
        },
    )
}

/** The question of a prompt in plain English. */
@Composable
private fun question(prompt: PendingPrompt): String {
    if (prompt.kind == PendingPrompt.Kind.CLIPBOARD) {
        val kind = LocalResources.current.getString(kindLabel(prompt.clipKind)).lowercase()
        return stringResource(R.string.prompt_clipboard_question, kind)
    }
    val files = if (prompt.files.size == 1) {
        prompt.files.single()
    } else {
        pluralStringResource(R.plurals.prompt_files_count, prompt.files.size, prompt.files.size)
    }
    return stringResource(R.string.prompt_files_question, files)
}

/** Name of a clipboard item kind. */
private fun kindLabel(kind: Local.Kind): Int = when (kind) {
    Local.Kind.KIND_IMAGE -> R.string.kind_image
    Local.Kind.KIND_FILES -> R.string.kind_files
    else -> R.string.kind_text
}

/** Tag of the question dialog. */
internal const val PROMPT_TAG = "prompt.dialog"
