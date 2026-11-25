package app.unison.ui

import app.unison.updater.UpdateCheckInterval
import app.unison.updater.UpdateUiState

/**
 * The update state with every action the screens need, kept in one object so a
 * banner, a dialog and a settings page take one parameter each.
 */
data class UpdatePanel(
    val state: UpdateUiState,
    val onOpenDialog: () -> Unit,
    val onCloseDialog: () -> Unit,
    val onDismissBanner: () -> Unit,
    val onCheckNow: () -> Unit,
    val onInstall: () -> Unit,
    val onCancel: () -> Unit,
    val onRequestInstallPermission: () -> Unit,
    val onSetEnabled: (Boolean) -> Unit,
    val onSetBetaChannel: (Boolean) -> Unit,
    val onSetInterval: (UpdateCheckInterval) -> Unit,
)
