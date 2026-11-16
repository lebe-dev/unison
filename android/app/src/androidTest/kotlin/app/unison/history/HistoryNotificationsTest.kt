package app.unison.history

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
class HistoryNotificationsTest {
    private val context = ApplicationProvider.getApplicationContext<Context>()

    @Test
    fun pendingEntryOffersADownload() {
        val notification = HistoryNotifications.pending(context, image(), "Mac")

        assertEquals(1, notification.actions.size)
        assertEquals("Download", notification.actions[0].title.toString())
        assertEquals("Mac", notification.extras.getCharSequence(Notification.EXTRA_TITLE).toString())
        assertTrue(
            notification.extras.getCharSequence(Notification.EXTRA_TEXT).toString().contains("Image"),
        )
    }

    @Test
    fun notificationIdsDifferPerEntry() {
        assertTrue(HistoryNotifications.idOf("one") != HistoryNotifications.idOf("two"))
    }

    private fun image(): Local.ClipItem = Local.ClipItem.newBuilder()
        .setId("item-id")
        .setKind(Local.Kind.KIND_IMAGE)
        .build()
}
