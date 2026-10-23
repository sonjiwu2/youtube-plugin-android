package ru.razgon.app

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.net.Network
import android.net.VpnService
import android.os.Build
import android.os.ParcelFileDescriptor
import android.os.SystemClock
import android.service.quicksettings.TileService
import androidx.core.app.NotificationCompat
import androidx.core.content.ContextCompat
import kotlinx.coroutines.*
import kotlin.math.max

class PluginVpnService : VpnService() {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private val settings by lazy { SettingsStore(this) }
    private val controller by lazy { EngineController(this) }
    private var worker: Job? = null
    private var descriptor: ParcelFileDescriptor? = null
    private var restartRequested = false
    private var notified = ""

    override fun onCreate() {
        super.onCreate()
        if (Build.VERSION.SDK_INT >= 26) getSystemService(NotificationManager::class.java).createNotificationChannel(
            NotificationChannel(CHANNEL, "Фоновое подключение", NotificationManager.IMPORTANCE_LOW))
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (intent?.action == STOP) settings.boolean("enabled", false)
        if (!settings.read().enabled) {
            if (worker == null) { stopForeground(STOP_FOREGROUND_REMOVE); stopSelf() }
            return START_NOT_STICKY
        }
        try {
            val notification = notification(ConnectionState(state = "starting"))
            if (Build.VERSION.SDK_INT >= 34) startForeground(NOTIFICATION, notification, ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE)
            else startForeground(NOTIFICATION, notification)
        } catch (error: Exception) {
            settings.boolean("enabled", false)
            PluginState.mutable.value = ConnectionState(state = "error", error = "Android не разрешил фоновое подключение: ${error.message}")
            stopSelf(); return START_NOT_STICKY
        }
        if (intent?.action == RECONFIGURE) restartRequested = true
        if (worker == null) worker = scope.launch { runConnection() }
        refreshTile(this)
        return START_STICKY
    }

    private suspend fun runConnection() {
        var running = false
        var previousNetwork: Network? = null
        var previousProfile = ""
        var previousSettings: PluginSettings? = null
        var started = false
        var paused = false
        var retryAt = 0L
        var retryDelay = 15_000L
        var lastDown = 0L
        var lastUp = 0L
        var baseDown = 0L
        var baseUp = 0L
        var lastSample = SystemClock.elapsedRealtime()
        val since = lastSample
        var history = emptyList<Float>()
        try {
            check(prepare(this) == null) { "Разрешите VPN-подключение в Android" }
            packageManager.getApplicationInfo(YOUTUBE, 0)
            // A nonempty per-app allowlist is mandatory. Never fall back to all apps.
            descriptor = Builder().setSession("Youtube Plugin").setMtu(1500)
                .addAddress("10.77.0.2", 24).addAddress("fd77:7974:7000::2", 64)
                .addRoute("0.0.0.0", 0).addRoute("::", 0).addDnsServer("10.77.0.1")
                .addAllowedApplication(YOUTUBE).setBlocking(false)
                .setConfigureIntent(openIntent()).apply {
                    if (Build.VERSION.SDK_INT >= 29) setMetered(false)
                    // The separate test UID exercises the exact same TUN path.
                    // It cannot enter the production allowlist.
                    if (BuildConfig.DEBUG && runCatching { packageManager.getApplicationInfo("ru.youtubeplugin.probe", 0) }.isSuccess) addAllowedApplication("ru.youtubeplugin.probe")
                }
                .establish() ?: error("Не удалось создать VPN-интерфейс. Проверьте разрешение Android")
            PluginState.strategies.value = withContext(Dispatchers.IO) { controller.strategies() }
            while (currentCoroutineContext().isActive && settings.read().enabled) {
                val config = settings.read()
                val profile = networkProfile(this)
                val now = SystemClock.elapsedRealtime()
                val changed = previousNetwork != profile.network || previousProfile != profile.key
                val reconfigured = restartRequested || previousSettings?.let { it.strategy != config.strategy || it.dns != config.dns } == true
                restartRequested = false
                if (changed || reconfigured) {
                    if (running) {
                        val final = withContext(Dispatchers.IO) { controller.status().also { controller.stop() } }
                        baseDown += final.downloaded; baseUp += final.uploaded
                        lastDown = 0; lastUp = 0; running = false
                    }
                    paused = started && changed && !config.reconnect && !reconfigured
                    retryAt = 0; retryDelay = 15_000
                    previousNetwork = profile.network; previousProfile = profile.key
                    setUnderlyingNetworks(profile.network?.let { arrayOf(it) })
                }
                if (paused && config.reconnect) paused = false
                previousSettings = config
                var state = when { !profile.online -> "offline"; paused -> "paused"; else -> "retrying" }
                var engineStatus = EngineStatus()
                var error = if (paused) "Включите восстановление связи или выключите и включите подключение." else ""
                if (profile.online && !paused && !running && now >= retryAt) {
                    PluginState.mutable.value = PluginState.mutable.value.copy(state = "starting", error = "")
                    try {
                        withContext(NonCancellable + Dispatchers.IO) { controller.start(descriptor!!.fd, profile.key, config) }
                        running = true; started = true; lastDown = 0; lastUp = 0; lastSample = now
                    } catch (failure: Exception) {
                        error = failure.message ?: "Не удалось запустить подключение"
                        retryAt = now + retryDelay
                        retryDelay = (retryDelay * 2).coerceAtMost(300_000)
                        state = "error"
                        if (!config.reconnect) paused = true
                    }
                }
                if (running) {
                    engineStatus = withContext(Dispatchers.IO) { controller.status() }
                    state = engineStatus.state; error = engineStatus.error
                    if (state == "ready") retryDelay = 15_000
                    PluginState.diagnostics = withContext(Dispatchers.IO) { controller.diagnostics() }
                    if (BuildConfig.DEBUG) withContext(Dispatchers.IO) { java.io.File(cacheDir, "vpn-diagnostics.json").writeText(PluginState.diagnostics) }
                    if (state == "blocked" && config.reconnect) {
                        baseDown += engineStatus.downloaded; baseUp += engineStatus.uploaded
                        withContext(NonCancellable + Dispatchers.IO) { controller.stop() }
                        running = false; lastDown = 0; lastUp = 0
                        retryAt = now + retryDelay; retryDelay = (retryDelay * 2).coerceAtMost(300_000)
                        engineStatus = engineStatus.copy(downloaded = 0, uploaded = 0)
                        state = "retrying"
                    }
                }
                val elapsed = max(1, now - lastSample) / 1000.0
                val down = if (running) max(0, engineStatus.downloaded - lastDown) / elapsed else 0.0
                val up = if (running) max(0, engineStatus.uploaded - lastUp) / elapsed else 0.0
                lastDown = engineStatus.downloaded; lastUp = engineStatus.uploaded; lastSample = now
                history = (history + down.toFloat()).takeLast(60)
                val snapshot = ConnectionState(state, engineStatus.strategy, error, down, up,
                    baseDown + engineStatus.downloaded, baseUp + engineStatus.uploaded, (now - since) / 1000, history, engineStatus.attempts)
                PluginState.mutable.value = snapshot
                notify(snapshot)
                delay(1_000)
            }
        } catch (cancelled: CancellationException) { throw cancelled }
        catch (failure: Exception) {
            settings.boolean("enabled", false)
            PluginState.mutable.value = PluginState.mutable.value.copy(state = "error", error = when (failure) {
                is android.content.pm.PackageManager.NameNotFoundException -> "Установите официальное приложение YouTube и включите подключение снова."
                else -> failure.message ?: "Не удалось запустить подключение"
            })
        } finally {
            withContext(NonCancellable + Dispatchers.IO) { controller.stop(); descriptor?.close(); descriptor = null }
            if (!settings.read().enabled && PluginState.mutable.value.state != "error") {
                PluginState.mutable.value = PluginState.mutable.value.copy(state = "stopped", downSpeed = 0.0, upSpeed = 0.0, error = "")
            }
            worker = null
            refreshTile(this)
            if (settings.read().enabled && scope.isActive) {
                worker = scope.launch { runConnection() }
            } else {
                stopForeground(STOP_FOREGROUND_REMOVE)
                stopSelf()
            }
        }
    }

    private fun openIntent() = PendingIntent.getActivity(this, 0, Intent(this, MainActivity::class.java), PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT)
    private fun notification(state: ConnectionState): Notification {
        val speed = settings.read().notificationSpeed
        val stop = PendingIntent.getService(this, 1, Intent(this, PluginVpnService::class.java).setAction(STOP), PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT)
        return NotificationCompat.Builder(this, CHANNEL).setSmallIcon(R.drawable.ic_service)
            .setContentTitle("Youtube Plugin · ${stateText(state.state)}")
            .setContentText(if (speed) "↓ ${speedText(state.downSpeed)} Мбит/с   ↑ ${speedText(state.upSpeed)} Мбит/с" else "Подключение для приложения YouTube")
            .setContentIntent(openIntent()).setOnlyAlertOnce(true).setSilent(true).setOngoing(true)
            .setCategory(NotificationCompat.CATEGORY_SERVICE)
            .addAction(0, "Выключить", stop).build()
    }
    private fun notify(state: ConnectionState) {
        val fingerprint = "${state.state}|${settings.read().notificationSpeed}|${speedText(state.downSpeed)}|${speedText(state.upSpeed)}"
        if (fingerprint == notified) return
        notified = fingerprint
        getSystemService(NotificationManager::class.java).notify(NOTIFICATION, notification(state))
        refreshTile(this)
    }
    override fun onRevoke() {
        settings.boolean("enabled", false)
        PluginState.mutable.value = PluginState.mutable.value.copy(state = "error", error = "Разрешение VPN отозвано или включён другой VPN. Включите подключение снова.")
        scope.cancel(); stopSelf(); super.onRevoke()
    }
    override fun onDestroy() {
        scope.cancel()
        super.onDestroy()
    }
    companion object {
        const val YOUTUBE = "com.google.android.youtube"
        const val STOP = "ru.razgon.app.STOP"
        const val RECONFIGURE = "ru.razgon.app.RECONFIGURE"
        private const val CHANNEL = "connection"
        private const val NOTIFICATION = 43
        fun start(context: Context, reconfigure: Boolean = false) {
            ContextCompat.startForegroundService(context, Intent(context, PluginVpnService::class.java).apply { if (reconfigure) action = RECONFIGURE })
        }
        fun disable(context: Context) {
            SettingsStore(context).boolean("enabled", false)
            // The service owns teardown; closing the activity never touches it.
            context.startService(Intent(context, PluginVpnService::class.java).setAction(STOP))
        }
        fun refreshTile(context: Context) = TileService.requestListeningState(context, ComponentName(context, QuickTileService::class.java))
    }
}
