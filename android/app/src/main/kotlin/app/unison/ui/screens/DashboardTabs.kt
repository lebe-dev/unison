package app.unison.ui.screens

import android.text.format.DateUtils
import androidx.annotation.DrawableRes
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.Shape
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import app.unison.R
import app.unison.history.FileNames
import app.unison.history.HistoryText
import app.unison.history.HistoryThumbnailLoader
import app.unison.history.HistoryThumbnails
import app.unison.history.SizeUnits
import app.unison.ui.IconTile
import app.unison.ui.groupItemColor
import app.unison.ui.groupShape
import app.unison.ui.sizeUnits
import unison.local.v1.Local

/** The newest images as square thumbnails. */
@Composable
internal fun ImageGrid(
    entries: List<Local.HistoryItem>,
    thumbnail: HistoryThumbnailLoader,
    actions: DashboardItemActions,
) {
    LazyVerticalGrid(
        columns = GridCells.Adaptive(minSize = 100.dp),
        modifier = Modifier.fillMaxSize(),
        verticalArrangement = Arrangement.spacedBy(8.dp),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
        contentPadding = PaddingValues(bottom = 16.dp),
    ) {
        items(entries, key = { it.item.id }) { entry ->
            ImageTile(
                entry = entry,
                thumbnail = thumbnail,
                actions = actions,
            )
        }
    }
}

/** One image of the grid, or a placeholder while the payload is away. */
@Composable
private fun ImageTile(
    entry: Local.HistoryItem,
    thumbnail: HistoryThumbnailLoader,
    actions: DashboardItemActions,
) {
    val item = entry.item
    var image by remember(item.id) { mutableStateOf<ImageBitmap?>(null) }
    var loading by remember(item.id, entry.complete) { mutableStateOf(entry.complete) }
    var menuOpen by remember(item.id) { mutableStateOf(false) }
    LaunchedEffect(item.id, entry.complete) {
        if (entry.complete) {
            image = thumbnail(item.id, HistoryThumbnails.TILE_SIDE)
        }
        loading = false
    }

    Box(
        modifier =
            Modifier
                .aspectRatio(1f)
                .clip(MaterialTheme.shapes.medium)
                .background(MaterialTheme.colorScheme.surfaceContainerHigh)
                .combinedClickable(
                    onClick = { actions.onOpen(entry) },
                    onLongClickLabel = stringResource(R.string.dashboard_item_actions),
                    onLongClick = { menuOpen = true },
                ).testTag(itemTag(item)),
        contentAlignment = Alignment.Center,
    ) {
        val bitmap = image
        when {
            !entry.complete -> NotDownloaded()
            bitmap != null ->
                Image(
                    bitmap = bitmap,
                    contentDescription = null,
                    contentScale = ContentScale.Crop,
                    modifier = Modifier.fillMaxSize(),
                )
            loading -> CircularProgressIndicator(modifier = Modifier.size(20.dp), strokeWidth = 2.dp)
        }
        DashboardItemMenu(
            expanded = menuOpen,
            devices = actions.devices,
            onDismiss = { menuOpen = false },
            onSend = { deviceId -> actions.onSend(entry, deviceId) },
            onCopy = { actions.onCopy(entry) },
            onDelete = { actions.onDelete(entry) },
        )
    }
}

/** Grouped vertical list of file or text entries. */
@Composable
internal fun DashboardEntryList(
    entries: List<Local.HistoryItem>,
    card: @Composable (Local.HistoryItem, SizeUnits, Shape) -> Unit,
) {
    val units = sizeUnits()
    LazyColumn(
        modifier = Modifier.fillMaxSize(),
        verticalArrangement = Arrangement.spacedBy(2.dp),
        contentPadding = PaddingValues(bottom = 16.dp),
    ) {
        itemsIndexed(entries, key = { _, entry -> entry.item.id }) { index, entry ->
            card(entry, units, groupShape(index, entries.size))
        }
    }
}

/** One file entry of the dashboard. */
@Composable
internal fun FileCard(
    entry: Local.HistoryItem,
    units: SizeUnits,
    shape: Shape,
    actions: DashboardItemActions,
) {
    val item = entry.item
    val names = HistoryText.fileNames(item)
    DashboardItemCard(
        entry = entry,
        units = units,
        shape = shape,
        icon = R.drawable.ic_kind_file,
        iconContainer = MaterialTheme.colorScheme.secondaryContainer,
        iconContent = MaterialTheme.colorScheme.onSecondaryContainer,
        actions = actions,
    ) {
        Text(
            text = fileTitle(names),
            style = MaterialTheme.typography.titleMedium,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
        item.representationsList.forEach { representation ->
            FileRepresentationRow(
                representation = representation,
                units = units,
                modifier = Modifier.padding(top = 2.dp),
            )
        }
    }
}

/** One text entry of the dashboard. */
@Composable
internal fun TextCard(
    entry: Local.HistoryItem,
    units: SizeUnits,
    shape: Shape,
    actions: DashboardItemActions,
) {
    val item = entry.item
    DashboardItemCard(
        entry = entry,
        units = units,
        shape = shape,
        icon = R.drawable.ic_kind_text,
        iconContainer = MaterialTheme.colorScheme.tertiaryContainer,
        iconContent = MaterialTheme.colorScheme.onTertiaryContainer,
        actions = actions,
    ) {
        Text(
            text = HistoryText.text(item).ifEmpty { stringResource(R.string.kind_text) },
            style = MaterialTheme.typography.bodyLarge,
            maxLines = 3,
            overflow = TextOverflow.Ellipsis,
        )
    }
}

/** Shared card layout for file and text entries with metadata and context menu. */
@Composable
private fun DashboardItemCard(
    entry: Local.HistoryItem,
    units: SizeUnits,
    shape: Shape,
    @DrawableRes icon: Int,
    iconContainer: Color,
    iconContent: Color,
    actions: DashboardItemActions,
    body: @Composable ColumnScope.() -> Unit,
) {
    val item = entry.item
    var menuOpen by remember(item.id) { mutableStateOf(false) }
    Box {
        Column(
            modifier =
                Modifier
                    .fillMaxWidth()
                    .clip(shape)
                    .background(groupItemColor)
                    .combinedClickable(
                        onClick = { actions.onOpen(entry) },
                        onLongClickLabel = stringResource(R.string.dashboard_item_actions),
                        onLongClick = { menuOpen = true },
                    ).testTag(itemTag(item))
                    .padding(16.dp),
        ) {
            Row(horizontalArrangement = Arrangement.spacedBy(16.dp)) {
                IconTile(icon = icon, container = iconContainer, content = iconContent)
                Column(modifier = Modifier.weight(1f)) {
                    body()
                    Text(
                        text =
                            metaLabel(
                                time = relativeTime(item.createdAtUnixMs),
                                size = HistoryText.formatSize(HistoryText.size(item), units),
                            ),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.padding(top = 4.dp),
                    )
                    if (!entry.complete) {
                        Text(
                            text = stringResource(R.string.dashboard_not_downloaded),
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                }
            }
        }
        DashboardItemMenu(
            expanded = menuOpen,
            devices = actions.devices,
            onDismiss = { menuOpen = false },
            onSend = { deviceId -> actions.onSend(entry, deviceId) },
            onCopy = { actions.onCopy(entry) },
            onDelete = { actions.onDelete(entry) },
        )
    }
}

/** One file name and its formatted size. */
@Composable
internal fun FileRepresentationRow(
    representation: Local.Representation,
    units: SizeUnits,
    modifier: Modifier = Modifier,
) {
    Row(
        modifier = modifier,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            text = representation.fileName.ifBlank { stringResource(R.string.kind_files) },
            style = MaterialTheme.typography.bodyMedium,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
            modifier = Modifier.weight(1f),
        )
        Text(
            text = HistoryText.formatSize(HistoryText.size(representation), units),
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}

/** Placeholder of an image whose payload is not on this device. */
@Composable
private fun NotDownloaded() {
    Column(
        modifier = Modifier.padding(8.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Icon(
            painter = painterResource(R.drawable.ic_kind_image),
            contentDescription = null,
            tint = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.size(22.dp),
        )
        Text(
            text = stringResource(R.string.dashboard_not_downloaded),
            style = MaterialTheme.typography.labelSmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}

/** Names of the files of an entry, with the number that did not fit. */
@Composable
private fun fileTitle(names: FileNames): String {
    if (names.rest > 0) {
        return pluralStringResource(R.plurals.names_more, names.rest, names.shown, names.rest)
    }
    return names.shown.ifEmpty { stringResource(R.string.kind_files) }
}

/** Time and size of an entry in one line. */
@Composable
private fun metaLabel(
    time: String,
    size: String,
): String = if (time.isEmpty()) size else stringResource(R.string.history_item_detail, time, size)

/** Time of the item relative to now. */
private fun relativeTime(unixMs: Long): String {
    if (unixMs <= 0) {
        return ""
    }
    return DateUtils
        .getRelativeTimeSpanString(unixMs, System.currentTimeMillis(), DateUtils.MINUTE_IN_MILLIS)
        .toString()
}

/** Tag of the tile or the row of an entry. */
internal fun itemTag(item: Local.ClipItem): String = "dashboard.item.${item.id}"
