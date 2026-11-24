package app.unison.updater

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.pm.PackageInstaller
import android.os.Build
import android.content.Intent.EXTRA_INTENT as EXTRA_CONFIRMATION

/** Receives the result of the install session the app started. */
class UpdateInstallReceiver : BroadcastReceiver() {
    override fun onReceive(
        context: Context,
        intent: Intent,
    ) {
        val status = intent.getIntExtra(PackageInstaller.EXTRA_STATUS, PackageInstaller.STATUS_FAILURE)
        val message = intent.getStringExtra(PackageInstaller.EXTRA_STATUS_MESSAGE)
        UpdateManager.onInstallStatus(
            context = context,
            status = status,
            message = message,
            confirmation = confirmationOf(intent),
        )
    }

    /** The system prompt that asks the user to finish the installation. */
    private fun confirmationOf(intent: Intent): Intent? =
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            intent.getParcelableExtra(EXTRA_CONFIRMATION, Intent::class.java)
        } else {
            @Suppress("DEPRECATION")
            intent.getParcelableExtra(EXTRA_CONFIRMATION)
        }
}
