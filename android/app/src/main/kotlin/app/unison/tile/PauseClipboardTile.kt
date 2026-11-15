package app.unison.tile

import android.service.quicksettings.Tile
import android.service.quicksettings.TileService
import android.text.format.DateFormat
import app.unison.R
import app.unison.clipboard.ClipboardPause
import app.unison.clipboard.PauseClipboardActivity
import java.util.Date
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.launch
import unison.local.v1.Local

/** Quick settings tile that pauses the clipboard synchronization. */
class PauseClipboardTile : TileService() {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)

    override fun onStartListening() {
        super.onStartListening()
        scope.launch { refresh() }
    }

    override fun onClick() {
        openActivity(PauseClipboardActivity::class.java)
    }

    override fun onDestroy() {
        scope.cancel()
        super.onDestroy()
    }

    private suspend fun refresh() {
        val tile = qsTile ?: return
        val pause = ClipboardPause.read()
        tile.state = if (pause?.paused == true) Tile.STATE_ACTIVE else Tile.STATE_INACTIVE
        tile.subtitle = subtitle(pause)
        tile.updateTile()
    }

    private fun subtitle(pause: Local.Pause?): String? = when {
        pause == null -> null
        !pause.paused -> getString(R.string.tile_pause_off)
        pause.indefinite -> getString(R.string.tile_pause_indefinite)
        else -> {
            val time = DateFormat.getTimeFormat(this).format(Date(pause.untilUnixMs))
            getString(R.string.tile_pause_until, time)
        }
    }
}
