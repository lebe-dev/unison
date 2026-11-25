package app.unison.ui.screens

import androidx.annotation.StringRes
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.PrimaryTabRow
import androidx.compose.material3.Tab
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import app.unison.R
import app.unison.history.HistoryThumbnailLoader
import app.unison.ui.ConfirmDialog
import app.unison.ui.ErrorCard
import app.unison.ui.ScreenTitle
import app.unison.ui.UpdateBanner
import app.unison.ui.UpdatePanel
import app.unison.ui.state.DashboardState
import unison.local.v1.Local

/**
 * The first screen: the newest items of the history in three tabs. Images open
 * in a fullscreen viewer, files and text in a preview dialog. A long press
 * opens the menu with the actions of the item.
 */
@Composable
fun DashboardScreen(
    panel: DashboardPanel,
    held: Set<String>,
    busy: Set<String>,
    modifier: Modifier = Modifier,
) {
    val dashboard = panel.state
    var selected by rememberSaveable { mutableStateOf(DashboardTab.IMAGES) }
    var previewId by remember { mutableStateOf<String?>(null) }
    var viewerId by remember { mutableStateOf<String?>(null) }
    var deletingId by remember { mutableStateOf<String?>(null) }

    val deleting = deletingId?.let { dashboard.findById(it) }
    val viewer = viewerId?.let { dashboard.findById(it) }
    val preview = previewId?.let { dashboard.findById(it) }

    deleting?.let { entry ->
        ConfirmDialog(
            title = stringResource(R.string.dashboard_delete_title),
            body = stringResource(R.string.dashboard_delete_body),
            onConfirm = {
                deletingId = null
                previewId = null
                panel.onDelete(entry)
            },
            onDismiss = { deletingId = null },
            confirmLabel = R.string.dashboard_delete_confirm,
        )
    }

    viewer?.let { entry ->
        DashboardImageViewer(
            entry = entry,
            thumbnail = panel.thumbnail,
            busy = entry.item.id in busy,
            onDownload = { panel.onDownload(entry) },
            onClose = { viewerId = null },
        )
    }

    preview?.let { entry ->
        DashboardPreviewDialog(
            entry = entry,
            busy = entry.item.id in busy,
            held = entry.item.id in held,
            onClose = { previewId = null },
            onCopy = { panel.onCopy(entry) },
            onDownload = { panel.onDownload(entry) },
            onApply = { panel.onApply(entry) },
            onDelete = { deletingId = entry.item.id },
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

        DashboardTabContent(
            tab = selected,
            entries = selected.itemsOf(dashboard),
            panel = panel,
            onOpenViewer = { viewerId = it.item.id },
            onOpenPreview = { previewId = it.item.id },
            onRequestDelete = { deletingId = it.item.id },
        )
    }
}

@Composable
private fun DashboardTabContent(
    tab: DashboardTab,
    entries: List<Local.HistoryItem>,
    panel: DashboardPanel,
    onOpenViewer: (Local.HistoryItem) -> Unit,
    onOpenPreview: (Local.HistoryItem) -> Unit,
    onRequestDelete: (Local.HistoryItem) -> Unit,
) {
    if (entries.isEmpty()) {
        EmptyTab(tab = tab)
        return
    }
    val actions =
        DashboardItemActions(
            devices = panel.devices,
            onOpen = if (tab == DashboardTab.IMAGES) onOpenViewer else onOpenPreview,
            onCopy = panel.onCopy,
            onSend = panel.onSend,
            onDelete = onRequestDelete,
        )
    when (tab) {
        DashboardTab.IMAGES ->
            ImageGrid(
                entries = entries,
                thumbnail = panel.thumbnail,
                actions = actions,
            )
        DashboardTab.FILES ->
            DashboardEntryList(entries = entries) { entry, units, shape ->
                FileCard(entry = entry, units = units, shape = shape, actions = actions)
            }
        DashboardTab.TEXT ->
            DashboardEntryList(entries = entries) { entry, units, shape ->
                TextCard(entry = entry, units = units, shape = shape, actions = actions)
            }
    }
}

/** Callbacks shared across dashboard item tiles and cards. */
internal data class DashboardItemActions(
    val devices: List<Local.Device>,
    val onOpen: (Local.HistoryItem) -> Unit,
    val onCopy: (Local.HistoryItem) -> Unit,
    val onSend: (Local.HistoryItem, String) -> Unit,
    val onDelete: (Local.HistoryItem) -> Unit,
)

/** State and actions of the dashboard, kept together as one screen. */
data class DashboardPanel(
    val state: DashboardState,
    val thumbnail: HistoryThumbnailLoader,
    val devices: List<Local.Device> = emptyList(),
    val onRefresh: () -> Unit,
    val onCopy: (Local.HistoryItem) -> Unit,
    val onSend: (Local.HistoryItem, String) -> Unit = { _, _ -> },
    val onApply: (Local.HistoryItem) -> Unit,
    val onDownload: (Local.HistoryItem) -> Unit,
    val onDelete: (Local.HistoryItem) -> Unit,
    val updates: UpdatePanel? = null,
)

/** The three tabs of the dashboard. */
@Composable
private fun DashboardTabs(
    selected: DashboardTab,
    onSelect: (DashboardTab) -> Unit,
) {
    PrimaryTabRow(selectedTabIndex = selected.ordinal) {
        DashboardTab.entries.forEach { tab ->
            Tab(
                selected = selected == tab,
                onClick = { onSelect(tab) },
                modifier = Modifier.testTag(tabTag(tab)),
                text = { Text(text = stringResource(tab.label)) },
            )
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

private fun DashboardState.findById(id: String): Local.HistoryItem? =
    images.firstOrNull { it.item.id == id }
        ?: files.firstOrNull { it.item.id == id }
        ?: text.firstOrNull { it.item.id == id }

/** Tag of the tab of a dashboard kind. */
internal fun tabTag(tab: DashboardTab): String = "dashboard.tab.${tab.name.lowercase()}"

/** Tag of the spinner of the first load. */
internal const val LOADING_TAG = "dashboard.loading"

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
