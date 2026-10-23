package ru.razgon.app

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.net.VpnService

class BootReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        if (intent.action !in listOf(Intent.ACTION_BOOT_COMPLETED, Intent.ACTION_MY_PACKAGE_REPLACED)) return
        val store = SettingsStore(context)
        val settings = store.read()
        if (!settings.enabled) return
        if (intent.action == Intent.ACTION_BOOT_COMPLETED && !settings.autoStart) { store.boolean("enabled", false); return }
        if (VpnService.prepare(context) == null) runCatching { PluginVpnService.start(context) }
    }
}
