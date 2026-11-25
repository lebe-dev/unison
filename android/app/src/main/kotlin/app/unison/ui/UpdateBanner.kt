package app.unison.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import app.unison.R
import app.unison.updater.UpdateStatus

/** Banner at the top of the dashboard while an update waits or installs. */
@Composable
fun UpdateBanner(
    panel: UpdatePanel,
    modifier: Modifier = Modifier,
) {
    val state = panel.state
    if (!state.showBanner) {
        return
    }
    val release = state.available ?: return
    Card(
        modifier =
            modifier
                .fillMaxWidth()
                .testTag(UPDATE_BANNER_TAG),
        colors =
            CardDefaults.cardColors(
                containerColor = MaterialTheme.colorScheme.secondaryContainer,
                contentColor = MaterialTheme.colorScheme.onSecondaryContainer,
            ),
    ) {
        Column(modifier = Modifier.padding(16.dp)) {
            Row(
                horizontalArrangement = Arrangement.spacedBy(12.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                IconTile(
                    icon = R.drawable.ic_update,
                    container = MaterialTheme.colorScheme.primary,
                    content = MaterialTheme.colorScheme.onPrimary,
                    size = 40.dp,
                )
                Column(modifier = Modifier.weight(1f)) {
                    Text(
                        text = stringResource(R.string.updates_banner_title, release.version),
                        style = MaterialTheme.typography.titleMedium,
                    )
                    if (release.prerelease) {
                        BetaBadge()
                    }
                    updateProgressText(state.status)?.let { text ->
                        Text(
                            text = text,
                            style = MaterialTheme.typography.bodySmall,
                            modifier = Modifier.padding(top = 2.dp),
                        )
                    }
                }
            }
            when (state.status) {
                is UpdateStatus.Downloading ->
                    LinearProgressIndicator(
                        progress = { updateProgress(state.status) },
                        modifier =
                            Modifier
                                .fillMaxWidth()
                                .padding(top = 12.dp),
                    )
                UpdateStatus.Verifying, UpdateStatus.Installing ->
                    LinearProgressIndicator(
                        modifier =
                            Modifier
                                .fillMaxWidth()
                                .padding(top = 12.dp),
                    )
                else -> Unit
            }
            Row(
                modifier =
                    Modifier
                        .fillMaxWidth()
                        .padding(top = 4.dp),
                horizontalArrangement = Arrangement.End,
                verticalAlignment = Alignment.CenterVertically,
            ) {
                if (!state.busy) {
                    TextButton(
                        onClick = panel.onDismissBanner,
                        modifier = Modifier.testTag(UPDATE_BANNER_LATER_TAG),
                    ) {
                        Text(text = stringResource(R.string.action_later))
                    }
                }
                FilledTonalButton(
                    onClick = panel.onOpenDialog,
                    modifier = Modifier.testTag(UPDATE_BANNER_UPDATE_TAG),
                ) {
                    Text(text = stringResource(R.string.updates_update))
                }
            }
        }
    }
}

/** Small label that marks a pre-release of GitHub. */
@Composable
fun BetaBadge(modifier: Modifier = Modifier) {
    Surface(
        modifier = modifier,
        shape = RoundedCornerShape(6.dp),
        color = MaterialTheme.colorScheme.tertiaryContainer,
        contentColor = MaterialTheme.colorScheme.onTertiaryContainer,
    ) {
        Text(
            text = stringResource(R.string.updates_beta_badge),
            style = MaterialTheme.typography.labelSmall,
            modifier = Modifier.padding(horizontal = 6.dp, vertical = 2.dp),
        )
    }
}

/** Fraction of a download that is done, from 0 to 1. */
internal fun updateProgress(status: UpdateStatus.Downloading): Float =
    if (status.total > 0) {
        (status.downloaded.toFloat() / status.total.toFloat()).coerceIn(0f, 1f)
    } else {
        0f
    }

/** Whole percentage of a download that is done, from 0 to 100. */
internal fun updatePercent(status: UpdateStatus.Downloading): Int = (updateProgress(status) * PERCENT).toInt()

/** Line under the title of the banner during a download or an installation. */
@Composable
internal fun updateProgressText(status: UpdateStatus): String? =
    when (status) {
        is UpdateStatus.Downloading -> stringResource(R.string.updates_downloading, updatePercent(status))
        UpdateStatus.Verifying -> stringResource(R.string.updates_verifying)
        UpdateStatus.Installing -> stringResource(R.string.updates_installing)
        else -> null
    }

/** Tag of the banner. */
internal const val UPDATE_BANNER_TAG = "updates.banner"

/** Tag of the button of the banner that opens the dialog. */
internal const val UPDATE_BANNER_UPDATE_TAG = "updates.banner.update"

/** Tag of the button that puts the banner away. */
internal const val UPDATE_BANNER_LATER_TAG = "updates.banner.later"

/** Percent of a fraction. */
private const val PERCENT = 100L
