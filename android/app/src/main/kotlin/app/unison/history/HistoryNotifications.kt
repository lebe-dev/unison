package app.unison.history

import android.app.Notification
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import androidx.core.app.NotificationCompat
import app.unison.MainActivity
import app.unison.R
import app.unison.service.NotificationChannels
import app.unison.service.UnisonService
import unison.local.v1.Local

/** Notifications about history entries that are not on the device yet. */
object HistoryNotifications {
    /** Identifier of the notification of one pending entry. */
    fun idOf(itemId: String): Int = BASE_ID + (itemId.hashCode() and ID_MASK)

    /** Notification of an entry that waits for the user to download it. */
    fun pending(
        context: Context,
        item: Local.ClipItem,
        device: String,
    ): Notification {
        val download =
            PendingIntent.getService(
                context,
                idOf(item.id),
                Intent(context, UnisonService::class.java)
                    .setAction(UnisonService.ACTION_DOWNLOAD_ITEM)
                    .putExtra(UnisonService.EXTRA_ITEM_ID, item.id),
                PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
            )

        return NotificationCompat
            .Builder(context, NotificationChannels.CLIPBOARD)
            .setSmallIcon(R.drawable.ic_stat_unison)
            .setContentTitle(device.ifBlank { context.getString(R.string.app_name) })
            .setContentText(context.getString(R.string.history_notification_body, kindLabel(context, item.kind)))
            .setContentIntent(openApp(context))
            .addAction(0, context.getString(R.string.action_download), download)
            .setCategory(NotificationCompat.CATEGORY_STATUS)
            .setPriority(NotificationCompat.PRIORITY_LOW)
            .setAutoCancel(true)
            .setShowWhen(false)
            .build()
    }

    /** Notification with the result of a download. */
    fun result(
        context: Context,
        saved: Boolean,
    ): Notification =
        NotificationCompat
            .Builder(context, NotificationChannels.CLIPBOARD)
            .setSmallIcon(R.drawable.ic_stat_unison)
            .setContentTitle(context.getString(R.string.app_name))
            .setContentText(
                context.getString(
                    if (saved) R.string.history_saved_notification else R.string.history_failed_notification,
                ),
            ).setCategory(NotificationCompat.CATEGORY_STATUS)
            .setPriority(NotificationCompat.PRIORITY_LOW)
            .setAutoCancel(true)
            .setShowWhen(false)
            .build()

    /** Label of the content kind of an entry. */
    private fun kindLabel(
        context: Context,
        kind: Local.Kind,
    ): String =
        context.getString(
            when (kind) {
                Local.Kind.KIND_IMAGE -> R.string.kind_image
                Local.Kind.KIND_FILES -> R.string.kind_files
                else -> R.string.kind_text
            },
        )

    /** Opens the app from the notification body. */
    private fun openApp(context: Context): PendingIntent =
        PendingIntent.getActivity(
            context,
            0,
            Intent(context, MainActivity::class.java),
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
        )

    /** Identifier of the download result notification. */
    const val RESULT_ID = 2

    private const val BASE_ID = 100
    private const val ID_MASK = 0xFFFF
}
