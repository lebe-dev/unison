package app.unison.service

import android.app.Notification
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import androidx.core.app.NotificationCompat
import app.unison.MainActivity
import app.unison.R
import app.unison.clipboard.SendClipboardActivity

/** Builds the persistent notification of the service. */
object ServiceNotifications {

    /** Status notification of the running core with the send and stop actions. */
    fun status(context: Context, text: CharSequence): Notification {
        val open = PendingIntent.getActivity(
            context,
            0,
            Intent(context, MainActivity::class.java),
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
        )

        val send = PendingIntent.getActivity(
            context,
            1,
            Intent(context, SendClipboardActivity::class.java),
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
        )

        val stop = PendingIntent.getService(
            context,
            2,
            Intent(context, UnisonService::class.java).setAction(UnisonService.ACTION_STOP),
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
        )

        val dismissed = PendingIntent.getService(
            context,
            3,
            Intent(context, UnisonService::class.java).setAction(UnisonService.ACTION_STATUS_DISMISSED),
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
        )

        return NotificationCompat.Builder(context, NotificationChannels.SERVICE)
            .setSmallIcon(R.drawable.ic_stat_unison)
            .setContentTitle(context.getString(R.string.app_name))
            .setContentText(text)
            .setContentIntent(open)
            .addAction(0, context.getString(R.string.send_clipboard), send)
            .addAction(0, context.getString(R.string.action_stop), stop)
            .setDeleteIntent(dismissed)
            .setCategory(NotificationCompat.CATEGORY_SERVICE)
            .setPriority(NotificationCompat.PRIORITY_MIN)
            .setForegroundServiceBehavior(NotificationCompat.FOREGROUND_SERVICE_IMMEDIATE)
            .setOngoing(true)
            .setOnlyAlertOnce(true)
            .setSilent(true)
            .setShowWhen(false)
            .build()
    }
}
