package app.unison.ui.screens

import android.text.format.DateUtils
import androidx.annotation.StringRes
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.PrimaryTabRow
import androidx.compose.material3.Tab
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
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
import app.unison.ui.ConfirmDialog
import app.unison.ui.ErrorCard
import app.unison.ui.IconTile
import app.unison.ui.ScreenTitle
import app.unison.ui.UpdateBanner
import app.unison.ui.UpdatePanel
import app.unison.ui.groupItemColor
import app.unison.ui.groupShape
import app.unison.ui.sizeUnits
import app.unison.ui.state.DashboardState
import unison.local.v1.Local

/**
 * The first screen: the newest items of the history in three tabs. Images open
 * as a grid of thumbnails, files as a list with their sizes and text as
 * snippets. A tap opens a preview with the actions of the item.
 */
@Composable
fun DashboardScreen(
    panel: DashboardPanel,
    held: Set<String>,
    busy: Set<String>,
    modifier: Modifier = Modifier,
) {
    val dashboard = panel.state
    var selected by rememberSaveable { mutableStateOf(DashboardTab.IMAGES.ordinal) }
    var preview by remember { mutableStateOf<Local.HistoryItem?>(null) }
    var deleting by remember { mutableStateOf<Local.HistoryItem?>(null) }

    deleting?.let { entry ->
        ConfirmDialog(
            title = stringResource(R.string.dashboard_delete_title),
            body = stringResource(R.string.dashboard_delete_body),
            onConfirm = {
                deleting = null
                preview = null
                panel.onDelete(entry)
            },
            onDismiss = { deleting = null },
            confirmLabel = R.string.dashboard_delete_confirm,
        )
    }

    preview?.let { entry ->
        PreviewDialog(
            entry = entry,
            busy = entry.item.id in busy,
            held = entry.item.id in held,
            thumbnail = panel.thumbnail,
            onClose = { preview = null },
            onCopy = { panel.onCopy(entry) },
            onDownload = { panel.onDownload(entry) },
            onApply = { panel.onApply(entry) },
            onDelete = { deleting = entry },
        )
    }

    Column(
        modifier =
            modifier
                .fillMaxSize()
                .padding(horizontal = 16.dp),
    ) {
        ScreenTitle(title = stringResource(R.string.nav_dashboard)) {
            TextButton(onClick = panel.onRefresh) {
                Text(text = stringResource(R.string.action_refresh))
            }
        }
        Text(
            text = stringResource(R.string.dashboard_newest, dashboard.limit),
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        panel.updates?.let { updates ->
            Spacer(modifier = Modifier.height(12.dp))
            UpdateBanner(panel = updates)
        }
        Spacer(modifier = Modifier.height(8.dp))
        DashboardTabs(
            selected = selected,
            onSelect = { selected = it },
        )
        Spacer(modifier = Modifier.height(12.dp))

        dashboard.failure?.let { failure ->
            ErrorCard(message = stringResource(failure.message), onRetry = panel.onRefresh)
            Spacer(modifier = Modifier.height(16.dp))
        }

        if (!dashboard.loaded) {
            if (dashboard.failure == null) {
                Loading()
            }
            return@Column
        }

        val tab = DashboardTab.entries[selected]
        val entries = tab.itemsOf(dashboard)
        if (entries.isEmpty()) {
            EmptyTab(tab = tab)
        } else {
            when (tab) {
                DashboardTab.IMAGES -> ImageGrid(entries = entries, thumbnail = panel.thumbnail, onOpen = { preview = it })
                DashboardTab.FILES -> FileList(entries = entries, onOpen = { preview = it })
                DashboardTab.TEXT -> TextList(entries = entries, onOpen = { preview = it })
            }
        }
    }
}

/** State and actions of the dashboard, kept together as one screen. */
data class DashboardPanel(
    val state: DashboardState,
    val thumbnail: HistoryThumbnailLoader,
    val onRefresh: () -> Unit,
    val onCopy: (Local.HistoryItem) -> Unit,
    val onApply: (Local.HistoryItem) -> Unit,
    val onDownload: (Local.HistoryItem) -> Unit,
    val onDelete: (Local.HistoryItem) -> Unit,
    val updates: UpdatePanel? = null,
)

/** The three tabs of the dashboard. */
@Composable
private fun DashboardTabs(
    selected: Int,
    onSelect: (Int) -> Unit,
) {
    PrimaryTabRow(selectedTabIndex = selected) {
        DashboardTab.entries.forEach { tab ->
            Tab(
                selected = selected == tab.ordinal,
                onClick = { onSelect(tab.ordinal) },
                modifier = Modifier.testTag(tabTag(tab)),
                text = { Text(text = stringResource(tab.label)) },
            )
        }
    }
}

/** The newest images as square thumbnails. */
@Composable
private fun ImageGrid(
    entries: List<Local.HistoryItem>,
    thumbnail: HistoryThumbnailLoader,
    onOpen: (Local.HistoryItem) -> Unit,
) {
    LazyVerticalGrid(
        columns = GridCells.Adaptive(minSize = 100.dp),
        modifier = Modifier.fillMaxSize(),
        verticalArrangement = Arrangement.spacedBy(8.dp),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
        contentPadding = PaddingValues(bottom = 16.dp),
    ) {
        items(entries, key = { it.item.id }) { entry ->
            ImageTile(entry = entry, thumbnail = thumbnail, onClick = { onOpen(entry) })
        }
    }
}

/** One image of the grid, or a placeholder while the payload is away. */
@Composable
private fun ImageTile(
    entry: Local.HistoryItem,
    thumbnail: HistoryThumbnailLoader,
    onClick: () -> Unit,
) {
    val item = entry.item
    var image by remember(item.id) { mutableStateOf<ImageBitmap?>(null) }
    var loading by remember(item.id) { mutableStateOf(entry.complete) }
    LaunchedEffect(item.id) {
        image = thumbnail(item.id, HistoryThumbnails.TILE_SIDE)
        loading = false
    }

    Box(
        modifier =
            Modifier
                .aspectRatio(1f)
                .clip(MaterialTheme.shapes.medium)
                .background(MaterialTheme.colorScheme.surfaceContainerHigh)
                .clickable(onClick = onClick)
                .testTag(itemTag(item)),
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
    }
}

/** The newest files, each with the names and the sizes of its payloads. */
@Composable
private fun FileList(
    entries: List<Local.HistoryItem>,
    onOpen: (Local.HistoryItem) -> Unit,
) {
    val units = sizeUnits()
    LazyColumn(
        modifier = Modifier.fillMaxSize(),
        verticalArrangement = Arrangement.spacedBy(2.dp),
        contentPadding = PaddingValues(bottom = 16.dp),
    ) {
        itemsIndexed(entries, key = { _, entry -> entry.item.id }) { index, entry ->
            FileCard(
                entry = entry,
                units = units,
                shape = groupShape(index, entries.size),
                onClick = { onOpen(entry) },
            )
        }
    }
}

/** One file entry of the dashboard. */
@Composable
private fun FileCard(
    entry: Local.HistoryItem,
    units: SizeUnits,
    shape: Shape,
    onClick: () -> Unit,
) {
    val item = entry.item
    val names = HistoryText.fileNames(item)
    Column(
        modifier =
            Modifier
                .fillMaxWidth()
                .clip(shape)
                .background(groupItemColor)
                .clickable(onClick = onClick)
                .testTag(itemTag(item))
                .padding(16.dp),
    ) {
        Row(horizontalArrangement = Arrangement.spacedBy(16.dp)) {
            IconTile(
                icon = R.drawable.ic_kind_file,
                container = MaterialTheme.colorScheme.secondaryContainer,
                content = MaterialTheme.colorScheme.onSecondaryContainer,
            )
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = fileTitle(names),
                    style = MaterialTheme.typography.titleMedium,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                item.representationsList.forEach { representation ->
                    Row(
                        modifier = Modifier.padding(top = 2.dp),
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
                            text = sizeLabel(representation, units),
                            style = MaterialTheme.typography.bodyMedium,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                }
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
}

/** The newest text entries with a snippet and their metadata. */
@Composable
private fun TextList(
    entries: List<Local.HistoryItem>,
    onOpen: (Local.HistoryItem) -> Unit,
) {
    val units = sizeUnits()
    LazyColumn(
        modifier = Modifier.fillMaxSize(),
        verticalArrangement = Arrangement.spacedBy(2.dp),
        contentPadding = PaddingValues(bottom = 16.dp),
    ) {
        itemsIndexed(entries, key = { _, entry -> entry.item.id }) { index, entry ->
            TextCard(
                entry = entry,
                units = units,
                shape = groupShape(index, entries.size),
                onClick = { onOpen(entry) },
            )
        }
    }
}

/** One text entry of the dashboard. */
@Composable
private fun TextCard(
    entry: Local.HistoryItem,
    units: SizeUnits,
    shape: Shape,
    onClick: () -> Unit,
) {
    val item = entry.item
    Column(
        modifier =
            Modifier
                .fillMaxWidth()
                .clip(shape)
                .background(groupItemColor)
                .clickable(onClick = onClick)
                .testTag(itemTag(item))
                .padding(16.dp),
    ) {
        Row(horizontalArrangement = Arrangement.spacedBy(16.dp)) {
            IconTile(
                icon = R.drawable.ic_kind_text,
                container = MaterialTheme.colorScheme.tertiaryContainer,
                content = MaterialTheme.colorScheme.onTertiaryContainer,
            )
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = HistoryText.text(item).ifEmpty { stringResource(R.string.kind_text) },
                    style = MaterialTheme.typography.bodyLarge,
                    maxLines = 3,
                    overflow = TextOverflow.Ellipsis,
                )
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
}

/** Explanation of a tab that holds nothing. */
@Composable
private fun EmptyTab(tab: DashboardTab) {
    Column(
        modifier =
            Modifier
                .fillMaxSize()
                .padding(bottom = 48.dp),
        verticalArrangement = Arrangement.Center,
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Text(
            text = stringResource(tab.empty),
            style = MaterialTheme.typography.titleMedium,
        )
        Text(
            text = stringResource(R.string.dashboard_empty_body),
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.padding(top = 8.dp),
        )
    }
}

/** Spinner of the first load. */
@Composable
private fun Loading() {
    Box(
        modifier =
            Modifier
                .fillMaxWidth()
                .padding(vertical = 32.dp)
                .testTag(LOADING_TAG),
        contentAlignment = Alignment.Center,
    ) {
        CircularProgressIndicator()
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

/** Preview of one item with its actions. */
@Composable
private fun PreviewDialog(
    entry: Local.HistoryItem,
    busy: Boolean,
    held: Boolean,
    thumbnail: HistoryThumbnailLoader,
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
                PreviewBody(entry = entry, thumbnail = thumbnail)
                Row(
                    modifier =
                        Modifier
                            .fillMaxWidth()
                            .padding(top = 12.dp),
                    horizontalArrangement = Arrangement.End,
                ) {
                    if (item.kind == Local.Kind.KIND_FILES) {
                        TextButton(
                            onClick = onCopy,
                            enabled = !busy,
                            modifier = Modifier.testTag(PREVIEW_COPY_TAG),
                        ) {
                            Text(text = stringResource(R.string.action_copy))
                        }
                        TextButton(
                            onClick = onDownload,
                            enabled = !busy,
                            modifier = Modifier.testTag(PREVIEW_DOWNLOAD_TAG),
                        ) {
                            Text(text = stringResource(R.string.action_download))
                        }
                    } else {
                        TextButton(
                            onClick = onCopy,
                            enabled = !busy,
                            modifier = Modifier.testTag(PREVIEW_COPY_TAG),
                        ) {
                            Text(text = stringResource(R.string.action_copy))
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

/** Content of the preview of one item. */
@Composable
private fun PreviewBody(
    entry: Local.HistoryItem,
    thumbnail: HistoryThumbnailLoader,
) {
    val item = entry.item
    when (item.kind) {
        Local.Kind.KIND_IMAGE -> PreviewImage(entry = entry, thumbnail = thumbnail)
        Local.Kind.KIND_FILES -> PreviewFiles(entry = entry)
        else ->
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

/** Large thumbnail of the previewed image. */
@Composable
private fun PreviewImage(
    entry: Local.HistoryItem,
    thumbnail: HistoryThumbnailLoader,
) {
    var image by remember(entry.item.id) { mutableStateOf<ImageBitmap?>(null) }
    var loading by remember(entry.item.id) { mutableStateOf(entry.complete) }
    LaunchedEffect(entry.item.id) {
        if (entry.complete) {
            image = thumbnail(entry.item.id, HistoryThumbnails.PREVIEW_SIDE)
        }
        loading = false
    }

    Box(
        modifier =
            Modifier
                .fillMaxWidth()
                .height(PREVIEW_IMAGE_HEIGHT),
        contentAlignment = Alignment.Center,
    ) {
        val bitmap = image
        when {
            bitmap != null ->
                Image(
                    bitmap = bitmap,
                    contentDescription = null,
                    contentScale = ContentScale.Fit,
                    modifier = Modifier.fillMaxSize(),
                )
            loading -> CircularProgressIndicator(modifier = Modifier.size(28.dp), strokeWidth = 3.dp)
            else ->
                Text(
                    text = stringResource(R.string.dashboard_not_downloaded),
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
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
            Row(
                modifier = Modifier.padding(vertical = 2.dp),
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
                    text = sizeLabel(representation, units),
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
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

/** Names of the files of an entry, with the number that did not fit. */
@Composable
private fun fileTitle(names: FileNames): String {
    if (names.rest > 0) {
        return pluralStringResource(R.plurals.names_more, names.rest, names.shown, names.rest)
    }
    return names.shown.ifEmpty { stringResource(R.string.kind_files) }
}

/** Size of one payload of an entry. */
private fun sizeLabel(
    representation: Local.Representation,
    units: SizeUnits,
): String {
    val size =
        if (representation.size > 0) {
            representation.size
        } else {
            representation.inlineBytes.size().toLong()
        }
    return HistoryText.formatSize(size, units)
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

/** Tag of the tab of a dashboard kind. */
internal fun tabTag(tab: DashboardTab): String = "dashboard.tab.${tab.name.lowercase()}"

/** Tag of the tile or the row of an entry. */
internal fun itemTag(item: Local.ClipItem): String = "dashboard.item.${item.id}"

/** Tag of the spinner of the first load. */
internal const val LOADING_TAG = "dashboard.loading"

/** Tag of the body of the preview. */
internal const val PREVIEW_TAG = "dashboard.preview"

/** Tag of the copy action of the preview. */
internal const val PREVIEW_COPY_TAG = "dashboard.preview.copy"

/** Tag of the download action of the preview. */
internal const val PREVIEW_DOWNLOAD_TAG = "dashboard.preview.download"

/** Tag of the apply action of the preview. */
internal const val PREVIEW_APPLY_TAG = "dashboard.preview.apply"

/** Height of the thumbnail in the preview. */
private val PREVIEW_IMAGE_HEIGHT = 260.dp

/** Height above which the previewed text scrolls. */
private val PREVIEW_TEXT_HEIGHT = 320.dp

/** Length of the text in the title of the preview. */
private const val PREVIEW_TITLE_LENGTH = 60

/** Tabs of the dashboard in the order they are shown. */
internal enum class DashboardTab(
    @StringRes val label: Int,
    @StringRes val empty: Int,
) {
    IMAGES(R.string.history_filter_images, R.string.dashboard_empty_images),
    FILES(R.string.history_filter_files, R.string.dashboard_empty_files),
    TEXT(R.string.history_filter_text, R.string.dashboard_empty_text),
    ;

    /** Entries of this tab, newest first. */
    fun itemsOf(dashboard: DashboardState): List<Local.HistoryItem> =
        when (this) {
            IMAGES -> dashboard.images
            FILES -> dashboard.files
            TEXT -> dashboard.text
        }
}
