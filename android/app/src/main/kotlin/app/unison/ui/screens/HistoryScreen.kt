package app.unison.ui.screens

import android.text.format.DateUtils
import androidx.compose.foundation.background
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.FilterChip
import androidx.compose.material3.FilterChipDefaults
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Shape
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import app.unison.R
import app.unison.history.HistoryText
import app.unison.ui.ErrorCard
import app.unison.ui.IconTile
import app.unison.ui.ScreenTitle
import app.unison.ui.groupItemColor
import app.unison.ui.groupShape
import app.unison.ui.state.HistoryState
import unison.local.v1.Local

/** History of the synchronized items. */
@Composable
fun HistoryScreen(
    history: HistoryState,
    status: Local.GetStatusResponse?,
    held: Set<String>,
    onRefresh: () -> Unit,
    onKindChange: (Local.Kind) -> Unit,
    onDeviceChange: (String) -> Unit,
    onCopy: (Local.HistoryItem) -> Unit,
    onApply: (Local.HistoryItem) -> Unit,
    onDownload: (Local.HistoryItem) -> Unit,
    onDelete: (Local.HistoryItem) -> Unit,
    modifier: Modifier = Modifier,
) {
    Column(
        modifier = modifier
            .fillMaxSize()
            .padding(horizontal = 16.dp),
    ) {
        val listState = rememberLazyListState()
        val newest = history.items.firstOrNull()?.item?.id
        LaunchedEffect(newest) {
            // An entry that arrives while the list is at its top stays visible:
            // a keyed list would otherwise keep the older entry in place and
            // push the new one above the screen. A list the user scrolled
            // further down keeps its position.
            if (newest != null && listState.firstVisibleItemIndex <= 1) {
                listState.requestScrollToItem(0)
            }
        }

        ScreenTitle(title = stringResource(R.string.nav_history)) {
            TextButton(onClick = onRefresh) {
                Text(text = stringResource(R.string.action_refresh))
            }
        }

        Filters(history = history, status = status, onKindChange = onKindChange, onDeviceChange = onDeviceChange)
        Spacer(modifier = Modifier.height(12.dp))

        history.failure?.let { failure ->
            Spacer(modifier = Modifier.height(16.dp))
            ErrorCard(message = stringResource(failure.message), onRetry = onRefresh)
        }

        if (history.loading && !history.loaded) {
            Box(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(vertical = 32.dp),
                contentAlignment = Alignment.Center,
            ) {
                CircularProgressIndicator()
            }
            return@Column
        }

        if (history.isEmpty) {
            EmptyHistory()
        } else {
            LazyColumn(
                state = listState,
                modifier = Modifier.fillMaxSize(),
                verticalArrangement = Arrangement.spacedBy(2.dp),
                contentPadding = PaddingValues(bottom = 16.dp),
            ) {
                itemsIndexed(history.items, key = { _, entry -> entry.item.id }) { index, entry ->
                    HistoryRow(
                        entry = entry,
                        shape = groupShape(index, history.items.size),
                        device = deviceName(status, entry.item.origin),
                        busy = entry.item.id in history.busy,
                        held = entry.item.id in held,
                        onCopy = { onCopy(entry) },
                        onApply = { onApply(entry) },
                        onDownload = { onDownload(entry) },
                        onDelete = { onDelete(entry) },
                    )
                }
            }
        }
    }
}

/** Kind chips and the device chip in one row that scrolls sideways. */
@Composable
private fun Filters(
    history: HistoryState,
    status: Local.GetStatusResponse?,
    onKindChange: (Local.Kind) -> Unit,
    onDeviceChange: (String) -> Unit,
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .horizontalScroll(rememberScrollState()),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        KINDS.forEach { kind ->
            FilterChip(
                selected = kind.kind == history.kind,
                onClick = { onKindChange(kind.kind) },
                label = { Text(text = stringResource(kind.label)) },
                leadingIcon = if (kind.kind == history.kind) {
                    {
                        Icon(
                            painter = painterResource(R.drawable.ic_check),
                            contentDescription = null,
                            modifier = Modifier.size(FilterChipDefaults.IconSize),
                        )
                    }
                } else {
                    null
                },
            )
        }
        DeviceFilter(history = history, status = status, onChange = onDeviceChange)
    }
}

/** Choice of the device whose entries are shown. */
@Composable
private fun DeviceFilter(
    history: HistoryState,
    status: Local.GetStatusResponse?,
    onChange: (String) -> Unit,
) {
    var expanded by remember { mutableStateOf(false) }
    val devices = remember(status, history.items) { devicesOf(status, history.items) }
    val label = devices.firstOrNull { it.first == history.deviceId }?.second
        ?: stringResource(R.string.history_all_devices)

    Box {
        FilterChip(
            selected = history.deviceId.isNotEmpty(),
            onClick = { expanded = true },
            label = { Text(text = stringResource(R.string.history_filter_device, label)) },
            trailingIcon = {
                Icon(
                    painter = painterResource(R.drawable.ic_expand),
                    contentDescription = null,
                    modifier = Modifier.size(FilterChipDefaults.IconSize),
                )
            },
        )
        DropdownMenu(expanded = expanded, onDismissRequest = { expanded = false }) {
            DropdownMenuItem(
                text = { Text(text = stringResource(R.string.history_all_devices)) },
                onClick = {
                    expanded = false
                    onChange("")
                },
            )
            devices.forEach { (deviceId, name) ->
                DropdownMenuItem(
                    text = { Text(text = name) },
                    onClick = {
                        expanded = false
                        onChange(deviceId)
                    },
                )
            }
        }
    }
}

/** One entry of the history with its actions. */
@Composable
private fun HistoryRow(
    entry: Local.HistoryItem,
    shape: Shape,
    device: String,
    busy: Boolean,
    held: Boolean,
    onCopy: () -> Unit,
    onApply: () -> Unit,
    onDownload: () -> Unit,
    onDelete: () -> Unit,
) {
    val item = entry.item
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(shape)
            .background(groupItemColor)
            .padding(start = 16.dp, end = 8.dp, top = 14.dp, bottom = 4.dp),
    ) {
        Row(horizontalArrangement = Arrangement.spacedBy(16.dp)) {
            KindTile(kind = item.kind)
            Column(modifier = Modifier.weight(1f)) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(
                        text = stringResource(kindLabel(item.kind)),
                        style = MaterialTheme.typography.labelLarge,
                        color = MaterialTheme.colorScheme.primary,
                    )
                    if (held) {
                        Text(
                            text = stringResource(R.string.history_held),
                            style = MaterialTheme.typography.labelMedium,
                            color = MaterialTheme.colorScheme.onTertiaryContainer,
                            modifier = Modifier
                                .padding(start = 8.dp)
                                .clip(MaterialTheme.shapes.extraSmall)
                                .background(MaterialTheme.colorScheme.tertiaryContainer)
                                .padding(horizontal = 8.dp, vertical = 2.dp),
                        )
                    }
                    Spacer(modifier = Modifier.weight(1f))
                    if (busy) {
                        CircularProgressIndicator(modifier = Modifier.size(16.dp), strokeWidth = 2.dp)
                    } else {
                        Text(
                            text = relativeTime(item.createdAtUnixMs),
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            modifier = Modifier.padding(end = 8.dp),
                        )
                    }
                }

                val preview = previewOf(item)
                if (preview.isNotEmpty()) {
                    Text(
                        text = preview,
                        style = MaterialTheme.typography.bodyLarge,
                        maxLines = 2,
                        overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.padding(top = 2.dp, end = 8.dp),
                    )
                }

                val size = HistoryText.formatSize(HistoryText.size(item))
                Text(
                    text = stringResource(
                        if (entry.complete) R.string.history_item_detail else R.string.history_item_detail_incomplete,
                        device,
                        size,
                    ),
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.padding(top = 2.dp),
                )
            }
        }

        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.End,
            verticalAlignment = Alignment.CenterVertically,
        ) {
            TextButton(onClick = onDelete, enabled = !busy) {
                Text(text = stringResource(R.string.action_delete))
            }
            TextButton(onClick = onDownload, enabled = !busy) {
                Text(text = stringResource(R.string.action_download))
            }
            if (held) {
                FilledTonalButton(onClick = onApply, enabled = !busy) {
                    Text(text = stringResource(R.string.action_apply))
                }
            } else if (item.kind != Local.Kind.KIND_FILES) {
                FilledTonalButton(onClick = onCopy, enabled = !busy) {
                    Text(text = stringResource(R.string.action_copy))
                }
            }
        }
    }
}

/** Icon of a content kind in a tile tinted by the kind. */
@Composable
private fun KindTile(kind: Local.Kind) {
    val colors = MaterialTheme.colorScheme
    when (kind) {
        Local.Kind.KIND_IMAGE -> IconTile(
            icon = R.drawable.ic_kind_image,
            container = colors.tertiaryContainer,
            content = colors.onTertiaryContainer,
        )
        Local.Kind.KIND_FILES -> IconTile(
            icon = R.drawable.ic_kind_file,
            container = colors.secondaryContainer,
            content = colors.onSecondaryContainer,
        )
        else -> IconTile(icon = R.drawable.ic_kind_text)
    }
}

/** Explanation shown when the history holds nothing. */
@Composable
private fun EmptyHistory() {
    Column(
        modifier = Modifier
            .fillMaxSize()
            .padding(bottom = 48.dp),
        verticalArrangement = Arrangement.Center,
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Text(
            text = stringResource(R.string.history_empty),
            style = MaterialTheme.typography.titleMedium,
        )
        Text(
            text = stringResource(R.string.history_empty_body),
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.padding(top = 8.dp),
        )
    }
}

/** Name of a device, shortened when the name is unknown. */
private fun deviceName(status: Local.GetStatusResponse?, deviceId: String): String {
    if (deviceId.isEmpty()) {
        return ""
    }
    val known = status?.devicesList?.firstOrNull { it.deviceId == deviceId }
    if (known != null) {
        return known.name.ifEmpty { deviceId.take(SHORT_ID_LENGTH) }
    }
    if (status?.deviceId == deviceId) {
        return status.deviceName.ifEmpty { deviceId.take(SHORT_ID_LENGTH) }
    }
    return deviceId.take(SHORT_ID_LENGTH)
}

/** Devices of the filter menu: this device first, then the paired ones. */
private fun devicesOf(
    status: Local.GetStatusResponse?,
    items: List<Local.HistoryItem>,
): List<Pair<String, String>> {
    val devices = mutableListOf<Pair<String, String>>()
    if (status != null && status.deviceId.isNotEmpty()) {
        devices += status.deviceId to status.deviceName.ifEmpty { status.deviceId.take(SHORT_ID_LENGTH) }
        status.devicesList.forEach { device ->
            devices += device.deviceId to device.name.ifEmpty { device.deviceId.take(SHORT_ID_LENGTH) }
        }
    }
    items.forEach { entry ->
        val origin = entry.item.origin
        if (origin.isNotEmpty() && devices.none { it.first == origin }) {
            devices += origin to origin.take(SHORT_ID_LENGTH)
        }
    }
    return devices
}

/** Short preview of the item content. */
private fun previewOf(item: Local.ClipItem): String = when (item.kind) {
    Local.Kind.KIND_TEXT -> HistoryText.text(item)
    Local.Kind.KIND_FILES -> HistoryText.fileNames(item)
    else -> ""
}

/** Time of the item relative to now. */
private fun relativeTime(unixMs: Long): String {
    if (unixMs <= 0) {
        return ""
    }
    return DateUtils.getRelativeTimeSpanString(unixMs, System.currentTimeMillis(), DateUtils.MINUTE_IN_MILLIS)
        .toString()
}

/** Label of a content kind. */
private fun kindLabel(kind: Local.Kind): Int = when (kind) {
    Local.Kind.KIND_IMAGE -> R.string.kind_image
    Local.Kind.KIND_FILES -> R.string.kind_files
    else -> R.string.kind_text
}

/** Filter rows with their labels. */
private val KINDS = listOf(
    Filter(Local.Kind.KIND_UNSPECIFIED, R.string.history_filter_all),
    Filter(Local.Kind.KIND_TEXT, R.string.history_filter_text),
    Filter(Local.Kind.KIND_IMAGE, R.string.history_filter_images),
    Filter(Local.Kind.KIND_FILES, R.string.history_filter_files),
)

/** One row of the kind filter. */
private data class Filter(val kind: Local.Kind, val label: Int)

/** Characters of a device ID shown when the name is unknown. */
private const val SHORT_ID_LENGTH = 8
