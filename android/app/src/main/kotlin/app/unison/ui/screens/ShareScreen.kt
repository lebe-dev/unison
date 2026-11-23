package app.unison.ui.screens

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.toggleable
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.Checkbox
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.unit.dp
import app.unison.R
import app.unison.history.HistoryText
import app.unison.share.ShareContent
import app.unison.share.ShareIntents
import app.unison.share.allowsFriends
import app.unison.ui.ErrorCard
import app.unison.ui.GroupItem
import app.unison.ui.ItemGroup
import app.unison.ui.ScreenTitle
import app.unison.ui.SectionTitle
import app.unison.ui.groupItemColor
import app.unison.ui.groupShape
import app.unison.ui.pathLabel
import app.unison.ui.platformLabel
import app.unison.ui.screenColor
import app.unison.ui.sizeUnits
import app.unison.ui.state.CoreState
import unison.local.v1.Local

/** Devices that receive the content of another app. */
@Composable
fun ShareScreen(
    content: ShareContent,
    state: CoreState,
    selection: Set<String>,
    sending: Boolean,
    onToggle: (String, Boolean) -> Unit,
    onSend: () -> Unit,
    onRetry: () -> Unit,
    onClose: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val personal = state.devices(Local.Role.ROLE_PERSONAL)
    val friends = if (content.allowsFriends) state.devices(Local.Role.ROLE_FRIENDS) else emptyList()
    val online = personal.filter { it.online }

    Scaffold(
        modifier = modifier.fillMaxSize(),
        containerColor = screenColor,
        bottomBar = {
            ShareAction(
                sending = sending,
                enabled = canSend(content, personal, friends, selection, sending),
                onSend = onSend,
            )
        },
    ) { padding ->
        Column(
            modifier =
                Modifier
                    .fillMaxSize()
                    .padding(padding)
                    .verticalScroll(rememberScrollState())
                    .padding(horizontal = 16.dp),
        ) {
            ScreenTitle(title = stringResource(R.string.share_label)) {
                TextButton(onClick = onClose) {
                    Text(text = stringResource(R.string.action_cancel))
                }
            }

            ShareSummary(content = content)

            state.failure?.let { failure ->
                Spacer(modifier = Modifier.height(16.dp))
                ErrorCard(message = stringResource(failure.message), onRetry = onRetry)
            }

            if (state.loading) {
                Box(
                    modifier =
                        Modifier
                            .fillMaxWidth()
                            .padding(vertical = 32.dp),
                    contentAlignment = Alignment.Center,
                ) {
                    CircularProgressIndicator()
                }
                return@Column
            }

            if (content is ShareContent.Files) {
                DeviceSection(
                    title = stringResource(R.string.devices_section_personal),
                    empty = stringResource(R.string.devices_empty_personal),
                    devices = personal,
                    selection = selection,
                    enabled = !sending,
                    onToggle = onToggle,
                )
                DeviceSection(
                    title = stringResource(R.string.devices_section_friends),
                    empty = stringResource(R.string.devices_empty_friends),
                    devices = friends,
                    selection = selection,
                    enabled = !sending,
                    onToggle = onToggle,
                )
            } else {
                TextRecipients(devices = online)
            }

            if (state.failure == null && online.isEmpty() && friends.none { it.online }) {
                Spacer(modifier = Modifier.height(16.dp))
                Text(
                    text = stringResource(R.string.share_no_online),
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }

            if (content is ShareContent.Files && content.files.size > ShareIntents.MAX_FILES) {
                Spacer(modifier = Modifier.height(16.dp))
                Text(
                    text = stringResource(R.string.share_too_many_files),
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.error,
                )
            }

            Spacer(modifier = Modifier.height(24.dp))
        }
    }
}

/** True when the content has an online device to go to. */
private fun canSend(
    content: ShareContent,
    personal: List<Local.Device>,
    friends: List<Local.Device>,
    selection: Set<String>,
    sending: Boolean,
): Boolean {
    if (sending) {
        return false
    }
    return when (content) {
        // Text is announced to every online Personal device, so the list decides.
        is ShareContent.Text -> personal.any { it.online }
        is ShareContent.Files ->
            content.files.size <= ShareIntents.MAX_FILES &&
                (personal + friends).any { it.online && it.deviceId in selection }
    }
}

/** Content of the share request with its size or its first characters. */
@Composable
private fun ShareSummary(content: ShareContent) {
    Surface(
        modifier = Modifier.fillMaxWidth(),
        shape = MaterialTheme.shapes.large,
        color = groupItemColor,
    ) {
        Column(modifier = Modifier.padding(16.dp)) {
            when (content) {
                is ShareContent.Text -> {
                    Text(
                        text = stringResource(R.string.share_text_summary),
                        style = MaterialTheme.typography.titleMedium,
                    )
                    val preview = content.draft.preview()
                    if (preview.isNotEmpty()) {
                        Spacer(modifier = Modifier.height(8.dp))
                        Text(text = preview, style = MaterialTheme.typography.bodyMedium, maxLines = 3)
                    }
                    Spacer(modifier = Modifier.height(8.dp))
                    Text(
                        text = stringResource(R.string.share_text_note),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                is ShareContent.Files -> {
                    val total = content.files.sumOf { it.size }
                    Text(
                        text =
                            pluralStringResource(
                                R.plurals.share_files_summary,
                                content.files.size,
                                content.files.size,
                                HistoryText.formatSize(total, sizeUnits()),
                            ),
                        style = MaterialTheme.typography.titleMedium,
                    )
                    val names = fileNames(content)
                    if (names.isNotEmpty()) {
                        Spacer(modifier = Modifier.height(8.dp))
                        Text(
                            text = names,
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            maxLines = 2,
                        )
                    }
                }
            }
        }
    }
}

/** First file names of the request and the number that did not fit. */
@Composable
private fun fileNames(
    content: ShareContent.Files,
    limit: Int = SUMMARY_NAMES,
): String {
    val names = content.files.take(limit).joinToString(", ") { it.name }
    val rest = (content.files.size - limit).coerceAtLeast(0)
    return if (rest > 0) {
        pluralStringResource(R.plurals.names_more, rest, names, rest)
    } else {
        names
    }
}

/** Recipients of a text share, which are not selectable. */
@Composable
private fun TextRecipients(devices: List<Local.Device>) {
    SectionTitle(text = stringResource(R.string.devices_section_personal))
    if (devices.isEmpty()) {
        Text(
            text = stringResource(R.string.devices_empty_personal),
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        return
    }
    Text(
        text =
            stringResource(
                R.string.share_text_recipients,
                devices.joinToString(", ") { it.name.ifEmpty { it.deviceId.take(SHORT_ID_LENGTH) } },
            ),
        style = MaterialTheme.typography.bodyMedium,
    )
}

/** One role of the devices with its selectable rows. */
@Composable
private fun DeviceSection(
    title: String,
    empty: String,
    devices: List<Local.Device>,
    selection: Set<String>,
    enabled: Boolean,
    onToggle: (String, Boolean) -> Unit,
) {
    SectionTitle(text = title)
    if (devices.isEmpty()) {
        Text(
            text = empty,
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        return
    }
    ItemGroup {
        devices.forEachIndexed { index, device ->
            SelectableDeviceRow(
                index = index,
                count = devices.size,
                device = device,
                checked = device.deviceId in selection,
                enabled = enabled && device.online,
                onToggle = { onToggle(device.deviceId, it) },
            )
        }
    }
}

/** One device with a checkbox; offline devices cannot be picked. */
@Composable
private fun SelectableDeviceRow(
    index: Int,
    count: Int,
    device: Local.Device,
    checked: Boolean,
    enabled: Boolean,
    onToggle: (Boolean) -> Unit,
) {
    GroupItem(
        index = index,
        count = count,
        modifier =
            Modifier
                .clip(groupShape(index, count))
                .toggleable(value = checked, enabled = enabled, role = Role.Checkbox, onValueChange = onToggle),
    ) {
        Column(modifier = Modifier.weight(1f)) {
            Text(
                text = device.name.ifEmpty { device.deviceId.take(SHORT_ID_LENGTH) },
                style = MaterialTheme.typography.titleMedium,
            )
            val path = pathLabel(device.path)
            Text(
                text =
                    if (path != null && device.online) {
                        stringResource(R.string.device_detail, platformLabel(device.platform), path)
                    } else {
                        platformLabel(device.platform)
                    },
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        Text(
            text =
                stringResource(
                    if (device.online) R.string.device_online else R.string.device_offline,
                ),
            style = MaterialTheme.typography.labelMedium,
            color =
                if (device.online) {
                    MaterialTheme.colorScheme.primary
                } else {
                    MaterialTheme.colorScheme.onSurfaceVariant
                },
        )
        Checkbox(checked = checked, onCheckedChange = null, enabled = enabled)
    }
}

/** Send button of the screen. */
@Composable
private fun ShareAction(
    sending: Boolean,
    enabled: Boolean,
    onSend: () -> Unit,
) {
    Surface(color = MaterialTheme.colorScheme.surfaceContainerHigh) {
        Button(
            onClick = onSend,
            enabled = enabled,
            modifier =
                Modifier
                    .fillMaxWidth()
                    .navigationBarsPadding()
                    .padding(16.dp)
                    .heightIn(min = 56.dp),
        ) {
            if (sending) {
                CircularProgressIndicator(
                    modifier = Modifier.size(18.dp),
                    strokeWidth = 2.dp,
                    color = MaterialTheme.colorScheme.onPrimary,
                )
                Spacer(modifier = Modifier.width(12.dp))
                Text(text = stringResource(R.string.share_sending))
            } else {
                Text(text = stringResource(R.string.action_send))
            }
        }
    }
}

/** Characters of the device ID shown when the name is empty. */
private const val SHORT_ID_LENGTH = 8

/** File names shown in the summary. */
private const val SUMMARY_NAMES = 3
