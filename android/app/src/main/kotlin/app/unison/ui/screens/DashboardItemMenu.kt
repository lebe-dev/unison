package app.unison.ui.screens

import androidx.compose.foundation.layout.padding
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import app.unison.R
import app.unison.ui.shortId
import unison.local.v1.Local

/** Step of the context menu of an item. */
private enum class MenuStep {
    ACTIONS,
    DEVICES,
}

/** Context menu of one dashboard item: Send, Copy and Delete. */
@Composable
internal fun DashboardItemMenu(
    expanded: Boolean,
    devices: List<Local.Device>,
    onDismiss: () -> Unit,
    onSend: (String) -> Unit,
    onCopy: () -> Unit,
    onDelete: () -> Unit,
) {
    var step by remember { mutableStateOf(MenuStep.ACTIONS) }
    DropdownMenu(
        expanded = expanded,
        onDismissRequest = {
            step = MenuStep.ACTIONS
            onDismiss()
        },
    ) {
        when (step) {
            MenuStep.ACTIONS -> {
                DropdownMenuItem(
                    text = { Text(text = stringResource(R.string.dashboard_send)) },
                    onClick = { step = MenuStep.DEVICES },
                    modifier = Modifier.testTag(MENU_SEND_TAG),
                )
                DropdownMenuItem(
                    text = { Text(text = stringResource(R.string.action_copy)) },
                    onClick = {
                        step = MenuStep.ACTIONS
                        onDismiss()
                        onCopy()
                    },
                    modifier = Modifier.testTag(MENU_COPY_TAG),
                )
                DropdownMenuItem(
                    text = {
                        Text(
                            text = stringResource(R.string.action_delete),
                            color = MaterialTheme.colorScheme.error,
                        )
                    },
                    onClick = {
                        step = MenuStep.ACTIONS
                        onDismiss()
                        onDelete()
                    },
                    modifier = Modifier.testTag(MENU_DELETE_TAG),
                )
            }
            MenuStep.DEVICES -> {
                Text(
                    text = stringResource(R.string.dashboard_send_to),
                    style = MaterialTheme.typography.labelMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
                )
                if (devices.isEmpty()) {
                    DropdownMenuItem(
                        text = { Text(text = stringResource(R.string.dashboard_no_devices)) },
                        onClick = {},
                        enabled = false,
                    )
                }
                devices.forEach { device ->
                    DropdownMenuItem(
                        text = { Text(text = device.name.ifEmpty { shortId(device.deviceId) }) },
                        onClick = {
                            step = MenuStep.ACTIONS
                            onDismiss()
                            onSend(device.deviceId)
                        },
                        modifier = Modifier.testTag(deviceMenuTag(device.deviceId)),
                    )
                }
                DropdownMenuItem(
                    text = { Text(text = stringResource(R.string.action_back)) },
                    onClick = { step = MenuStep.ACTIONS },
                )
            }
        }
    }
}

/** Tag of a device in the send picker. */
internal fun deviceMenuTag(deviceId: String): String = "dashboard.send.$deviceId"

/** Tag of the send entry of the context menu. */
internal const val MENU_SEND_TAG = "dashboard.menu.send"

/** Tag of the copy entry of the context menu. */
internal const val MENU_COPY_TAG = "dashboard.menu.copy"

/** Tag of the delete entry of the context menu. */
internal const val MENU_DELETE_TAG = "dashboard.menu.delete"
