package app.unison.tile

import android.app.Activity
import android.app.PendingIntent
import android.annotation.SuppressLint
import android.content.Intent
import android.os.Build
import android.service.quicksettings.TileService

/** Opens an activity of the app from a tile. */
@SuppressLint("StartActivityAndCollapseDeprecated")
internal fun TileService.openActivity(target: Class<out Activity>) {
    val intent = Intent(this, target).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
    if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
        val pending = PendingIntent.getActivity(
            this,
            0,
            intent,
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
        )
        startActivityAndCollapse(pending)
        return
    }
    @Suppress("DEPRECATION")
    startActivityAndCollapse(intent)
}
