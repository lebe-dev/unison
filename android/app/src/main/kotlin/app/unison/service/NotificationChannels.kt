package app.unison.service

import android.app.NotificationChannel
import android.app.NotificationManager
import android.content.Context
import app.unison.R

/** Notification channels of the app. */
object NotificationChannels {
    /** Progress of outgoing and incoming transfers. */
    const val TRANSFERS = "transfers"

    /** Files that arrived from a peer. The channel makes a sound, so a received
     * file does not stay unnoticed. */
    const val RECEIVED = "received"

    /** File offers that wait for a decision. */
    const val REQUESTS = "requests"

    /** Incoming clipboard items. */
    const val CLIPBOARD = "clipboard"

    /** Clipboard items that wait for the user to apply them. The channel pops up,
     * so a held item does not stay unnoticed in the shade. */
    const val CONFIRMATIONS = "confirmations"

    /** The persistent notification of the service. */
    const val SERVICE = "service"

    fun create(context: Context) {
        val manager = context.getSystemService(NotificationManager::class.java)

        manager.createNotificationChannel(
            NotificationChannel(
                SERVICE,
                context.getString(R.string.channel_service),
                NotificationManager.IMPORTANCE_MIN,
            ).apply { setShowBadge(false) },
        )

        manager.createNotificationChannel(
            NotificationChannel(
                TRANSFERS,
                context.getString(R.string.channel_transfers),
                NotificationManager.IMPORTANCE_LOW,
            ).apply { setShowBadge(false) },
        )

        manager.createNotificationChannel(
            NotificationChannel(
                RECEIVED,
                context.getString(R.string.channel_received),
                NotificationManager.IMPORTANCE_DEFAULT,
            ).apply { setShowBadge(false) },
        )

        manager.createNotificationChannel(
            NotificationChannel(
                REQUESTS,
                context.getString(R.string.channel_requests),
                NotificationManager.IMPORTANCE_HIGH,
            ),
        )

        manager.createNotificationChannel(
            NotificationChannel(
                CONFIRMATIONS,
                context.getString(R.string.channel_confirmations),
                NotificationManager.IMPORTANCE_HIGH,
            ),
        )

        manager.createNotificationChannel(
            NotificationChannel(
                CLIPBOARD,
                context.getString(R.string.channel_clipboard),
                NotificationManager.IMPORTANCE_LOW,
            ).apply {
                setShowBadge(false)
                setSound(null, null)
                enableVibration(false)
            },
        )
    }
}
