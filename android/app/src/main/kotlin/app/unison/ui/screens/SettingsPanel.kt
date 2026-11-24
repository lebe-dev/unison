package app.unison.ui.screens

import android.net.Uri
import app.unison.clipboard.PauseRequest
import app.unison.history.HistoryTtl
import app.unison.policy.PolicyConfig
import app.unison.service.NotificationSettings
import app.unison.service.OperatingMode
import app.unison.ui.UpdatePanel
import unison.local.v1.Local

/** State and callbacks for the Device settings page. */
data class DeviceSettingsPanel(
    val deviceName: String,
    val operatingMode: OperatingMode,
    val dashboardLimit: Int,
    val onDeviceNameChange: (String) -> Unit = {},
    val onOperatingModeChange: (OperatingMode) -> Unit = {},
    val onDashboardLimitChange: (Int) -> Unit = {},
    val onFriendsDiscoveryChange: (Boolean) -> Unit = {},
    val onLeaveGroup: () -> Unit = {},
)

/** State and callbacks for the Clipboard settings page. */
data class ClipboardSettingsPanel(
    val stripTracking: Boolean,
    val onPause: (PauseRequest) -> Unit = {},
    val onSelfConfirmationChange: (Local.ConfirmationScope, Boolean) -> Unit = { _, _ -> },
    val onAllowRemoteSettingsChange: (Boolean) -> Unit = {},
    val onStripTrackingChange: (Boolean) -> Unit = {},
)

/** State and callbacks for the Files settings page. */
data class FilesSettingsPanel(
    val downloadsFolder: String? = null,
    val onDownloadsFolderPick: (Uri) -> Unit = {},
    val onDownloadsFolderReset: () -> Unit = {},
)

/** State and callbacks for the History settings page. */
data class HistorySettingsPanel(
    val ttl: HistoryTtl,
    val onTtlSave: (Local.Kind, Long) -> Unit = { _, _ -> },
    val onTtlReset: () -> Unit = {},
)

/** State and callbacks for the Network settings page. */
data class NetworkSettingsPanel(
    val policy: PolicyConfig,
    val onPolicyChange: (PolicyConfig) -> Unit = {},
    val onPolicyReset: () -> Unit = {},
    val onRelaySave: (url: String, token: String?, filesOnRequest: Boolean?) -> Unit = { _, _, _ -> },
    val onRelayOff: () -> Unit = {},
    val onRefreshRelay: () -> Unit = {},
    val onRelayCheck: (url: String, token: String?) -> Unit = { _, _ -> },
)

/** State and callbacks for notifications, diagnostics, journal, settings file and updates. */
data class AppSettingsPanel(
    val notifications: NotificationSettings,
    val onNotificationsChange: (NotificationSettings) -> Unit = {},
    val diagnosticsEnabled: Boolean = false,
    val onDiagnosticsChange: (Boolean) -> Unit = {},
    val journalEnabled: Boolean = false,
    val onJournalChange: (Boolean) -> Unit = {},
    val onExportSettings: (Uri) -> Unit = {},
    val onImportSettings: (Uri) -> Unit = {},
    val updates: UpdatePanel,
)

/** All state and callbacks rendered by [SettingsScreen], grouped by topic. */
data class SettingsPanel(
    val device: DeviceSettingsPanel,
    val clipboard: ClipboardSettingsPanel,
    val files: FilesSettingsPanel,
    val history: HistorySettingsPanel,
    val network: NetworkSettingsPanel,
    val app: AppSettingsPanel,
    val onRefresh: () -> Unit = {},
)
