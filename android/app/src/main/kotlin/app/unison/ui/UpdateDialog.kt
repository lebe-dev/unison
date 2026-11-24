package app.unison.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import app.unison.R
import app.unison.history.HistoryText
import app.unison.updater.ReleaseInfo
import app.unison.updater.UpdateMarkdownText
import app.unison.updater.UpdateStatus
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.time.format.FormatStyle

/** The dialog with the changelog, the progress and the errors of an update. */
@Composable
fun UpdateDialog(panel: UpdatePanel) {
    val state = panel.state
    if (!state.dialogOpen) {
        return
    }
    val release = state.available
    val status = state.status
    val installing = status == UpdateStatus.Verifying || status == UpdateStatus.Installing
    AlertDialog(
        onDismissRequest = { if (!installing) panel.onCloseDialog() },
        modifier = Modifier.testTag(UPDATE_DIALOG_TAG),
        title = {
            Column {
                Text(text = stringResource(R.string.updates_dialog_title))
                if (release != null) {
                    Text(
                        text = stringResource(R.string.updates_version_change, state.currentVersion, release.version),
                        style = MaterialTheme.typography.bodyMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
        },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                if (release != null) {
                    ReleaseFacts(release = release)
                    ReleaseNotes(body = release.body)
                }
                if (status == UpdateStatus.PermissionRequired) {
                    PermissionCard(onOpen = panel.onRequestInstallPermission)
                }
                ProgressBlock(status = status)
                if (status is UpdateStatus.Failed) {
                    FailureCard(
                        message = stringResource(status.message),
                        onRetry = if (release != null) panel.onInstall else null,
                    )
                }
            }
        },
        confirmButton = { DialogConfirmButton(panel = panel, status = status, release = release) },
        dismissButton = { DialogDismissButton(panel = panel, status = status) },
    )
}

/** The primary button, which follows the step of the update. */
@Composable
private fun DialogConfirmButton(
    panel: UpdatePanel,
    status: UpdateStatus,
    release: ReleaseInfo?,
) {
    when (status) {
        is UpdateStatus.Downloading ->
            TextButton(
                onClick = panel.onCancel,
                modifier = Modifier.testTag(UPDATE_DIALOG_CANCEL_TAG),
            ) {
                Text(text = stringResource(R.string.action_cancel))
            }
        UpdateStatus.Verifying, UpdateStatus.Installing ->
            TextButton(onClick = {}, enabled = false) {
                Text(text = stringResource(R.string.updates_installing))
            }
        UpdateStatus.PermissionRequired ->
            FilledTonalButton(
                onClick = panel.onRequestInstallPermission,
                modifier = Modifier.testTag(UPDATE_DIALOG_PERMISSION_TAG),
            ) {
                Text(text = stringResource(R.string.updates_permission_open))
            }
        UpdateStatus.Checking ->
            TextButton(onClick = {}, enabled = false) {
                Text(text = stringResource(R.string.updates_checking))
            }
        else ->
            FilledTonalButton(
                onClick = panel.onInstall,
                enabled = release != null,
                modifier = Modifier.testTag(UPDATE_DIALOG_INSTALL_TAG),
            ) {
                Text(text = stringResource(R.string.updates_install))
            }
    }
}

/** The button that closes the dialog, hidden while the APK is already handed over. */
@Composable
private fun DialogDismissButton(
    panel: UpdatePanel,
    status: UpdateStatus,
) {
    if (status == UpdateStatus.Verifying || status == UpdateStatus.Installing) {
        return
    }
    TextButton(
        onClick = panel.onCloseDialog,
        modifier = Modifier.testTag(UPDATE_DIALOG_LATER_TAG),
    ) {
        Text(text = stringResource(R.string.action_later))
    }
}

/** Version badge, publish date and size of the release. */
@Composable
private fun ReleaseFacts(release: ReleaseInfo) {
    val units = sizeUnits()
    Row(
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Text(
            text = stringResource(R.string.updates_published, publishDate(release.publishedAt)),
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        if (release.assetSize > 0) {
            Text(
                text = HistoryText.formatSize(release.assetSize, units),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        if (release.prerelease) {
            BetaBadge()
        }
    }
}

/** The changelog in a scrollable frame. */
@Composable
private fun ReleaseNotes(body: String) {
    val scroll = rememberScrollState()
    Column(
        modifier =
            Modifier
                .fillMaxWidth()
                .heightIn(max = NOTES_HEIGHT)
                .verticalScroll(scroll)
                .testTag(UPDATE_DIALOG_NOTES_TAG),
    ) {
        if (body.isBlank()) {
            Text(
                text = stringResource(R.string.updates_no_notes),
                style = MaterialTheme.typography.bodyMedium,
            )
        } else {
            UpdateMarkdownText(markdown = body)
        }
    }
}

/** Explanation of the permission Android needs before it installs an APK. */
@Composable
private fun PermissionCard(onOpen: () -> Unit) {
    Card(
        modifier = Modifier.fillMaxWidth(),
        colors =
            CardDefaults.cardColors(
                containerColor = MaterialTheme.colorScheme.tertiaryContainer,
                contentColor = MaterialTheme.colorScheme.onTertiaryContainer,
            ),
    ) {
        Column(modifier = Modifier.padding(16.dp)) {
            Text(
                text = stringResource(R.string.updates_permission_title),
                style = MaterialTheme.typography.titleSmall,
            )
            Text(
                text = stringResource(R.string.updates_permission_body),
                style = MaterialTheme.typography.bodyMedium,
                modifier = Modifier.padding(top = 4.dp),
            )
            TextButton(
                onClick = onOpen,
                modifier = Modifier.padding(top = 4.dp),
            ) {
                Text(text = stringResource(R.string.updates_permission_open))
            }
        }
    }
}

/** Progress of a download or of the hand-off to Android. */
@Composable
private fun ProgressBlock(status: UpdateStatus) {
    val units = sizeUnits()
    when (status) {
        is UpdateStatus.Downloading -> {
            LinearProgressIndicator(
                progress = { updateProgress(status) },
                modifier = Modifier.fillMaxWidth(),
            )
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
            ) {
                Text(
                    text =
                        stringResource(
                            R.string.updates_download_progress,
                            HistoryText.formatSize(status.downloaded, units),
                            HistoryText.formatSize(status.total, units),
                        ),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                Text(
                    text =
                        stringResource(
                            R.string.updates_percent,
                            ((status.downloaded * PERCENT) / status.total.coerceAtLeast(1)).toInt(),
                        ),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
        UpdateStatus.Verifying, UpdateStatus.Installing -> {
            LinearProgressIndicator(modifier = Modifier.fillMaxWidth())
            Text(
                text = stringResource(if (status == UpdateStatus.Verifying) R.string.updates_verifying else R.string.updates_installing),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        else -> Unit
    }
}

/** A failure with a way to repeat it. */
@Composable
private fun FailureCard(
    message: String,
    onRetry: (() -> Unit)?,
) {
    Card(
        modifier = Modifier.fillMaxWidth(),
        colors =
            CardDefaults.cardColors(
                containerColor = MaterialTheme.colorScheme.errorContainer,
                contentColor = MaterialTheme.colorScheme.onErrorContainer,
            ),
    ) {
        Column(modifier = Modifier.padding(16.dp)) {
            Text(text = message, style = MaterialTheme.typography.bodyMedium)
            if (onRetry != null) {
                TextButton(
                    onClick = onRetry,
                    modifier =
                        Modifier
                            .padding(top = 4.dp)
                            .testTag(UPDATE_DIALOG_RETRY_TAG),
                ) {
                    Text(text = stringResource(R.string.action_retry))
                }
            }
        }
    }
}

/** Publish date of the release in the locale of the device. */
@Composable
private fun publishDate(publishedAt: String): String {
    if (publishedAt.isBlank()) {
        return stringResource(R.string.updates_unknown_date)
    }
    val locale = LocalConfiguration.current.locales[0]
    val date =
        runCatching {
            DateTimeFormatter
                .ofLocalizedDate(FormatStyle.MEDIUM)
                .withLocale(locale)
                .withZone(ZoneId.systemDefault())
                .format(Instant.parse(publishedAt))
        }.getOrNull()
    return date ?: publishedAt
}

/** Tag of the dialog. */
internal const val UPDATE_DIALOG_TAG = "updates.dialog"

/** Tag of the changelog of the dialog. */
internal const val UPDATE_DIALOG_NOTES_TAG = "updates.dialog.notes"

/** Tag of the primary button of the dialog. */
internal const val UPDATE_DIALOG_INSTALL_TAG = "updates.dialog.install"

/** Tag of the button that stops a download. */
internal const val UPDATE_DIALOG_CANCEL_TAG = "updates.dialog.cancel"

/** Tag of the button that repeats a failed step. */
internal const val UPDATE_DIALOG_RETRY_TAG = "updates.dialog.retry"

/** Tag of the button that opens the permission settings. */
internal const val UPDATE_DIALOG_PERMISSION_TAG = "updates.dialog.permission"

/** Tag of the button that closes the dialog. */
internal const val UPDATE_DIALOG_LATER_TAG = "updates.dialog.later"

/** Height above which the changelog scrolls. */
private val NOTES_HEIGHT = 320.dp

/** Percent of a fraction. */
private const val PERCENT = 100L
