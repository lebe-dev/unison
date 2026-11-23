package app.unison.tile

import android.service.quicksettings.Tile
import android.service.quicksettings.TileService
import app.unison.R
import app.unison.clipboard.ClipboardPause
import app.unison.clipboard.SendClipboardActivity
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.launch

/** Quick settings tile that sends the local clipboard. */
class SendClipboardTile : TileService() {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)

    override fun onStartListening() {
        super.onStartListening()
        scope.launch { refresh() }
    }

    override fun onClick() {
        openActivity(SendClipboardActivity::class.java)
    }

    override fun onDestroy() {
        scope.cancel()
        super.onDestroy()
    }

    private suspend fun refresh() {
        val tile = qsTile ?: return
        val paused = ClipboardPause.read()?.paused == true
        tile.state = Tile.STATE_INACTIVE
        tile.subtitle = getString(if (paused) R.string.tile_send_paused else R.string.tile_send_subtitle)
        tile.updateTile()
    }
}
