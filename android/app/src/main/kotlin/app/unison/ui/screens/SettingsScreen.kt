package app.unison.ui.screens

import androidx.activity.compose.BackHandler
import androidx.annotation.DrawableRes
import androidx.annotation.StringRes
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.MediumTopAppBar
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.input.nestedscroll.nestedScroll
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import app.unison.R
import app.unison.ui.ConfirmDialog
import app.unison.ui.ErrorCard
import app.unison.ui.GroupItem
import app.unison.ui.IconTile
import app.unison.ui.ItemGroup
import app.unison.ui.ScreenTitle
import app.unison.ui.SectionTitle
import app.unison.ui.SettingsCard
import app.unison.ui.pauseLabel
import app.unison.ui.screenColor
import app.unison.ui.state.CoreState

/**
 * Index of the settings with a row per topic. A row opens the page of its
 * topic; the back arrow and the system back return to the index.
 */
@Composable
fun SettingsScreen(
    state: CoreState,
    panel: SettingsPanel,
    modifier: Modifier = Modifier,
) {
    var page by rememberSaveable { mutableStateOf<SettingsPage?>(null) }
    var leaving by remember { mutableStateOf(false) }

    val current = page
    if (current == null) {
        SettingsIndex(
            state = state,
            panel = panel,
            onOpen = { page = it },
            modifier = modifier,
        )
    } else {
        BackHandler { page = null }
        SettingsPageFrame(page = current, onBack = { page = null }, modifier = modifier) {
            SettingsPageContent(
                page = current,
                state = state,
                panel = panel,
                onLeaveRequest = { leaving = true },
            )
        }
    }

    if (leaving) {
        ConfirmDialog(
            title = stringResource(R.string.settings_leave_title),
            body = stringResource(R.string.settings_leave_body),
            onConfirm = {
                leaving = false
                panel.device.onLeaveGroup()
            },
            onDismiss = { leaving = false },
        )
    }
}

@Composable
private fun SettingsPageContent(
    page: SettingsPage,
    state: CoreState,
    panel: SettingsPanel,
    onLeaveRequest: () -> Unit,
) {
    when (page) {
        SettingsPage.DEVICE -> {
            val device = panel.device
            SettingsCard {
                DeviceName(deviceName = device.deviceName, onDeviceNameChange = device.onDeviceNameChange)
            }
            SettingsCard {
                ServiceMode(operatingMode = device.operatingMode, onOperatingModeChange = device.onOperatingModeChange)
            }
            SettingsCard {
                DashboardSettings(limit = device.dashboardLimit, onLimitChange = device.onDashboardLimitChange)
            }
            SettingsCard {
                FriendsDiscoverySection(
                    search = state.status?.friendsDiscovery,
                    onRefresh = panel.onRefresh,
                    onChange = device.onFriendsDiscoveryChange,
                )
            }
            SettingsCard {
                Group(onLeave = onLeaveRequest)
            }
        }
        SettingsPage.CLIPBOARD -> {
            val clipboard = panel.clipboard
            SettingsCard {
                Pause(pause = state.status?.pause, onPause = clipboard.onPause)
            }
            SettingsCard {
                ClipboardConfirmation(
                    required = state.requiresConfirmation,
                    onChange = clipboard.onSelfConfirmationChange,
                )
            }
            SettingsCard {
                RemoteSettingsPermission(
                    allowed = state.allowsRemoteSettings,
                    onChange = clipboard.onAllowRemoteSettingsChange,
                )
            }
            SettingsCard {
                Links(enabled = clipboard.stripTracking, onChange = clipboard.onStripTrackingChange)
            }
        }
        SettingsPage.FILES -> {
            val files = panel.files
            SettingsCard {
                DownloadsFolderSection(
                    folder = files.downloadsFolder,
                    onPick = files.onDownloadsFolderPick,
                    onReset = files.onDownloadsFolderReset,
                )
            }
            SettingsCard {
                FileConfirmation(
                    required = state.requiresFileConfirmation,
                    onChange = panel.clipboard.onSelfConfirmationChange,
                )
            }
        }
        SettingsPage.HISTORY -> {
            SettingsCard {
                HistoryTtlSection(
                    ttl = panel.history.ttl,
                    onSave = panel.history.onTtlSave,
                    onReset = panel.history.onTtlReset,
                )
            }
        }
        SettingsPage.NOTIFICATIONS -> {
            val app = panel.app
            SettingsCard {
                NotificationCategories(settings = app.notifications, onChange = app.onNotificationsChange)
            }
            SettingsCard {
                PersistentStatus(settings = app.notifications, onChange = app.onNotificationsChange)
            }
            SettingsCard {
                SystemNotifications()
            }
        }
        SettingsPage.NETWORK -> {
            val network = panel.network
            SettingsCard {
                PolicySection(
                    policy = network.policy,
                    network = state.status?.network,
                    onPolicyChange = network.onPolicyChange,
                    onReset = network.onPolicyReset,
                )
            }
            SettingsCard {
                RelaySection(
                    relay = state.relay,
                    onSave = network.onRelaySave,
                    onOff = network.onRelayOff,
                    onRefresh = network.onRefreshRelay,
                    checking = state.relayChecking,
                    check = state.relayCheck,
                    onCheck = network.onRelayCheck,
                )
            }
        }
        SettingsPage.ABOUT -> {
            if (state.status != null) {
                SettingsCard {
                    About(state = state)
                }
            }
            SettingsCard {
                Diagnostics(enabled = panel.app.diagnosticsEnabled, onChange = panel.app.onDiagnosticsChange)
            }
        }
        SettingsPage.JOURNAL -> {
            SettingsCard {
                JournalSection(
                    enabled = panel.app.journalEnabled,
                    onEnabledChange = panel.app.onJournalChange,
                    state = state,
                    policy = panel.network.policy,
                )
            }
        }
        SettingsPage.SETTINGS_FILE -> {
            SettingsCard {
                SettingsFileSection(onExport = panel.app.onExportSettings, onImport = panel.app.onImportSettings)
            }
        }
        SettingsPage.UPDATES -> UpdatesSection(panel = panel.app.updates)
    }
}

/** The list of the topics, each with a short summary of its current values. */
@Composable
private fun SettingsIndex(
    state: CoreState,
    panel: SettingsPanel,
    onOpen: (SettingsPage) -> Unit,
    modifier: Modifier = Modifier,
) {
    val summaries =
        mapOf(
            SettingsPage.DEVICE to
                stringResource(
                    R.string.settings_summary_pair,
                    panel.device.deviceName,
                    stringResource(panel.device.operatingMode.label()),
                ),
            SettingsPage.CLIPBOARD to pauseLabel(state.status?.pause),
            SettingsPage.FILES to (panel.files.downloadsFolder ?: stringResource(R.string.settings_downloads_default)),
            SettingsPage.HISTORY to stringResource(R.string.settings_page_history_summary),
            SettingsPage.NOTIFICATIONS to stringResource(R.string.settings_page_notifications_summary),
            SettingsPage.NETWORK to stringResource(R.string.settings_page_network_summary),
            SettingsPage.UPDATES to updatesSummary(panel.app.updates.state),
            SettingsPage.ABOUT to (
                state.status?.let { stringResource(R.string.core_version, it.version) }
                    ?: stringResource(R.string.settings_page_about_summary)
            ),
            SettingsPage.JOURNAL to stringResource(R.string.settings_page_journal_summary),
            SettingsPage.SETTINGS_FILE to stringResource(R.string.settings_page_settings_file_summary),
        )

    Column(
        modifier =
            modifier
                .fillMaxSize()
                .verticalScroll(rememberScrollState())
                .padding(horizontal = 16.dp),
    ) {
        ScreenTitle(title = stringResource(R.string.nav_settings))

        state.failure?.let { failure ->
            ErrorCard(message = stringResource(failure.message), onRetry = panel.onRefresh)
            Spacer(modifier = Modifier.height(16.dp))
        }

        PageGroup(pages = MAIN_PAGES, summaries = summaries, onOpen = onOpen)
        Spacer(modifier = Modifier.height(16.dp))
        PageGroup(
            pages = SettingsPage.entries - MAIN_PAGES.toSet(),
            summaries = summaries,
            onOpen = onOpen,
        )
        Spacer(modifier = Modifier.height(24.dp))
    }
}

/** Rows of the index that open their pages. */
@Composable
private fun PageGroup(
    pages: List<SettingsPage>,
    summaries: Map<SettingsPage, String>,
    onOpen: (SettingsPage) -> Unit,
) {
    ItemGroup {
        pages.forEachIndexed { index, page ->
            GroupItem(
                index = index,
                count = pages.size,
                onClick = { onOpen(page) },
                modifier = Modifier.testTag(settingsPageTag(page)),
            ) {
                IconTile(
                    icon = page.icon,
                    container = MaterialTheme.colorScheme.secondaryContainer,
                    content = MaterialTheme.colorScheme.onSecondaryContainer,
                    size = 40.dp,
                )
                Column(modifier = Modifier.weight(1f)) {
                    Text(text = stringResource(page.title), style = MaterialTheme.typography.titleMedium)
                    summaries[page]?.let { summary ->
                        Text(
                            text = summary,
                            style = MaterialTheme.typography.bodyMedium,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                        )
                    }
                }
                Icon(
                    painter = painterResource(R.drawable.ic_chevron_right),
                    contentDescription = null,
                    tint = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }
}

/** A page of one topic under a top app bar that collapses while the page scrolls. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun SettingsPageFrame(
    page: SettingsPage,
    onBack: () -> Unit,
    modifier: Modifier = Modifier,
    content: @Composable ColumnScope.() -> Unit,
) {
    val scrollBehavior = TopAppBarDefaults.exitUntilCollapsedScrollBehavior()
    Column(
        modifier =
            modifier
                .fillMaxSize()
                .nestedScroll(scrollBehavior.nestedScrollConnection),
    ) {
        MediumTopAppBar(
            title = { Text(text = stringResource(page.title)) },
            navigationIcon = {
                IconButton(onClick = onBack) {
                    Icon(
                        painter = painterResource(R.drawable.ic_arrow_back),
                        contentDescription = stringResource(R.string.action_back),
                    )
                }
            },
            // The scaffold of the app already keeps the content clear of the status bar.
            windowInsets = WindowInsets(0, 0, 0, 0),
            colors =
                TopAppBarDefaults.topAppBarColors(
                    containerColor = screenColor,
                    scrolledContainerColor = MaterialTheme.colorScheme.surfaceContainerHigh,
                ),
            scrollBehavior = scrollBehavior,
        )
        Column(
            modifier =
                Modifier
                    .weight(1f)
                    .verticalScroll(rememberScrollState())
                    .padding(horizontal = 16.dp),
        ) {
            content()
            Spacer(modifier = Modifier.height(24.dp))
        }
    }
}

/** Confirmation flags of this device that the paired devices may change from their side. */
@Composable
private fun RemoteSettingsPermission(
    allowed: Boolean,
    onChange: (Boolean) -> Unit,
) {
    SectionTitle(text = stringResource(R.string.settings_section_remote))
    SettingsSwitch(
        title = stringResource(R.string.settings_allow_remote),
        hint = stringResource(R.string.settings_allow_remote_hint),
        checked = allowed,
        tag = ALLOW_REMOTE_TAG,
        onChange = onChange,
    )
}

/** A page of the settings, opened from the index. */
internal enum class SettingsPage(
    @StringRes val title: Int,
    @DrawableRes val icon: Int,
) {
    DEVICE(R.string.settings_page_device, R.drawable.ic_device_phone),
    CLIPBOARD(R.string.settings_page_clipboard, R.drawable.ic_copy),
    FILES(R.string.settings_page_files, R.drawable.ic_kind_file),
    HISTORY(R.string.settings_page_history, R.drawable.ic_nav_history),
    NOTIFICATIONS(R.string.settings_page_notifications, R.drawable.ic_nav_notifications),
    NETWORK(R.string.settings_page_network, R.drawable.ic_network),
    UPDATES(R.string.settings_page_updates, R.drawable.ic_update),
    ABOUT(R.string.settings_section_about, R.drawable.ic_info),
    JOURNAL(R.string.settings_page_journal, R.drawable.ic_log),
    SETTINGS_FILE(R.string.settings_page_settings_file, R.drawable.ic_kind_file),
}

/** Pages of the first group of the index; the rest sit in a group of their own. */
private val MAIN_PAGES =
    listOf(
        SettingsPage.DEVICE,
        SettingsPage.CLIPBOARD,
        SettingsPage.FILES,
        SettingsPage.HISTORY,
        SettingsPage.NOTIFICATIONS,
        SettingsPage.NETWORK,
    )

/** Tag of the row of the index that opens [page]. */
internal fun settingsPageTag(page: SettingsPage): String = "settings.page.${page.name.lowercase()}"

/** Tag of the switch that lets the paired devices change the confirmation flags. */
internal const val ALLOW_REMOTE_TAG = "settings.allow_remote"
