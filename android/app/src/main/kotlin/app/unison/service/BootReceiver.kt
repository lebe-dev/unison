package app.unison.service

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.util.Log

/** Starts the service after boot when the operating mode asks for it. */
class BootReceiver : BroadcastReceiver() {
    override fun onReceive(
        context: Context,
        intent: Intent,
    ) {
        if (intent.action != Intent.ACTION_BOOT_COMPLETED) {
            return
        }
        // The service itself waits for Wi-Fi in the Wi-Fi only mode.
        if (UnisonSettings.operatingMode(context) == OperatingMode.APP_OPEN) {
            return
        }

        try {
            UnisonService.start(context)
        } catch (e: Exception) {
            Log.w("BootReceiver", "service start after boot failed: ${e.message}")
        }
    }
}
