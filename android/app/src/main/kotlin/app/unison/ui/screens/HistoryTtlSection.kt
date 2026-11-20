package app.unison.ui.screens

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
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
import androidx.compose.ui.unit.dp
import app.unison.R
import app.unison.history.HistoryTtl
import app.unison.history.HistoryTtlText
import app.unison.ui.SectionTitle
import unison.local.v1.Local

/** Lifetimes of the history items of this device. */
@Composable
internal fun HistoryTtlSection(
    ttl: HistoryTtl,
    onSave: (Local.Kind, Long) -> Unit,
    onReset: () -> Unit,
) {
    var invalid by remember { mutableStateOf(false) }

    SectionTitle(text = stringResource(R.string.history_ttl_section))
    Text(
        text = stringResource(R.string.history_ttl_hint),
        style = MaterialTheme.typography.bodySmall,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
    )
    Column(
        modifier = Modifier.padding(top = 8.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Lifetimes(ttl = ttl, onInvalid = { invalid = it }, onSave = onSave)
        if (invalid) {
            Text(
                text = stringResource(R.string.history_ttl_invalid),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.error,
            )
        }
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.SpaceBetween,
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                text = stringResource(R.string.history_ttl_formats),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.weight(1f),
            )
            TextButton(onClick = onReset) {
                Text(text = stringResource(R.string.history_ttl_reset))
            }
        }
    }
}

/** One lifetime field per kind. */
@Composable
private fun Lifetimes(
    ttl: HistoryTtl,
    onInvalid: (Boolean) -> Unit,
    onSave: (Local.Kind, Long) -> Unit,
) {
    val rows =
        listOf(
            Triple(Local.Kind.KIND_TEXT, R.string.history_ttl_text, ttl.text),
            Triple(Local.Kind.KIND_IMAGE, R.string.history_ttl_image, ttl.image),
            Triple(Local.Kind.KIND_FILES, R.string.history_ttl_files, ttl.files),
        )
    for ((kind, label, seconds) in rows) {
        var draft by rememberSaveable(seconds) { mutableStateOf(HistoryTtlText.format(seconds)) }
        val stored = HistoryTtlText.format(seconds)
        Row(
            modifier = Modifier.fillMaxWidth(),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                text = stringResource(label),
                style = MaterialTheme.typography.bodyLarge,
                modifier = Modifier.weight(1f),
            )
            OutlinedTextField(
                value = draft,
                onValueChange = { draft = it },
                modifier =
                    Modifier
                        .width(120.dp)
                        .testTag(ttlTag(kind)),
                singleLine = true,
            )
            TextButton(
                modifier = Modifier.testTag(ttlSaveTag(kind)),
                onClick = {
                    val parsed = HistoryTtlText.parse(draft)
                    onInvalid(parsed == null)
                    if (parsed != null) {
                        onSave(kind, parsed)
                    }
                },
                enabled = draft.trim() != stored,
            ) {
                Text(text = stringResource(R.string.action_save))
            }
        }
    }
}

/** Tag of the lifetime field of a kind. */
internal fun ttlTag(kind: Local.Kind): String =
    when (kind) {
        Local.Kind.KIND_IMAGE -> "settings.ttl.image"
        Local.Kind.KIND_FILES -> "settings.ttl.files"
        else -> "settings.ttl.text"
    }

/** Tag of the save button of a lifetime. */
internal fun ttlSaveTag(kind: Local.Kind): String = ttlTag(kind) + ".save"
