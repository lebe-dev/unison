package app.unison.clipboard

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
class HeldNotificationsTest {
    private val context = ApplicationProvider.getApplicationContext<Context>()

    @Test
    fun heldItemOffersAnApply() {
        val notification = HeldNotifications.held(context, item(), "Mac")

        assertEquals(1, notification.actions.size)
        assertEquals("Apply", notification.actions[0].title.toString())
        assertEquals("Mac", notification.extras.getCharSequence(Notification.EXTRA_TITLE).toString())
        assertEquals(
            "Clipboard arrived - apply it to use it",
            notification.extras.getCharSequence(Notification.EXTRA_TEXT).toString(),
        )
    }

    @Test
    fun notificationIdsDifferPerItem() {
        assertTrue(HeldNotifications.idOf("one") != HeldNotifications.idOf("two"))
        assertTrue(HeldNotifications.RESULT_ID != HeldNotifications.idOf("one"))
    }

    private fun item(): Local.ClipItem = Local.ClipItem.newBuilder()
        .setId("item-id")
        .setKind(Local.Kind.KIND_TEXT)
        .build()
}
