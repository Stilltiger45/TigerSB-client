package app.tigersb

import android.app.PendingIntent
import android.content.Intent
import android.net.VpnService
import android.os.Build
import android.service.quicksettings.Tile
import app.tigersb.mobile.Mobile

/** Quick settings tile: one tap connects or disconnects. */
class TileService : android.service.quicksettings.TileService() {
    private val update = { refresh() }

    override fun onStartListening() {
        VpnState.listen(update)
        refresh()
    }

    override fun onStopListening() {
        VpnState.unlisten(update)
    }

    override fun onClick() {
        when (VpnState.phase) {
            VpnState.Phase.CONNECTED, VpnState.Phase.CONNECTING -> TigerVpnService.stop(this)
            else -> {
                Core.init(this)
                val ready = VpnService.prepare(this) == null && runCatching { Mobile.current() }.getOrDefault("").isNotEmpty()
                if (ready) TigerVpnService.start(this) else openApp()
            }
        }
    }

    /** No key or no VPN permission yet: the app asks for them. */
    private fun openApp() {
        val i = Intent(this, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        if (Build.VERSION.SDK_INT >= 34) {
            startActivityAndCollapse(PendingIntent.getActivity(this, 0, i, PendingIntent.FLAG_IMMUTABLE))
        } else {
            @Suppress("DEPRECATION")
            startActivityAndCollapse(i)
        }
    }

    private fun refresh() {
        val tile = qsTile ?: return
        tile.state = when (VpnState.phase) {
            VpnState.Phase.CONNECTED, VpnState.Phase.CONNECTING -> Tile.STATE_ACTIVE
            else -> Tile.STATE_INACTIVE
        }
        tile.updateTile()
    }
}
