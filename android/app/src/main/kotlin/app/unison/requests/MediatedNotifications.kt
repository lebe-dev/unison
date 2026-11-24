package app.unison.requests

import android.app.Notification
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import androidx.core.app.NotificationCompat
import app.unison.R
import app.unison.service.NotificationChannels
import app.unison.service.UnisonService
import unison.local.v1.Local

/** Notifications about introductions that another device offers. */
object MediatedNotifications {
    /** Identifier of the notification of one offer. */
    fun idOf(sessionId: String): Int = BASE_ID + (sessionId.hashCode() and ID_MASK)

    /** Offer to introduce this device to another one, with both answers. */
    fun offer(
        context: Context,
        pair: Local.MediatedPair,
    ): Notification {
        val peer = pair.peer
        val name = peer.name.ifBlank { peer.deviceId.take(SHORT_ID_LENGTH).ifEmpty { context.getString(R.string.app_name) } }
        val mediator = pair.mediator.name.ifBlank { pair.mediator.deviceId.take(SHORT_ID_LENGTH) }
        val details =
            buildList {
                add(context.getString(R.string.mediated_offer_from, mediator))
                if (pair.hasSas()) {
                    add(context.getString(R.string.mediated_notification_sas, pair.sas.digits))
                }
                if (pair.alreadyPaired) {
                    add(context.getString(R.string.mediated_offer_already_paired))
                }
            }.joinToString("\n")
        return NotificationCompat
            .Builder(context, NotificationChannels.REQUESTS)
            .setSmallIcon(R.drawable.ic_stat_unison)
            .setContentTitle(context.getString(R.string.mediated_offer_title, name))
            .setContentText(context.getString(R.string.mediated_offer_from, mediator))
            .setContentIntent(openRequestsApp(context))
            .addAction(0, context.getString(R.string.action_decline), answer(context, pair, accept = false))
            .addAction(0, context.getString(R.string.action_accept), answer(context, pair, accept = true))
            .setStyle(NotificationCompat.BigTextStyle().bigText(details))
            .setCategory(NotificationCompat.CATEGORY_MESSAGE)
            .setPriority(NotificationCompat.PRIORITY_HIGH)
            // The service posts the offer again on every refresh; only the first post alerts.
            .setOnlyAlertOnce(true)
            .setShowWhen(false)
            .build()
    }

    /** Sends the answer of an offer to the service. */
    private fun answer(
        context: Context,
        pair: Local.MediatedPair,
        accept: Boolean,
    ): PendingIntent {
        val action = if (accept) UnisonService.ACTION_ACCEPT_MEDIATED else UnisonService.ACTION_DECLINE_MEDIATED
        val code = idOf(pair.sessionId) + if (accept) 0 else REQUEST_CODE_STRIDE
        val intent =
            Intent(context, UnisonService::class.java)
                .setAction(action)
                .putExtra(UnisonService.EXTRA_SESSION_ID, pair.sessionId)
                .putExtra(UnisonService.EXTRA_ENABLE_FRIENDS_DISCOVERY, pair.role == Local.Role.ROLE_FRIENDS)
        return PendingIntent.getService(
            context,
            code,
            intent,
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
        )
    }

    private const val BASE_ID = 0x20000
    private const val ID_MASK = 0x7FFF

    /** Keeps the pending intents of the two answers apart. */
    private const val REQUEST_CODE_STRIDE = 0x8000

    /** Characters of a device ID shown when the name is empty. */
    private const val SHORT_ID_LENGTH = 8
}
