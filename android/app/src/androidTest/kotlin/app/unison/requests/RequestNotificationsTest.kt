package app.unison.requests

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
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import unison.local.v1.Local

@RunWith(AndroidJUnit4::class)
class RequestNotificationsTest {
    private val context = ApplicationProvider.getApplicationContext<Context>()

    @Test
    fun offerOffersBothAnswers() {
        val notification = RequestNotifications.request(context, request())

        assertEquals(2, notification.actions.size)
        assertEquals(testString(R.string.action_decline), notification.actions[0].title.toString())
        assertEquals(testString(R.string.action_accept), notification.actions[1].title.toString())
        assertEquals("Friend", notification.extras.getCharSequence(Notification.EXTRA_TITLE).toString())
        assertEquals(
            testQuantity(
                R.plurals.request_files,
                2,
                2,
                HistoryText.formatSize(3072, SizeUnits.of(testContext)),
            ),
            notification.extras.getCharSequence(Notification.EXTRA_TEXT).toString(),
        )
    }

    @Test
    fun notificationIdsDifferPerOffer() {
        assertTrue(RequestNotifications.idOf("one") != RequestNotifications.idOf("two"))
        assertTrue(RequestNotifications.RESULT_ID != RequestNotifications.idOf("one"))
    }

    private fun request(): Local.ShareRequest = Local.ShareRequest.newBuilder()
        .setId("request-id")
        .setDeviceId("friend-id")
        .setDeviceName("Friend")
        .addFiles(Local.TransferFile.newBuilder().setName("notes.txt").setSize(1024).build())
        .addFiles(Local.TransferFile.newBuilder().setName("plan.pdf").setSize(2048).build())
        .setTotalBytes(3072)
        .build()
}
