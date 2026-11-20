package app.unison.share

import android.app.DownloadManager
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
import app.unison.storage.DownloadsFolder
import unison.local.v1.Local

/** Notifications about the files peers send to this device. */
object ReceiveNotifications {
    /** Identifier of the notification of one incoming transfer, reused for its result. */
    fun idOf(transferId: String): Int = BASE_ID + (transferId.hashCode() and ID_MASK)

    /** Progress of one incoming transfer. */
    fun progress(
        context: Context,
        transfer: Local.Transfer,
    ): Notification {
        val builder =
            NotificationCompat
                .Builder(context, NotificationChannels.TRANSFERS)
                .setSmallIcon(R.drawable.ic_stat_unison)
                .setContentTitle(context.getString(R.string.receive_progress_title, senderOf(context, transfer)))
                .setContentIntent(openApp(context))
                .setCategory(NotificationCompat.CATEGORY_PROGRESS)
                .setPriority(NotificationCompat.PRIORITY_LOW)
                .setOnlyAlertOnce(true)
                .setOngoing(true)
                .setShowWhen(false)

        val total = transfer.totalBytes
        if (total <= 0) {
            builder.setContentText(filesOf(context, transfer))
            builder.setProgress(0, 0, true)
            return builder.build()
        }
        val percent = ((transfer.bytes * 100) / total).coerceIn(0, 100).toInt()
        val units = SizeUnits.of(context)
        val amount =
            context.getString(
                R.string.transfer_progress_text,
                percent,
                HistoryText.formatSize(transfer.bytes, units),
                HistoryText.formatSize(total, units),
            )
        builder.setContentText(context.getString(R.string.receive_progress_text, filesOf(context, transfer), amount))
        builder.setProgress(PROGRESS_MAX, percent * PROGRESS_MAX / 100, false)
        return builder.build()
    }

    /**
     * Result of an incoming transfer that ended. A received file alerts the user,
     * and a tap opens the folder with the saved files.
     */
    fun result(
        context: Context,
        transfer: Local.Transfer,
    ): Notification {
        val received = transfer.state == Local.TransferState.TRANSFER_STATE_DONE
        val text =
            when (transfer.state) {
                Local.TransferState.TRANSFER_STATE_DONE -> receivedText(context, transfer)
                Local.TransferState.TRANSFER_STATE_CANCELED -> context.getString(R.string.receive_canceled)
                else -> context.getString(R.string.receive_failed)
            }
        val channel = if (received) NotificationChannels.RECEIVED else NotificationChannels.TRANSFERS
        return NotificationCompat
            .Builder(context, channel)
            .setSmallIcon(R.drawable.ic_stat_unison)
            .setContentTitle(senderOf(context, transfer))
            .setContentText(text)
            .setContentIntent(if (received) openDownloads(context) else openApp(context))
            .setCategory(NotificationCompat.CATEGORY_STATUS)
            .setPriority(if (received) NotificationCompat.PRIORITY_DEFAULT else NotificationCompat.PRIORITY_LOW)
            .setAutoCancel(true)
            .build()
    }

    private fun receivedText(
        context: Context,
        transfer: Local.Transfer,
    ): String {
        val files = transfer.filesList
        if (files.size == 1) {
            return context.getString(R.string.receive_done, files.first().name)
        }
        return context.resources.getQuantityString(R.plurals.receive_done_files, files.size, files.size)
    }

    /** Name of the only file, or the number of files. */
    private fun filesOf(
        context: Context,
        transfer: Local.Transfer,
    ): String {
        val files = transfer.filesList
        if (files.size == 1) {
            return files.first().name
        }
        return context.resources.getQuantityString(R.plurals.receive_files, files.size, files.size)
    }

    private fun senderOf(
        context: Context,
        transfer: Local.Transfer,
    ): String =
        when {
            transfer.deviceName.isNotBlank() -> transfer.deviceName
            else -> context.getString(R.string.app_name)
        }

    /**
     * Opens the system list of downloads, where the files of the default folder
     * are. A folder the user picked has no such screen, so the app opens instead.
     */
    private fun openDownloads(context: Context): PendingIntent {
        val downloads = Intent(DownloadManager.ACTION_VIEW_DOWNLOADS).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        val usable =
            DownloadsFolder.current(context) == null &&
                downloads.resolveActivity(context.packageManager) != null
        if (!usable) {
            return openApp(context)
        }
        return PendingIntent.getActivity(
            context,
            REQUEST_DOWNLOADS,
            downloads,
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
        )
    }

    private fun openApp(context: Context): PendingIntent =
        PendingIntent.getActivity(
            context,
            REQUEST_OPEN,
            Intent(context, MainActivity::class.java),
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
        )

    private const val PROGRESS_MAX = 100
    private const val REQUEST_OPEN = 3
    private const val REQUEST_DOWNLOADS = 4
    private const val BASE_ID = 0x20000
    private const val ID_MASK = 0xFFFF
}
