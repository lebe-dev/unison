package app.unison.ui.screens

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import app.unison.R
import app.unison.history.HistoryText
import app.unison.ui.sizeUnits
import unison.local.v1.Local

/** Preview of one dashboard item with its actions. */
@Composable
internal fun DashboardPreviewDialog(
    entry: Local.HistoryItem,
    busy: Boolean,
    held: Boolean,
    onClose: () -> Unit,
    onCopy: () -> Unit,
    onDownload: () -> Unit,
    onApply: () -> Unit,
    onDelete: () -> Unit,
) {
    val item = entry.item
    AlertDialog(
        onDismissRequest = onClose,
        title = { Text(text = previewTitle(item)) },
        text = {
            Column(modifier = Modifier.testTag(PREVIEW_TAG)) {
                PreviewBody(entry = entry)
                Row(
                    modifier =
                        Modifier
                            .fillMaxWidth()
                            .padding(top = 12.dp),
                    horizontalArrangement = Arrangement.End,
                ) {
                    TextButton(
                        onClick = onCopy,
                        enabled = !busy,
                        modifier = Modifier.testTag(PREVIEW_COPY_TAG),
                    ) {
                        Text(text = stringResource(R.string.action_copy))
                    }
                    if (item.kind == Local.Kind.KIND_FILES) {
                        TextButton(
                            onClick = onDownload,
                            enabled = !busy,
                            modifier = Modifier.testTag(PREVIEW_DOWNLOAD_TAG),
                        ) {
                            Text(text = stringResource(R.string.action_download))
                        }
                    }
                    if (held) {
                        TextButton(
                            onClick = onApply,
                            enabled = !busy,
                            modifier = Modifier.testTag(PREVIEW_APPLY_TAG),
                        ) {
                            Text(text = stringResource(R.string.action_apply))
                        }
                    }
                }
            }
        },
        confirmButton = {
            TextButton(onClick = onClose) {
                Text(text = stringResource(R.string.action_close))
            }
        },
        dismissButton = {
            TextButton(onClick = onDelete, enabled = !busy) {
                Text(
                    text = stringResource(R.string.action_delete),
                    color = MaterialTheme.colorScheme.error,
                )
            }
        },
    )
}

/** Content of the preview of a file or a text entry. */
@Composable
private fun PreviewBody(entry: Local.HistoryItem) {
    val item = entry.item
    if (item.kind == Local.Kind.KIND_FILES) {
        PreviewFiles(entry = entry)
    } else {
        SelectionContainer {
            Text(
                text = HistoryText.fullText(item).ifEmpty { stringResource(R.string.kind_text) },
                style = MaterialTheme.typography.bodyMedium,
                modifier =
                    Modifier
                        .heightIn(max = PREVIEW_TEXT_HEIGHT)
                        .verticalScroll(rememberScrollState()),
            )
        }
    }
}

/** Names and sizes of the files of the previewed entry. */
@Composable
private fun PreviewFiles(entry: Local.HistoryItem) {
    val units = sizeUnits()
    Column {
        entry.item.representationsList.forEach { representation ->
            FileRepresentationRow(
                representation = representation,
                units = units,
                modifier = Modifier.padding(vertical = 2.dp),
            )
        }
        if (!entry.complete) {
            Text(
                text = stringResource(R.string.dashboard_not_downloaded),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(top = 4.dp),
            )
        }
    }
}

/** Name of an item in the title of the preview. */
@Composable
private fun previewTitle(item: Local.ClipItem): String =
    when (item.kind) {
        Local.Kind.KIND_TEXT ->
            HistoryText.text(item, PREVIEW_TITLE_LENGTH).ifEmpty { stringResource(R.string.kind_text) }
        Local.Kind.KIND_FILES ->
            HistoryText.fileNames(item).shown.ifEmpty { stringResource(R.string.kind_files) }
        else -> stringResource(R.string.kind_image)
    }

/** Tag of the body of the preview. */
internal const val PREVIEW_TAG = "dashboard.preview"

/** Tag of the copy action of the preview. */
internal const val PREVIEW_COPY_TAG = "dashboard.preview.copy"

/** Tag of the download action of the preview. */
internal const val PREVIEW_DOWNLOAD_TAG = "dashboard.preview.download"

/** Tag of the apply action of the preview. */
internal const val PREVIEW_APPLY_TAG = "dashboard.preview.apply"

/** Height above which the previewed text scrolls. */
private val PREVIEW_TEXT_HEIGHT = 320.dp

/** Length of the text in the title of the preview. */
private const val PREVIEW_TITLE_LENGTH = 60
