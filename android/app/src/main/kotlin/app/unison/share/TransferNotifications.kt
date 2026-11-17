package app.unison.share

import android.app.Notification
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import androidx.core.app.NotificationCompat
import app.unison.MainActivity
import app.unison.R
import app.unison.history.HistoryText
import app.unison.history.SizeUnits
import app.unison.service.NotificationChannels
import app.unison.service.UnisonService
import unison.local.v1.Local

/** Notifications about outgoing file transfers. */
object TransferNotifications {

    /** Identifier of the transfers notification, reused for the result. */
    const val ID = 200

    /** Progress of the running transfers with the cancel action. */
    fun progress(context: Context, transfers: List<Local.Transfer>): Notification {
        val builder = NotificationCompat.Builder(context, NotificationChannels.TRANSFERS)
            .setSmallIcon(R.drawable.ic_stat_unison)
            .setContentTitle(
                context.getString(R.string.transfer_progress_title, namesOf(context, TransferProgress.names(transfers))),
            )
            .setContentIntent(openApp(context))
            .addAction(0, context.getString(R.string.action_cancel), cancelTransfers(context))
            .setCategory(NotificationCompat.CATEGORY_PROGRESS)
            .setPriority(NotificationCompat.PRIORITY_LOW)
            .setOnlyAlertOnce(true)
            .setOngoing(true)
            .setShowWhen(false)

        val total = TransferProgress.totalBytes(transfers)
        if (total <= 0) {
            builder.setContentText(context.getString(R.string.transfer_preparing))
            builder.setProgress(0, 0, true)
            return builder.build()
        }
        val percent = TransferProgress.percent(transfers)
        val units = SizeUnits.of(context)
        builder.setContentText(
            context.getString(
                R.string.transfer_progress_text,
                percent,
                HistoryText.formatSize(TransferProgress.bytes(transfers), units),
                HistoryText.formatSize(total, units),
            ),
        )
        builder.setProgress(PROGRESS_MAX, percent * PROGRESS_MAX / 100, false)
        return builder.build()
    }

    /** Result of the transfers that just ended. */
    fun result(context: Context, text: CharSequence): Notification =
        NotificationCompat.Builder(context, NotificationChannels.TRANSFERS)
            .setSmallIcon(R.drawable.ic_stat_unison)
            .setContentTitle(context.getString(R.string.app_name))
            .setContentText(text)
            .setContentIntent(openApp(context))
            .setCategory(NotificationCompat.CATEGORY_STATUS)
            .setPriority(NotificationCompat.PRIORITY_LOW)
            .setAutoCancel(true)
            .setShowWhen(false)
            .build()

    /** Names of the peers of a progress notification. */
    private fun namesOf(context: Context, names: List<String>): String = when {
        names.isEmpty() -> context.getString(R.string.app_name)
        names.size == 1 -> names.first()
        else -> context.resources.getQuantityString(
            R.plurals.transfer_more_devices,
            names.size - 1,
            names.first(),
            names.size - 1,
        )
    }

    /** Stops the transfers from the notification action. */
    private fun cancelTransfers(context: Context): PendingIntent = PendingIntent.getService(
        context,
        REQUEST_CANCEL,
        Intent(context, UnisonService::class.java).setAction(UnisonService.ACTION_CANCEL_TRANSFERS),
        PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
    )

    /** Opens the app from the notification body. */
    private fun openApp(context: Context): PendingIntent = PendingIntent.getActivity(
        context,
        REQUEST_OPEN,
        Intent(context, MainActivity::class.java),
        PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
    )

    private const val PROGRESS_MAX = 100
    private const val REQUEST_CANCEL = 1
    private const val REQUEST_OPEN = 2
}
