package app.unison.network

import android.content.ActivityNotFoundException
import android.content.Context
import android.content.Intent
import android.net.ConnectivityManager
import android.net.Uri
import android.provider.Settings
import android.util.Log

/**
 * Reads the background data restriction of the app.
 *
 * Android reports one status for two different cases: the system data saver is
 * on, or the user turned off background data for this app alone. Only the
 * allowed status proves that the system data saver is on, so the restricted
 * status is not taken for it: the service runs in the foreground, where the
 * restriction does not apply, and the relay must stay usable.
 */
object BackgroundData {
    private const val TAG = "BackgroundData"

    /** The system data saver is on for sure. */
    fun dataSaver(status: Int): Boolean = status == ConnectivityManager.RESTRICT_BACKGROUND_STATUS_WHITELISTED

    /** Background data of the app is restricted on metered networks. */
    fun restricted(status: Int): Boolean = status == ConnectivityManager.RESTRICT_BACKGROUND_STATUS_ENABLED

    /** The restriction status of the app, disabled when it cannot be read. */
    fun status(context: Context): Int =
        context.getSystemService(ConnectivityManager::class.java)?.restrictBackgroundStatus
            ?: ConnectivityManager.RESTRICT_BACKGROUND_STATUS_DISABLED

    /**
     * Opens the system page where the user allows the background data of the
     * app, or the app details when the device has no such page.
     */
    fun openSettings(context: Context) {
        val uri = Uri.fromParts("package", context.packageName, null)
        val intents =
            listOf(
                Intent(Settings.ACTION_IGNORE_BACKGROUND_DATA_RESTRICTIONS_SETTINGS, uri),
                Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS, uri),
            )
        for (intent in intents) {
            try {
                context.startActivity(intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
                return
            } catch (e: ActivityNotFoundException) {
                Log.w(TAG, "settings page is not available: ${intent.action}")
            }
        }
    }
}
