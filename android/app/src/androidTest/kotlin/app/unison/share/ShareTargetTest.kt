package app.unison.share

import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.content.pm.ResolveInfo
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class ShareTargetTest {

    private val context: Context get() = ApplicationProvider.getApplicationContext()

    @Test
    fun sendOffersEveryType() {
        assertEquals(ShareActivity::class.java.name, single(Intent.ACTION_SEND, "text/plain"))
        assertEquals(ShareActivity::class.java.name, single(Intent.ACTION_SEND, "image/png"))
        assertEquals(ShareActivity::class.java.name, single(Intent.ACTION_SEND, "application/pdf"))
    }

    @Test
    fun multipleOffersEveryType() {
        assertTrue(activities(Intent.ACTION_SEND_MULTIPLE, "image/jpeg").isNotEmpty())
        assertTrue(activities(Intent.ACTION_SEND_MULTIPLE, "application/octet-stream").isNotEmpty())
    }

    private fun single(action: String, type: String): String {
        val found = activities(action, type)
        assertEquals(1, found.size)
        return found.single().activityInfo.name
    }

    private fun activities(action: String, type: String): List<ResolveInfo> {
        val intent = Intent(action)
            .setType(type)
            .addCategory(Intent.CATEGORY_DEFAULT)
        return context.packageManager
            .queryIntentActivities(intent, PackageManager.MATCH_DEFAULT_ONLY)
            .filter { it.activityInfo.packageName == context.packageName }
    }
}
