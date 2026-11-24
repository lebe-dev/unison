package app.unison.ui.screens

import android.net.Uri
import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.assertIsNotEnabled
import androidx.compose.ui.test.assertIsOff
import androidx.compose.ui.test.assertIsOn
import androidx.compose.ui.test.junit4.v2.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollTo
import androidx.compose.ui.test.performTextReplacement
import androidx.test.ext.junit.runners.AndroidJUnit4
import app.unison.R
import app.unison.clipboard.PauseRequest
import app.unison.history.HistoryTtl
import app.unison.history.HistoryTtlDefaults
import app.unison.policy.PolicyDefaults
import app.unison.service.NotificationSettings
import app.unison.service.OperatingMode
import app.unison.testString
import app.unison.ui.state.CoreState
import app.unison.ui.theme.UnisonTheme
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import unison.local.v1.Local

@RunWith(AndroidJUnit4::class)
class SettingsScreenTest {
    @get:Rule
    val compose = createComposeRule()

    @Test
    fun indexListsEveryPage() {
        setContent()

        SettingsPage.entries.forEach { page ->
            compose.onNodeWithTag(settingsPageTag(page)).performScrollTo().assertIsDisplayed()
        }
        compose.onNodeWithTag(NAME_SAVE_TAG).assertDoesNotExist()
    }

    @Test
    fun indexSummarizesTheCurrentValues() {
        setContent(deviceName = "Pixel", downloadsFolder = "Documents/Inbox")

        compose.onNodeWithText("Pixel", substring = true).assertIsDisplayed()
        compose.onNodeWithText("Documents/Inbox").performScrollTo().assertIsDisplayed()
    }

    @Test
    fun pageOpensAndGoesBackToTheIndex() {
        setContent()

        open(SettingsPage.NETWORK)
        compose.onNodeWithText(testString(R.string.policy_section)).assertIsDisplayed()
        compose.onNodeWithTag(settingsPageTag(SettingsPage.DEVICE)).assertDoesNotExist()

        goBack()
        compose.onNodeWithTag(settingsPageTag(SettingsPage.DEVICE)).assertIsDisplayed()
    }

    @Test
    fun settingsFilePageOffersExportAndImport() {
        setContent()

        open(SettingsPage.SETTINGS_FILE)

        compose.onNodeWithTag(SETTINGS_FILE_EXPORT_TAG).assertIsDisplayed()
        compose.onNodeWithTag(SETTINGS_FILE_IMPORT_TAG).assertIsDisplayed()
        compose.onNodeWithText(testString(R.string.settings_file_hint)).assertIsDisplayed()
    }

    @Test
    fun aboutPageShowsTheVersion() {
        setContent()

        open(SettingsPage.ABOUT)

        compose.onNodeWithText(testString(R.string.core_version, "0.1.0")).assertIsDisplayed()
    }

    @Test
    fun serviceModeIsSelected() {
        var selected: OperatingMode? = null
        setContent(operatingMode = OperatingMode.ALWAYS, onOperatingModeChange = { selected = it })
        open(SettingsPage.DEVICE)

        compose.onNodeWithText(testString(R.string.mode_wifi_only)).performClick()

        assertEquals(OperatingMode.WIFI_ONLY, selected)
    }

    @Test
    fun dashboardLimitIsSelected() {
        var limit: Int? = null
        setContent(dashboardLimit = 12, onDashboardLimitChange = { limit = it })
        open(SettingsPage.DEVICE)

        compose.onNodeWithTag(dashboardLimitTag(48)).performScrollTo().performClick()

        assertEquals(48, limit)
    }

    @Test
    fun deviceNameIsSaved() {
        var saved: String? = null
        setContent(deviceName = "Pixel", onDeviceNameChange = { saved = it })
        open(SettingsPage.DEVICE)

        compose.onNodeWithText(testString(R.string.settings_name_hint)).performTextReplacement("Desk")
        compose.onNodeWithTag(NAME_SAVE_TAG).performClick()

        assertEquals("Desk", saved)
    }

    @Test
    fun saveIsDisabledWithoutChanges() {
        var saved: String? = null
        setContent(deviceName = "Pixel", onDeviceNameChange = { saved = it })
        open(SettingsPage.DEVICE)

        compose.onNodeWithTag(NAME_SAVE_TAG).assertIsNotEnabled().performClick()

        assertEquals(null, saved)
    }

    @Test
    fun pauseOptionsAreOffered() {
        var request: PauseRequest? = null
        setContent(operatingMode = OperatingMode.ALWAYS, onPause = { request = it })
        open(SettingsPage.CLIPBOARD)

        compose.onNodeWithText(testString(R.string.pause_five_minutes)).performScrollTo().performClick()

        assertEquals(Local.PauseMode.PAUSE_MODE_UNTIL, request?.mode)
    }

    @Test
    fun pausedClipboardCanBeResumed() {
        var request: PauseRequest? = null
        setContent(
            status =
                Local.GetStatusResponse
                    .newBuilder()
                    .setDeviceName("Pixel")
                    .setPause(
                        Local.Pause
                            .newBuilder()
                            .setPaused(true)
                            .setIndefinite(true)
                            .build(),
                    ).build(),
            onPause = { request = it },
        )
        open(SettingsPage.CLIPBOARD)

        compose.onAllNodesWithText(testString(R.string.pause_indefinite)).assertCountEquals(2)
        compose.onNodeWithText(testString(R.string.pause_resume)).performScrollTo().performClick()

        assertEquals(Local.PauseMode.PAUSE_MODE_OFF, request?.mode)
    }

    @Test
    fun selfConfirmationIsOffered() {
        var changed: Pair<Local.ConfirmationScope, Boolean>? = null
        setContent(onSelfConfirmationChange = { scope, require -> changed = scope to require })

        open(SettingsPage.CLIPBOARD)
        compose.onNodeWithTag(CONFIRMATION_TAG).performScrollTo().performClick()
        assertEquals(Local.ConfirmationScope.CONFIRMATION_SCOPE_CLIPBOARD to true, changed)

        goBack()
        open(SettingsPage.FILES)
        compose.onNodeWithTag(FILE_CONFIRMATION_TAG).performScrollTo().performClick()
        assertEquals(Local.ConfirmationScope.CONFIRMATION_SCOPE_FILES to true, changed)
    }

    @Test
    fun remoteSettingsPermissionIsOffered() {
        var allowed: Boolean? = null
        setContent(
            settings =
                Local.Settings
                    .newBuilder()
                    .setAllowRemoteSettings(true)
                    .build(),
            onAllowRemoteSettingsChange = { allowed = it },
        )

        open(SettingsPage.CLIPBOARD)
        compose
            .onNodeWithTag(ALLOW_REMOTE_TAG)
            .performScrollTo()
            .assertIsOn()
            .performClick()

        assertEquals(false, allowed)
    }

    @Test
    fun remoteSettingsPermissionIsOffWhenTheCoreSaysSo() {
        setContent(
            settings =
                Local.Settings
                    .newBuilder()
                    .setAllowRemoteSettings(false)
                    .build(),
        )

        open(SettingsPage.CLIPBOARD)

        compose.onNodeWithTag(ALLOW_REMOTE_TAG).performScrollTo().assertIsOff()
    }

    @Test
    fun diagnosticsReportingIsOffered() {
        var enabled: Boolean? = null
        setContent(onDiagnosticsChange = { enabled = it })
        open(SettingsPage.ABOUT)

        compose.onNodeWithTag(DIAGNOSTICS_TAG).performScrollTo().performClick()

        assertEquals(true, enabled)
    }

    @Test
    fun notificationCategoriesCanBeTurnedOff() {
        var saved: NotificationSettings? = null
        setContent(onNotificationsChange = { saved = it })
        open(SettingsPage.NOTIFICATIONS)

        compose.onNodeWithTag(TRANSFERS_TAG).performScrollTo().performClick()
        assertEquals(false, saved?.transfers)

        compose.onNodeWithTag(CONFIRMATIONS_TAG).performScrollTo().performClick()
        assertEquals(false, saved?.confirmations)
    }

    @Test
    fun dismissedStatusCanStayHidden() {
        var saved: NotificationSettings? = null
        setContent(onNotificationsChange = { saved = it })
        open(SettingsPage.NOTIFICATIONS)

        compose.onNodeWithTag(HIDE_STATUS_TAG).performScrollTo().performClick()

        assertEquals(false, saved?.hideStatusAfterDismissal)
    }

    @Test
    fun statusDetailsCanBeHidden() {
        var saved: NotificationSettings? = null
        setContent(onNotificationsChange = { saved = it })
        open(SettingsPage.NOTIFICATIONS)

        compose.onNodeWithTag(STATUS_DETAILS_TAG).performScrollTo().performClick()

        assertEquals(false, saved?.statusDetails)
    }

    @Test
    fun trackingParametersCanBeKept() {
        var enabled: Boolean? = null
        setContent(stripTracking = true, onStripTrackingChange = { enabled = it })
        open(SettingsPage.CLIPBOARD)

        compose.onNodeWithTag(STRIP_TRACKING_TAG).performScrollTo().performClick()

        assertEquals(false, enabled)
    }

    @Test
    fun historyLifetimeIsSaved() {
        var saved: Pair<Local.Kind, Long>? = null
        setContent(onTtlSave = { kind, seconds -> saved = kind to seconds })
        open(SettingsPage.HISTORY)

        compose.onNodeWithTag(ttlTag(Local.Kind.KIND_TEXT)).performTextReplacement("30d")
        compose.onNodeWithTag(ttlSaveTag(Local.Kind.KIND_TEXT)).performScrollTo().performClick()

        assertEquals(Local.Kind.KIND_TEXT to 30L * 24 * 60 * 60, saved)
    }

    @Test
    fun unusableHistoryLifetimeIsRefused() {
        var saved: Pair<Local.Kind, Long>? = null
        setContent(onTtlSave = { kind, seconds -> saved = kind to seconds })
        open(SettingsPage.HISTORY)

        compose.onNodeWithTag(ttlTag(Local.Kind.KIND_IMAGE)).performTextReplacement("soon")
        compose.onNodeWithTag(ttlSaveTag(Local.Kind.KIND_IMAGE)).performScrollTo().performClick()

        compose
            .onNodeWithText(testString(R.string.history_ttl_invalid))
            .performScrollTo()
            .assertIsDisplayed()
        assertEquals(null, saved)
    }

    @Test
    fun historyLifetimesCanBeReset() {
        var reset = false
        setContent(onTtlReset = { reset = true })
        open(SettingsPage.HISTORY)

        compose.onNodeWithText(testString(R.string.history_ttl_reset)).performScrollTo().performClick()

        assertTrue(reset)
    }

    @Test
    fun defaultDownloadsFolderIsNamed() {
        setContent(downloadsFolder = null)
        open(SettingsPage.FILES)

        compose.onNodeWithText(testString(R.string.settings_downloads_default)).performScrollTo().assertIsDisplayed()
        compose.onNodeWithTag(DOWNLOADS_RESET_TAG).assertIsNotEnabled()
    }

    @Test
    fun chosenDownloadsFolderIsShownAndCanBeReset() {
        var reset = false
        setContent(downloadsFolder = "Documents/Inbox", onDownloadsFolderReset = { reset = true })
        open(SettingsPage.FILES)

        compose.onNodeWithText("Documents/Inbox").performScrollTo().assertIsDisplayed()
        compose.onNodeWithTag(DOWNLOADS_RESET_TAG).performScrollTo().performClick()

        assertTrue(reset)
    }

    @Test
    fun leavingTheGroupAsksForConfirmation() {
        var left = false
        setContent(onLeaveGroup = { left = true })
        open(SettingsPage.DEVICE)

        compose.onNodeWithText(testString(R.string.settings_leave_group)).performScrollTo().performClick()
        compose.onNodeWithText(testString(R.string.settings_leave_title)).assertIsDisplayed()
        assertFalse(left)

        compose.onNodeWithText(testString(R.string.action_remove)).performClick()

        assertTrue(left)
    }

    private fun open(page: SettingsPage) {
        compose.onNodeWithTag(settingsPageTag(page)).performScrollTo().performClick()
    }

    private fun goBack() {
        compose.onNodeWithContentDescription(testString(R.string.action_back)).performClick()
    }

    private fun setContent(
        deviceName: String = "Pixel",
        operatingMode: OperatingMode = OperatingMode.ALWAYS,
        status: Local.GetStatusResponse =
            Local.GetStatusResponse
                .newBuilder()
                .setDeviceName(deviceName)
                .setVersion("0.1.0")
                .build(),
        settings: Local.Settings = Local.Settings.getDefaultInstance(),
        ttl: HistoryTtl = HistoryTtlDefaults.values,
        dashboardLimit: Int = app.unison.ui.state.DashboardLimit.DEFAULT,
        onDeviceNameChange: (String) -> Unit = {},
        onOperatingModeChange: (OperatingMode) -> Unit = {},
        onDashboardLimitChange: (Int) -> Unit = {},
        onPause: (PauseRequest) -> Unit = {},
        onRelaySave: (url: String, token: String?, filesOnRequest: Boolean?) -> Unit = { _, _, _ -> },
        onRelayOff: () -> Unit = {},
        onRefreshRelay: () -> Unit = {},
        onTtlSave: (Local.Kind, Long) -> Unit = { _, _ -> },
        onTtlReset: () -> Unit = {},
        downloadsFolder: String? = null,
        onDownloadsFolderReset: () -> Unit = {},
        onSelfConfirmationChange: (Local.ConfirmationScope, Boolean) -> Unit = { _, _ -> },
        onAllowRemoteSettingsChange: (Boolean) -> Unit = {},
        onLeaveGroup: () -> Unit = {},
        diagnosticsEnabled: Boolean = false,
        onDiagnosticsChange: (Boolean) -> Unit = {},
        notifications: NotificationSettings = NotificationSettings(),
        onNotificationsChange: (NotificationSettings) -> Unit = {},
        journalEnabled: Boolean = false,
        onJournalChange: (Boolean) -> Unit = {},
        onExportSettings: (Uri) -> Unit = {},
        onImportSettings: (Uri) -> Unit = {},
        stripTracking: Boolean = true,
        onStripTrackingChange: (Boolean) -> Unit = {},
    ) {
        compose.setContent {
            UnisonTheme(dynamicColor = false) {
                SettingsScreen(
                    state = CoreState(loading = false, status = status, settings = settings),
                    deviceName = deviceName,
                    operatingMode = operatingMode,
                    policy = PolicyDefaults.config,
                    ttl = ttl,
                    dashboardLimit = dashboardLimit,
                    onDeviceNameChange = onDeviceNameChange,
                    onOperatingModeChange = onOperatingModeChange,
                    onDashboardLimitChange = onDashboardLimitChange,
                    onPolicyChange = {},
                    onPolicyReset = {},
                    onTtlSave = onTtlSave,
                    onTtlReset = onTtlReset,
                    downloadsFolder = downloadsFolder,
                    onDownloadsFolderPick = {},
                    onDownloadsFolderReset = onDownloadsFolderReset,
                    onPause = onPause,
                    onRelaySave = onRelaySave,
                    onRelayOff = onRelayOff,
                    onRefreshRelay = onRefreshRelay,
                    onRelayCheck = { _, _ -> },
                    onSelfConfirmationChange = onSelfConfirmationChange,
                    onAllowRemoteSettingsChange = onAllowRemoteSettingsChange,
                    onLeaveGroup = onLeaveGroup,
                    diagnosticsEnabled = diagnosticsEnabled,
                    onDiagnosticsChange = onDiagnosticsChange,
                    notifications = notifications,
                    onNotificationsChange = onNotificationsChange,
                    journalEnabled = journalEnabled,
                    onJournalChange = onJournalChange,
                    onExportSettings = onExportSettings,
                    onImportSettings = onImportSettings,
                    stripTracking = stripTracking,
                    onStripTrackingChange = onStripTrackingChange,
                    onRefresh = {},
                )
            }
        }
    }
}
