package app.unison.share

import android.app.Notification
import android.content.Context
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
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
        val notification = TransferNotifications.progress(
            context,
            listOf(transfer("Mac", bytes = 40, total = 100)),
        )

        assertEquals("Sending to Mac", notification.extras.getCharSequence(Notification.EXTRA_TITLE).toString())
        assertEquals("40% - 40 B of 100 B", notification.extras.getCharSequence(Notification.EXTRA_TEXT).toString())
        assertEquals(40, notification.extras.getInt(Notification.EXTRA_PROGRESS))
        assertEquals(100, notification.extras.getInt(Notification.EXTRA_PROGRESS_MAX))
        assertEquals(1, notification.actions.size)
        assertEquals("Cancel", notification.actions[0].title.toString())
    }

    @Test
    fun progressWithoutSizeIsIndeterminate() {
        val notification = TransferNotifications.progress(
            context,
            listOf(transfer("Mac", bytes = 0, total = 0)),
        )

        assertTrue(notification.extras.getBoolean(Notification.EXTRA_PROGRESS_INDETERMINATE))
    }

    @Test
    fun severalPeersAreSummarized() {
        val notification = TransferNotifications.progress(
            context,
            listOf(
                transfer("Laptop", bytes = 0, total = 10),
                transfer("Mac", bytes = 0, total = 10),
                transfer("Phone", bytes = 0, total = 10),
            ),
        )

        assertEquals("Sending to Laptop and 2 more", notification.extras.getCharSequence(Notification.EXTRA_TITLE).toString())
    }

    @Test
    fun resultIsDismissedOnTap() {
        val notification = TransferNotifications.result(context, "Sent to Mac")

        assertEquals("Sent to Mac", notification.extras.getCharSequence(Notification.EXTRA_TEXT).toString())
        assertTrue(notification.flags and Notification.FLAG_AUTO_CANCEL != 0)
        assertTrue(notification.actions.isNullOrEmpty())
        assertFalse(notification.flags and Notification.FLAG_ONGOING_EVENT != 0)
        assertNotNull(notification.contentIntent)
    }

    private fun transfer(name: String, bytes: Long, total: Long): Local.Transfer =
        Local.Transfer.newBuilder()
            .setId(name)
            .setDirection(Local.TransferDirection.TRANSFER_DIRECTION_OUTGOING)
            .setState(Local.TransferState.TRANSFER_STATE_TRANSFERRING)
            .setDeviceName(name)
            .setDeviceId("ab".repeat(32))
            .setBytes(bytes)
            .setTotalBytes(total)
            .build()
}
