package app.unison.service

import android.app.NotificationManager
import android.content.Context
import android.content.Intent
import android.provider.Settings
import androidx.core.app.NotificationManagerCompat

/** State of the notification settings of the system. */
object NotificationSystem {

    /** True when the app is allowed to show notifications. */
    fun enabled(context: Context): Boolean =
        NotificationManagerCompat.from(context).areNotificationsEnabled()

    /** True when the user turned the channel off in the system settings. */
    fun channelBlocked(context: Context, channelId: String): Boolean {
        val manager = context.getSystemService(NotificationManager::class.java) ?: return false
        return manager.getNotificationChannel(channelId)?.importance == NotificationManager.IMPORTANCE_NONE
    }

    /** Opens the notification settings of the app, or of one of its channels. */
    fun openSettings(context: Context, channelId: String? = null) {
        val intent = if (channelId == null) {
            Intent(Settings.ACTION_APP_NOTIFICATION_SETTINGS)
                .putExtra(Settings.EXTRA_APP_PACKAGE, context.packageName)
        } else {
            Intent(Settings.ACTION_CHANNEL_NOTIFICATION_SETTINGS)
                .putExtra(Settings.EXTRA_APP_PACKAGE, context.packageName)
                .putExtra(Settings.EXTRA_CHANNEL_ID, channelId)
        }
        context.startActivity(intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
    }
}
