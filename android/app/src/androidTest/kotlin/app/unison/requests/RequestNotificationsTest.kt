package app.unison.requests

import android.app.Notification
import android.content.Context
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
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
        assertEquals("Decline", notification.actions[0].title.toString())
        assertEquals("Accept", notification.actions[1].title.toString())
        assertEquals("Friend", notification.extras.getCharSequence(Notification.EXTRA_TITLE).toString())
        assertTrue(
            notification.extras.getCharSequence(Notification.EXTRA_TEXT).toString().contains("2 files"),
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
