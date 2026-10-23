package ru.razgon.app

import android.annotation.SuppressLint
import android.app.PendingIntent
import android.content.Intent
import android.net.VpnService
import android.os.Build
import android.service.quicksettings.Tile
import android.service.quicksettings.TileService

class QuickTileService : TileService() {
    override fun onStartListening() {
        super.onStartListening()
        qsTile?.apply { state = if (SettingsStore(this@QuickTileService).read().enabled) Tile.STATE_ACTIVE else Tile.STATE_INACTIVE; updateTile() }
    }
    @SuppressLint("StartActivityAndCollapseDeprecated")
    override fun onClick() {
        super.onClick()
        if (SettingsStore(this).read().enabled) PluginVpnService.disable(this)
        else if (VpnService.prepare(this) == null && runCatching { packageManager.getApplicationInfo(PluginVpnService.YOUTUBE, 0) }.isSuccess) {
            SettingsStore(this).boolean("enabled", true)
            PluginVpnService.start(this)
        } else {
            val intent = Intent(this, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK).putExtra("enable", true)
            if (Build.VERSION.SDK_INT >= 34) startActivityAndCollapse(PendingIntent.getActivity(this, 7, intent, PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT))
            else { @Suppress("DEPRECATION") startActivityAndCollapse(intent) }
        }
        onStartListening()
    }
}
