package app.unison.requests

import android.app.Notification
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import androidx.core.app.NotificationCompat
import app.unison.MainActivity
import app.unison.R
import app.unison.history.HistoryText
import app.unison.service.NotificationChannels
import app.unison.service.UnisonService
import app.unison.ui.Destination
import unison.local.v1.Local

/** Notifications about file offers of Friends that wait for an answer. */
object RequestNotifications {

    /** Identifier of the result notification, reused for every answer. */
    const val RESULT_ID = 4

    /** Identifier of the notification of one offer. */
    fun idOf(requestId: String): Int = BASE_ID + (requestId.hashCode() and ID_MASK)

    /** Offer with the accept and decline actions. */
    fun request(context: Context, request: Local.ShareRequest): Notification {
        val files = request.filesList
        val size = HistoryText.formatSize(request.totalBytes)
        val names = files.map { it.name }.filter { it.isNotEmpty() }

        val builder = NotificationCompat.Builder(context, NotificationChannels.REQUESTS)
            .setSmallIcon(R.drawable.ic_stat_unison)
            .setContentTitle(request.deviceName.ifBlank { context.getString(R.string.app_name) })
            .setContentText(context.resources.getQuantityString(R.plurals.request_files, files.size, files.size, size))
            .setContentIntent(openApp(context))
            .addAction(0, context.getString(R.string.action_decline), answer(context, request.id, accept = false))
            .addAction(0, context.getString(R.string.action_accept), answer(context, request.id, accept = true))
            .setCategory(NotificationCompat.CATEGORY_MESSAGE)
            .setPriority(NotificationCompat.PRIORITY_HIGH)
            .setShowWhen(false)

        if (names.isNotEmpty()) {
            builder.setStyle(NotificationCompat.BigTextStyle().bigText(names.joinToString("\n")))
        }
        return builder.build()
    }

    /** Outcome of an answered offer. */
    fun result(context: Context, text: CharSequence): Notification =
        NotificationCompat.Builder(context, NotificationChannels.REQUESTS)
            .setSmallIcon(R.drawable.ic_stat_unison)
            .setContentTitle(context.getString(R.string.app_name))
            .setContentText(text)
            .setContentIntent(openApp(context))
            .setCategory(NotificationCompat.CATEGORY_STATUS)
            .setPriority(NotificationCompat.PRIORITY_LOW)
            .setAutoCancel(true)
            .setShowWhen(false)
            .build()

    /** Sends the answer of an offer to the service. */
    private fun answer(context: Context, requestId: String, accept: Boolean): PendingIntent {
        val action = if (accept) UnisonService.ACTION_ACCEPT_REQUEST else UnisonService.ACTION_DECLINE_REQUEST
        val code = idOf(requestId) + if (accept) 0 else REQUEST_CODE_STRIDE
        val intent = Intent(context, UnisonService::class.java)
            .setAction(action)
            .putExtra(UnisonService.EXTRA_REQUEST_ID, requestId)
        return PendingIntent.getService(
            context,
            code,
            intent,
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
        )
    }

    /** Opens the requests screen from the notification body. */
    private fun openApp(context: Context): PendingIntent = PendingIntent.getActivity(
        context,
        0,
        Intent(context, MainActivity::class.java)
            .putExtra(MainActivity.EXTRA_SCREEN, Destination.SCREEN_REQUESTS),
        PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
    )

    private const val BASE_ID = 300
    private const val ID_MASK = 0xFFFF

    /** Keeps the pending intents of the two answers apart. */
    private const val REQUEST_CODE_STRIDE = 0x10000
}
