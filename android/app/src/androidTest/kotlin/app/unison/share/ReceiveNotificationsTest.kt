package app.unison.share

import android.app.Notification
import android.content.Context
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import app.unison.R
import app.unison.history.HistoryText
import app.unison.history.SizeUnits
import app.unison.service.NotificationChannels
import app.unison.testContext
import app.unison.testQuantity
import app.unison.testString
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import unison.local.v1.Local

@RunWith(AndroidJUnit4::class)
class ReceiveNotificationsTest {

    private val context: Context get() = ApplicationProvider.getApplicationContext()

    @Test
    fun progressNamesTheSenderAndTheFile() {
        val notification = ReceiveNotifications.progress(
            context,
            transfer(Local.TransferState.TRANSFER_STATE_TRANSFERRING, files = listOf("report.pdf"), bytes = 40, total = 100),
        )

        assertEquals(
            testString(R.string.receive_progress_title, "Mac"),
            notification.extras.getCharSequence(Notification.EXTRA_TITLE).toString(),
        )
        val units = SizeUnits.of(testContext)
        assertEquals(
            testString(
                R.string.receive_progress_text,
                "report.pdf",
                testString(
                    R.string.transfer_progress_text,
                    40,
                    HistoryText.formatSize(40, units),
                    HistoryText.formatSize(100, units),
                ),
            ),
            notification.extras.getCharSequence(Notification.EXTRA_TEXT).toString(),
        )
        assertEquals(40, notification.extras.getInt(Notification.EXTRA_PROGRESS))
        assertTrue(notification.flags and Notification.FLAG_ONGOING_EVENT != 0)
        assertEquals(NotificationChannels.TRANSFERS, notification.channelId)
    }

    @Test
    fun resultNamesTheOnlyFile() {
        val notification = ReceiveNotifications.result(
            context,
            transfer(Local.TransferState.TRANSFER_STATE_DONE, files = listOf("report.pdf")),
        )

        assertEquals("Mac", notification.extras.getCharSequence(Notification.EXTRA_TITLE).toString())
        assertEquals(
            testString(R.string.receive_done, "report.pdf"),
            notification.extras.getCharSequence(Notification.EXTRA_TEXT).toString(),
        )
        assertEquals(NotificationChannels.RECEIVED, notification.channelId)
        assertTrue(notification.flags and Notification.FLAG_AUTO_CANCEL != 0)
        assertFalse(notification.flags and Notification.FLAG_ONGOING_EVENT != 0)
        assertNotNull(notification.contentIntent)
    }

    @Test
    fun resultCountsSeveralFiles() {
        val notification = ReceiveNotifications.result(
            context,
            transfer(Local.TransferState.TRANSFER_STATE_DONE, files = listOf("a.jpg", "b.jpg", "c.jpg")),
        )

        assertEquals(
            testQuantity(R.plurals.receive_done_files, 3, 3),
            notification.extras.getCharSequence(Notification.EXTRA_TEXT).toString(),
        )
    }

    @Test
    fun failedResultSaysSo() {
        val notification = ReceiveNotifications.result(
            context,
            transfer(Local.TransferState.TRANSFER_STATE_FAILED, files = listOf("a.jpg")),
        )

        assertEquals(
            testString(R.string.receive_failed),
            notification.extras.getCharSequence(Notification.EXTRA_TEXT).toString(),
        )
    }

    @Test
    fun everyTransferHasItsOwnNotification() {
        assertNotEquals(ReceiveNotifications.idOf("a"), ReceiveNotifications.idOf("b"))
        assertEquals(ReceiveNotifications.idOf("a"), ReceiveNotifications.idOf("a"))
    }

    private fun transfer(
        state: Local.TransferState,
        files: List<String>,
        bytes: Long = 0,
        total: Long = 0,
    ): Local.Transfer = Local.Transfer.newBuilder()
        .setId("transfer-1")
        .setDirection(Local.TransferDirection.TRANSFER_DIRECTION_INCOMING)
        .setState(state)
        .setDeviceName("Mac")
        .setDeviceId("ab".repeat(32))
        .setBytes(bytes)
        .setTotalBytes(total)
        .addAllFiles(files.map { Local.TransferFile.newBuilder().setName(it).build() })
        .build()
}
