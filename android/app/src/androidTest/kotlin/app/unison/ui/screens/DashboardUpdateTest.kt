package app.unison.ui.screens

import androidx.compose.foundation.layout.Column
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.assertIsOff
import androidx.compose.ui.test.assertIsOn
import androidx.compose.ui.test.junit4.v2.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.test.ext.junit.runners.AndroidJUnit4
import app.unison.R
import app.unison.testString
import app.unison.ui.UPDATE_BANNER_LATER_TAG
import app.unison.ui.UPDATE_BANNER_TAG
import app.unison.ui.UPDATE_BANNER_UPDATE_TAG
import app.unison.ui.UPDATE_DIALOG_INSTALL_TAG
import app.unison.ui.UPDATE_DIALOG_TAG
import app.unison.ui.UpdateDialog
import app.unison.ui.UpdatePanel
import app.unison.ui.state.DashboardState
import app.unison.ui.theme.UnisonTheme
import app.unison.updater.ReleaseInfo
import app.unison.updater.UpdateCheckInterval
import app.unison.updater.UpdateConfig
import app.unison.updater.UpdateStatus
import app.unison.updater.UpdateUiState
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class DashboardUpdateTest {
    @get:Rule
    val compose = createComposeRule()

    @Test
    fun bannerShowsTheUpdateAndLaterHidesIt() {
        var state by mutableStateOf(availableState())
        compose.setContent {
            UnisonTheme(dynamicColor = false) {
                val current = state
                val currentPanel =
                    panel(
                        state = current,
                        onDismissBanner = {
                            state = current.copy(config = current.config.copy(dismissedVersion = current.available?.version))
                        },
                    )
                DashboardScreen(
                    panel = dashboard(currentPanel),
                    held = emptySet(),
                    busy = emptySet(),
                )
            }
        }

        compose.onNodeWithTag(UPDATE_BANNER_TAG).assertIsDisplayed()
        compose.onNodeWithText(testString(R.string.updates_banner_title, "0.3.0")).assertIsDisplayed()

        compose.onNodeWithTag(UPDATE_BANNER_LATER_TAG).performClick()

        compose.onNodeWithTag(UPDATE_BANNER_TAG).assertDoesNotExist()
        assertEquals("0.3.0", state.config.dismissedVersion)
    }

    @Test
    fun theUpdateButtonOpensTheDialog() {
        var state by mutableStateOf(availableState())
        var installed = false
        compose.setContent {
            UnisonTheme(dynamicColor = false) {
                val current = state
                val panel =
                    panel(
                        state = current,
                        onOpenDialog = { state = current.copy(dialogOpen = true) },
                        onCloseDialog = { state = current.copy(dialogOpen = false) },
                        onInstall = { installed = true },
                    )
                Column {
                    DashboardScreen(
                        panel = dashboard(panel),
                        held = emptySet(),
                        busy = emptySet(),
                    )
                    UpdateDialog(panel = panel)
                }
            }
        }

        compose.onNodeWithTag(UPDATE_BANNER_UPDATE_TAG).performClick()

        compose.onNodeWithTag(UPDATE_DIALOG_TAG).assertIsDisplayed()
        compose.onNodeWithText(testString(R.string.updates_version_change, "0.2.0", "0.3.0")).assertIsDisplayed()
        compose.onNodeWithText("Release notes").assertIsDisplayed()

        compose.onNodeWithTag(UPDATE_DIALOG_INSTALL_TAG).performClick()
        assertTrue(installed)
    }

    @Test
    fun theSettingsPageKeepsTheChecksOffByDefault() {
        val panel = panel(UpdateUiState(currentVersion = "0.2.0"))
        compose.setContent {
            UnisonTheme(dynamicColor = false) {
                UpdatesSection(panel = panel)
            }
        }

        compose.onNodeWithTag(UPDATES_AUTO_TAG).assertIsOff()
        compose.onNodeWithTag(UPDATES_BETA_TAG).assertIsOff()
        compose.onNodeWithText(testString(R.string.updates_interval_daily)).assertIsDisplayed()
    }

    @Test
    fun theSettingsPageChangesTheIntervalAndTheChannel() {
        var selected: UpdateCheckInterval? = null
        var beta: Boolean? = null
        var checked = false
        val panel =
            panel(
                UpdateUiState(currentVersion = "0.2.0"),
                onSetInterval = { selected = it },
                onSetBetaChannel = { beta = it },
                onCheckNow = { checked = true },
            )
        compose.setContent {
            UnisonTheme(dynamicColor = false) {
                UpdatesSection(panel = panel)
            }
        }

        compose.onNodeWithTag(updatesIntervalTag(UpdateCheckInterval.WEEKLY)).performClick()
        compose.onNodeWithTag(UPDATES_BETA_TAG).performClick()
        compose.onNodeWithTag(UPDATES_CHECK_TAG).performClick()

        assertEquals(UpdateCheckInterval.WEEKLY, selected)
        assertEquals(true, beta)
        assertTrue(checked)
    }

    @Test
    fun theSettingsPageShowsTheEnabledSchedule() {
        val state =
            UpdateUiState(
                config = UpdateConfig(enabled = true, interval = UpdateCheckInterval.SIX_HOURS),
                currentVersion = "0.2.0",
            )
        compose.setContent {
            UnisonTheme(dynamicColor = false) {
                UpdatesSection(panel = panel(state))
            }
        }

        compose.onNodeWithTag(UPDATES_AUTO_TAG).assertIsOn()
        compose.onNodeWithText(testString(R.string.updates_interval_six_hours)).assertIsDisplayed()
    }

    private fun availableState(): UpdateUiState =
        UpdateUiState(
            config = UpdateConfig(),
            currentVersion = "0.2.0",
            status = UpdateStatus.Available,
            available = release(),
        )

    private fun panel(
        state: UpdateUiState,
        onOpenDialog: () -> Unit = {},
        onCloseDialog: () -> Unit = {},
        onDismissBanner: () -> Unit = {},
        onCheckNow: () -> Unit = {},
        onInstall: () -> Unit = {},
        onCancel: () -> Unit = {},
        onRequestInstallPermission: () -> Unit = {},
        onSetEnabled: (Boolean) -> Unit = {},
        onSetBetaChannel: (Boolean) -> Unit = {},
        onSetInterval: (UpdateCheckInterval) -> Unit = {},
    ): UpdatePanel =
        UpdatePanel(
            state = state,
            onOpenDialog = onOpenDialog,
            onCloseDialog = onCloseDialog,
            onDismissBanner = onDismissBanner,
            onCheckNow = onCheckNow,
            onInstall = onInstall,
            onCancel = onCancel,
            onRequestInstallPermission = onRequestInstallPermission,
            onSetEnabled = onSetEnabled,
            onSetBetaChannel = onSetBetaChannel,
            onSetInterval = onSetInterval,
        )

    private fun dashboard(panel: UpdatePanel): DashboardPanel =
        DashboardPanel(
            state = DashboardState(loaded = true),
            thumbnail = { _, _ -> null },
            onRefresh = {},
            onCopy = {},
            onApply = {},
            onDownload = {},
            onDelete = {},
            updates = panel,
        )

    private fun release(): ReleaseInfo =
        ReleaseInfo(
            version = "0.3.0",
            tagName = "0.3.0",
            name = "Unison 0.3.0",
            body = "# Release notes\n\n- A new thing",
            publishedAt = "2026-11-24T10:20:30Z",
            prerelease = false,
            htmlUrl = "https://github.com/lebe-dev/unison/releases/tag/0.3.0",
            assetName = "unison-v0.3.0-android.apk",
            assetUrl = "https://github.com/lebe-dev/unison/releases/download/0.3.0/unison-v0.3.0-android.apk",
            assetSize = 1024,
            checksumsUrl = "https://github.com/lebe-dev/unison/releases/download/0.3.0/SHA256SUMS",
            signatureUrl = "https://github.com/lebe-dev/unison/releases/download/0.3.0/SHA256SUMS.sig",
        )
}
