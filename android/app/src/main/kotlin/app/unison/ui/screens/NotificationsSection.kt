package app.unison.ui.screens

import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalLifecycleOwner
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import app.unison.R
import app.unison.service.NotificationChannels
import app.unison.service.NotificationSettings
import app.unison.service.NotificationSystem
import app.unison.ui.SectionTitle

/** Categories of the notifications the app posts. */
@Composable
internal fun NotificationCategories(
    settings: NotificationSettings,
    onChange: (NotificationSettings) -> Unit,
) {
    SectionTitle(text = stringResource(R.string.settings_section_notifications))
    SettingsSwitch(
        title = stringResource(R.string.settings_notification_transfers),
        hint = stringResource(R.string.settings_notification_transfers_hint),
        checked = settings.transfers,
        tag = TRANSFERS_TAG,
        onChange = { onChange(settings.copy(transfers = it)) },
    )
    SettingsSwitch(
        title = stringResource(R.string.settings_notification_requests),
        hint = stringResource(R.string.settings_notification_requests_hint),
        checked = settings.requests,
        tag = REQUESTS_TAG,
        onChange = { onChange(settings.copy(requests = it)) },
    )
    SettingsSwitch(
        title = stringResource(R.string.settings_notification_clipboard),
        hint = stringResource(R.string.settings_notification_clipboard_hint),
        checked = settings.clipboard,
        tag = CLIPBOARD_TAG,
        onChange = { onChange(settings.copy(clipboard = it)) },
    )
    SettingsSwitch(
        title = stringResource(R.string.settings_notification_confirmations),
        hint = stringResource(R.string.settings_notification_confirmations_hint),
        checked = settings.confirmations,
        tag = CONFIRMATIONS_TAG,
        onChange = { onChange(settings.copy(confirmations = it)) },
    )
}

/** Behavior of the persistent notification of the service. */
@Composable
internal fun PersistentStatus(
    settings: NotificationSettings,
    onChange: (NotificationSettings) -> Unit,
) {
    SectionTitle(text = stringResource(R.string.settings_section_status))
    SettingsSwitch(
        title = stringResource(R.string.settings_notification_hide_status),
        hint = stringResource(R.string.settings_notification_hide_status_hint),
        checked = settings.hideStatusAfterDismissal,
        tag = HIDE_STATUS_TAG,
        onChange = { onChange(settings.copy(hideStatusAfterDismissal = it)) },
    )
    SettingsSwitch(
        title = stringResource(R.string.settings_notification_status_details),
        hint = stringResource(R.string.settings_notification_status_details_hint),
        checked = settings.statusDetails,
        tag = STATUS_DETAILS_TAG,
        onChange = { onChange(settings.copy(statusDetails = it)) },
    )
    Text(
        text = stringResource(R.string.settings_notification_required),
        style = MaterialTheme.typography.bodySmall,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        modifier = Modifier.padding(top = 4.dp),
    )
}

/** State of the notifications in the system settings and a way there. */
@Composable
internal fun SystemNotifications() {
    val context = LocalContext.current
    var enabled by remember { mutableStateOf(NotificationSystem.enabled(context)) }
    var serviceBlocked by remember {
        mutableStateOf(NotificationSystem.channelBlocked(context, NotificationChannels.SERVICE))
    }
    val owner = LocalLifecycleOwner.current
    DisposableEffect(owner) {
        val observer =
            LifecycleEventObserver { _, event ->
                if (event == Lifecycle.Event.ON_RESUME) {
                    enabled = NotificationSystem.enabled(context)
                    serviceBlocked = NotificationSystem.channelBlocked(context, NotificationChannels.SERVICE)
                }
            }
        owner.lifecycle.addObserver(observer)
        onDispose { owner.lifecycle.removeObserver(observer) }
    }

    SectionTitle(text = stringResource(R.string.settings_section_system))
    if (!enabled) {
        Text(
            text = stringResource(R.string.settings_notification_system_off),
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.error,
        )
    } else if (serviceBlocked) {
        Text(
            text = stringResource(R.string.settings_notification_channel_off),
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
    OutlinedButton(
        onClick = { NotificationSystem.openSettings(context) },
        modifier =
            Modifier
                .padding(top = 8.dp)
                .testTag(SYSTEM_TAG),
    ) {
        Text(text = stringResource(R.string.settings_notification_system))
    }
}

/** Tag of the switch of the transfer notifications. */
internal const val TRANSFERS_TAG = "settings.notifications.transfers"

/** Tag of the switch of the file offers. */
internal const val REQUESTS_TAG = "settings.notifications.requests"

/** Tag of the switch of the clipboard notifications. */
internal const val CLIPBOARD_TAG = "settings.notifications.clipboard"

/** Tag of the switch of the confirmation notifications. */
internal const val CONFIRMATIONS_TAG = "settings.notifications.confirmations"

/** Tag of the switch that keeps the status notification hidden after a dismissal. */
internal const val HIDE_STATUS_TAG = "settings.notifications.hide_status"

/** Tag of the switch that shows the details in the status notification. */
internal const val STATUS_DETAILS_TAG = "settings.notifications.status_details"

/** Tag of the button that opens the system notification settings. */
internal const val SYSTEM_TAG = "settings.notifications.system"
