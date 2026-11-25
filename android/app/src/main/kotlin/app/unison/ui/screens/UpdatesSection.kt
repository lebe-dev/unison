package app.unison.ui.screens

import android.text.format.DateUtils
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.selection.selectableGroup
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.unit.dp
import app.unison.BuildConfig
import app.unison.R
import app.unison.ui.SectionTitle
import app.unison.ui.SettingsCard
import app.unison.ui.UpdatePanel
import app.unison.updater.UpdateCheckInterval
import app.unison.updater.UpdateStatus
import app.unison.updater.UpdateUiState

/** The Updates page of the settings: the schedule and the last result. */
@Composable
internal fun UpdatesSection(panel: UpdatePanel) {
    val state = panel.state
    SettingsCard {
        SectionTitle(text = stringResource(R.string.settings_section_updates))
        SettingsSwitch(
            title = stringResource(R.string.updates_auto),
            hint = stringResource(R.string.updates_auto_hint),
            checked = state.config.enabled,
            tag = UPDATES_AUTO_TAG,
            onChange = panel.onSetEnabled,
        )
        UpdateIntervals(
            selected = state.config.interval,
            enabled = true,
            onSelect = panel.onSetInterval,
        )
        SettingsSwitch(
            title = stringResource(R.string.updates_beta),
            hint = stringResource(R.string.updates_beta_hint),
            checked = state.config.betaChannel,
            tag = UPDATES_BETA_TAG,
            onChange = panel.onSetBetaChannel,
        )
    }
    SettingsCard {
        SectionTitle(text = stringResource(R.string.settings_section_update_status))
        UpdateStatusBlock(panel = panel)
    }
}

/** The three intervals of the background check. */
@Composable
private fun UpdateIntervals(
    selected: UpdateCheckInterval,
    enabled: Boolean,
    onSelect: (UpdateCheckInterval) -> Unit,
) {
    Text(
        text = stringResource(R.string.updates_interval),
        style = MaterialTheme.typography.bodyLarge,
        modifier = Modifier.padding(top = 8.dp),
    )
    Column(modifier = Modifier.selectableGroup()) {
        UpdateCheckInterval.entries.forEach { interval ->
            Row(
                modifier =
                    Modifier
                        .fillMaxWidth()
                        .selectable(
                            selected = interval == selected,
                            enabled = enabled,
                            onClick = { onSelect(interval) },
                            role = Role.RadioButton,
                        ).testTag(updatesIntervalTag(interval))
                        .padding(vertical = 8.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                RadioButton(selected = interval == selected, onClick = null, enabled = enabled)
                Text(
                    text = intervalLabel(interval),
                    style = MaterialTheme.typography.bodyLarge,
                    modifier = Modifier.padding(start = 8.dp),
                )
            }
        }
    }
}

/** The installed version, the last check and the current result. */
@Composable
private fun UpdateStatusBlock(panel: UpdatePanel) {
    val state = panel.state
    val available = state.available
    Text(
        text = stringResource(R.string.updates_version, BuildConfig.VERSION_NAME),
        style = MaterialTheme.typography.bodyLarge,
    )
    Text(
        text = lastCheckedLabel(state),
        style = MaterialTheme.typography.bodySmall,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        modifier = Modifier.padding(top = 2.dp),
    )
    Text(
        text = resultLabel(state),
        style = MaterialTheme.typography.bodyMedium,
        color = if (state.status is UpdateStatus.Failed) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.onSurface,
        modifier = Modifier.padding(top = 8.dp),
    )
    Row(
        modifier =
            Modifier
                .fillMaxWidth()
                .padding(top = 8.dp),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        OutlinedButton(
            onClick = panel.onCheckNow,
            enabled = state.status != UpdateStatus.Checking,
            modifier = Modifier.testTag(UPDATES_CHECK_TAG),
        ) {
            if (state.status == UpdateStatus.Checking) {
                CircularProgressIndicator(modifier = Modifier.size(16.dp), strokeWidth = 2.dp)
            } else {
                Text(text = stringResource(R.string.updates_check_now))
            }
        }
        if (available != null) {
            FilledTonalButton(
                onClick = panel.onOpenDialog,
                modifier = Modifier.testTag(UPDATES_VIEW_TAG),
            ) {
                Text(text = stringResource(R.string.updates_view))
            }
        }
    }
}

/** The line of the last successful check. */
@Composable
private fun lastCheckedLabel(state: UpdateUiState): String {
    if (state.config.lastCheckedMs <= 0) {
        return stringResource(R.string.updates_never_checked)
    }
    val time =
        DateUtils
            .getRelativeTimeSpanString(
                state.config.lastCheckedMs,
                System.currentTimeMillis(),
                DateUtils.MINUTE_IN_MILLIS,
            ).toString()
    return stringResource(R.string.updates_last_checked, time)
}

/** The result of the last check, or the state of the running one. */
@Composable
private fun resultLabel(state: UpdateUiState): String {
    val status = state.status
    return when {
        status == UpdateStatus.Checking -> stringResource(R.string.updates_checking)
        status is UpdateStatus.Failed -> stringResource(status.message)
        state.available != null -> stringResource(R.string.updates_available, state.available.version)
        state.config.lastCheckedMs > 0 -> stringResource(R.string.updates_up_to_date)
        else -> stringResource(R.string.updates_page_hint)
    }
}

/** Label of one interval. */
@Composable
internal fun intervalLabel(interval: UpdateCheckInterval): String =
    stringResource(
        when (interval) {
            UpdateCheckInterval.SIX_HOURS -> R.string.updates_interval_six_hours
            UpdateCheckInterval.DAILY -> R.string.updates_interval_daily
            UpdateCheckInterval.WEEKLY -> R.string.updates_interval_weekly
        },
    )

/** Summary of the Updates row of the settings index. */
@Composable
internal fun updatesSummary(state: UpdateUiState): String {
    state.available?.let { release ->
        return stringResource(R.string.updates_available, release.version)
    }
    if (!state.config.enabled) {
        return stringResource(R.string.updates_off)
    }
    return intervalLabel(state.config.interval)
}

/** Tag of the switch of the automatic checks. */
internal const val UPDATES_AUTO_TAG = "settings.updates.auto"

/** Tag of the switch of the beta channel. */
internal const val UPDATES_BETA_TAG = "settings.updates.beta"

/** Tag of the manual check button. */
internal const val UPDATES_CHECK_TAG = "settings.updates.check"

/** Tag of the button that opens the dialog. */
internal const val UPDATES_VIEW_TAG = "settings.updates.view"

/** Tag of the interval option. */
internal fun updatesIntervalTag(interval: UpdateCheckInterval): String = "settings.updates.interval.${interval.value}"
