package app.unison.share

import android.app.Notification
import android.content.Context
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import app.unison.R
import app.unison.history.HistoryText
import app.unison.history.SizeUnits
import app.unison.testContext
import app.unison.testQuantity
import app.unison.testString
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import unison.local.v1.Local

@RunWith(AndroidJUnit4::class)
class TransferNotificationsTest {
    private val context: Context get() = ApplicationProvider.getApplicationContext()

    @Test
    fun progressShowsThePercentAndTheCancelAction() {
        val notification =
            TransferNotifications.progress(
                context,
                listOf(transfer("Mac", bytes = 40, total = 100)),
            )

        assertEquals(
            testString(R.string.transfer_progress_title, "Mac"),
            notification.extras.getCharSequence(Notification.EXTRA_TITLE).toString(),
        )
        val units = SizeUnits.of(testContext)
        assertEquals(
            testString(
                R.string.transfer_progress_text,
                40,
                HistoryText.formatSize(40, units),
                HistoryText.formatSize(100, units),
            ),
            notification.extras.getCharSequence(Notification.EXTRA_TEXT).toString(),
        )
        assertEquals(40, notification.extras.getInt(Notification.EXTRA_PROGRESS))
        assertEquals(100, notification.extras.getInt(Notification.EXTRA_PROGRESS_MAX))
        assertEquals(1, notification.actions.size)
        assertEquals(testString(R.string.action_cancel), notification.actions[0].title.toString())
    }

    @Test
    fun progressWithoutSizeIsIndeterminate() {
        val notification =
            TransferNotifications.progress(
                context,
                listOf(transfer("Mac", bytes = 0, total = 0)),
            )

        assertTrue(notification.extras.getBoolean(Notification.EXTRA_PROGRESS_INDETERMINATE))
    }

    @Test
    fun severalPeersAreSummarized() {
        val notification =
            TransferNotifications.progress(
                context,
                listOf(
                    transfer("Laptop", bytes = 0, total = 10),
                    transfer("Mac", bytes = 0, total = 10),
                    transfer("Phone", bytes = 0, total = 10),
                ),
            )

        assertEquals(
            testString(
                R.string.transfer_progress_title,
                testQuantity(R.plurals.transfer_more_devices, 2, "Laptop", 2),
            ),
            notification.extras.getCharSequence(Notification.EXTRA_TITLE).toString(),
        )
    }

    @Test
    fun resultIsDismissedOnTap() {
        val notification = TransferNotifications.result(context, testString(R.string.transfer_sent, "Mac"))

        assertEquals(
            testString(R.string.transfer_sent, "Mac"),
            notification.extras.getCharSequence(Notification.EXTRA_TEXT).toString(),
        )
        assertTrue(notification.flags and Notification.FLAG_AUTO_CANCEL != 0)
        assertTrue(notification.actions.isNullOrEmpty())
        assertFalse(notification.flags and Notification.FLAG_ONGOING_EVENT != 0)
        assertNotNull(notification.contentIntent)
    }

    private fun transfer(
        name: String,
        bytes: Long,
        total: Long,
    ): Local.Transfer =
        Local.Transfer
            .newBuilder()
            .setId(name)
            .setDirection(Local.TransferDirection.TRANSFER_DIRECTION_OUTGOING)
            .setState(Local.TransferState.TRANSFER_STATE_TRANSFERRING)
            .setDeviceName(name)
            .setDeviceId("ab".repeat(32))
            .setBytes(bytes)
            .setTotalBytes(total)
            .build()
}
