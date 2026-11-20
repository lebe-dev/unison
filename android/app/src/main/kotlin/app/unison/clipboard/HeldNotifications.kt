package app.unison.clipboard

import android.app.Notification
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import androidx.core.app.NotificationCompat
import app.unison.MainActivity
import app.unison.R
import app.unison.service.NotificationChannels
import app.unison.service.UnisonService
import app.unison.ui.Destination
import unison.local.v1.Local

/** Notifications about clipboard items that wait for the user to apply them. */
object HeldNotifications {
    /** Identifier of the result notification, reused for every apply. */
    const val RESULT_ID = 5

    /** Identifier of the notification of one held item. */
    fun idOf(itemId: String): Int = BASE_ID + (itemId.hashCode() and ID_MASK)

    /** Notification of an item with the apply action. It pops up like a request. */
    fun held(
        context: Context,
        item: Local.ClipItem,
        device: String,
    ): Notification {
        val intent =
            Intent(context, UnisonService::class.java)
                .setAction(UnisonService.ACTION_APPLY_ITEM)
                .putExtra(UnisonService.EXTRA_ITEM_ID, item.id)
        val apply =
            PendingIntent.getService(
                context,
                idOf(item.id),
                intent,
                PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
            )

        return NotificationCompat
            .Builder(context, NotificationChannels.CONFIRMATIONS)
            .setSmallIcon(R.drawable.ic_stat_unison)
            .setContentTitle(device.ifBlank { context.getString(R.string.app_name) })
            .setContentText(context.getString(R.string.clipboard_held_body))
            .setContentIntent(openHistory(context))
            .addAction(0, context.getString(R.string.action_apply), apply)
            .setCategory(NotificationCompat.CATEGORY_MESSAGE)
            .setPriority(NotificationCompat.PRIORITY_HIGH)
            .setAutoCancel(true)
            .setShowWhen(false)
            .build()
    }

    /** Outcome of an apply. */
    fun result(
        context: Context,
        applied: Boolean,
    ): Notification =
        NotificationCompat
            .Builder(context, NotificationChannels.CLIPBOARD)
            .setSmallIcon(R.drawable.ic_stat_unison)
            .setContentTitle(context.getString(R.string.app_name))
            .setContentText(
                context.getString(
                    if (applied) R.string.clipboard_applied else R.string.clipboard_apply_failed,
                ),
            ).setCategory(NotificationCompat.CATEGORY_STATUS)
            .setPriority(NotificationCompat.PRIORITY_LOW)
            .setAutoCancel(true)
            .setShowWhen(false)
            .build()

    /** Opens the history screen from the notification body. */
    private fun openHistory(context: Context): PendingIntent =
        PendingIntent.getActivity(
            context,
            0,
            Intent(context, MainActivity::class.java)
                .putExtra(MainActivity.EXTRA_SCREEN, Destination.SCREEN_HISTORY),
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
        )

    private const val BASE_ID = 400
    private const val ID_MASK = 0xFFFF
}
