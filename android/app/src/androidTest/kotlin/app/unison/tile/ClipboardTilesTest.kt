package app.unison.tile

import android.Manifest
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.content.pm.ResolveInfo
import android.content.pm.ServiceInfo
import android.graphics.drawable.Drawable
import android.service.quicksettings.TileService
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import app.unison.R
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith

/**
 * Covers the declarations of the two Quick Settings tiles. The behaviour of a
 * tile while it is shown in the shade needs the system binder and is checked by
 * hand on a device.
 */
@RunWith(AndroidJUnit4::class)
class ClipboardTilesTest {

    private val context: Context get() = ApplicationProvider.getApplicationContext()

    @Test
    fun bothTilesAreDeclared() {
        val declared = tiles()
        assertEquals(2, declared.size)
        val names = declared.map { it.serviceInfo.name }.toSet()
        assertEquals(
            setOf(SendClipboardTile::class.java.name, PauseClipboardTile::class.java.name),
            names,
        )
        for (info in declared) {
            val service: ServiceInfo = info.serviceInfo
            assertEquals(Manifest.permission.BIND_QUICK_SETTINGS_TILE, service.permission)
            assertTrue("${service.name} is not exported", service.exported)
            assertTrue("${service.name} is disabled", service.enabled)
            assertTrue(
                "${service.name} is not a tile service",
                TileService::class.java.isAssignableFrom(Class.forName(service.name)),
            )
        }
    }

    @Test
    fun tilesCarryLabelsAndIcons() {
        val labels = mapOf(
            SendClipboardTile::class.java.name to R.string.send_clipboard,
            PauseClipboardTile::class.java.name to R.string.tile_pause_clipboard,
        )
        val packageManager = context.packageManager
        for (info in tiles()) {
            val service = info.serviceInfo
            val want = context.getString(labels.getValue(service.name))
            assertEquals("label of ${service.name}", want, service.loadLabel(packageManager).toString())

            val icon: Drawable? = service.loadIcon(packageManager)
            assertNotNull("icon of ${service.name}", icon)
            assertTrue(
                "icon of ${service.name} is empty",
                icon!!.intrinsicWidth > 0 && icon.intrinsicHeight > 0,
            )
        }
    }

    @Test
    fun tileSubtitlesArePresent() {
        for (id in listOf(
            R.string.tile_send_subtitle,
            R.string.tile_send_paused,
            R.string.tile_pause_off,
            R.string.tile_pause_indefinite,
            R.string.tile_pause_until,
        )) {
            assertTrue("string $id is empty", context.getString(id).isNotBlank())
        }
    }

    private fun tiles(): List<ResolveInfo> =
        context.packageManager
            .queryIntentServices(Intent(TileService.ACTION_QS_TILE), 0)
            .filter { it.serviceInfo.packageName == context.packageName }
}
