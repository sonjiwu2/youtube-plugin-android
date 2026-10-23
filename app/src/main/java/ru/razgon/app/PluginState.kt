package ru.razgon.app

import android.content.Context
import android.annotation.SuppressLint
import android.content.SharedPreferences
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import java.util.Locale

data class PluginSettings(
    val enabled: Boolean = false, val autoStart: Boolean = false, val reconnect: Boolean = true,
    val notificationSpeed: Boolean = true, val theme: String = "system",
    val strategy: String = "auto", val dns: String = "auto",
)
// Toggle changes must reach disk before a service restart or device shutdown.
@SuppressLint("ApplySharedPref")
class SettingsStore(context: Context) {
    val preferences: SharedPreferences = context.getSharedPreferences("plugin", Context.MODE_PRIVATE)
    fun read() = PluginSettings(
        preferences.getBoolean("enabled", false), preferences.getBoolean("autoStart", false),
        preferences.getBoolean("reconnect", true), preferences.getBoolean("notificationSpeed", true),
        preferences.getString("theme", "system") ?: "system", preferences.getString("strategy", "auto") ?: "auto",
        preferences.getString("dns", "auto") ?: "auto",
    )
    fun boolean(key: String, value: Boolean) { preferences.edit().putBoolean(key, value).commit() }
    fun text(key: String, value: String) { preferences.edit().putString(key, value).commit() }
}
data class ConnectionState(
    val state: String = "stopped", val strategy: String = "", val error: String = "",
    val downSpeed: Double = 0.0, val upSpeed: Double = 0.0,
    val downloaded: Long = 0, val uploaded: Long = 0, val seconds: Long = 0,
    val history: List<Float> = emptyList(), val attempts: Int = 0,
)
object PluginState {
    val mutable = MutableStateFlow(ConnectionState())
    val connection = mutable.asStateFlow()
    val strategies = MutableStateFlow<List<StrategyChoice>>(emptyList())
    @Volatile var diagnostics = "Подключение ещё не запускалось"
}
fun speedText(bytes: Double): String = String.format(Locale.US, "%.1f", bytes * 8 / 1_000_000)
fun sizeText(bytes: Long): String = when {
    bytes >= 1_073_741_824 -> String.format(Locale.US, "%.2f ГБ", bytes / 1_073_741_824.0)
    bytes >= 1_048_576 -> String.format(Locale.US, "%.1f МБ", bytes / 1_048_576.0)
    else -> String.format(Locale.US, "%.0f КБ", bytes / 1024.0)
}
fun durationText(seconds: Long): String = String.format(Locale.US, "%02d:%02d:%02d", seconds / 3600, seconds / 60 % 60, seconds % 60)
fun stateText(state: String): String = when (state) {
    "ready" -> "Работает в фоне"
    "starting", "connecting" -> "Подключаемся"
    "selecting" -> "Подбираем стратегию"
    "offline" -> "Ожидаем сеть"
    "retrying" -> "Восстанавливаем связь"
    "blocked" -> "Стратегия не подошла"
    "paused" -> "Нужно переподключиться"
    "error" -> "Не удалось подключиться"
    "stopping" -> "Отключаемся"
    else -> "Выключено"
}
