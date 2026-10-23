package ru.razgon.app

import android.content.Context
import org.json.JSONArray
import org.json.JSONObject
import ru.razgon.mobile.Mobile
import ru.razgon.mobile.Engine
import java.io.File

data class EngineStatus(
    val state: String = "stopped", val strategy: String = "", val strategyId: String = "",
    val downloaded: Long = 0, val uploaded: Long = 0, val attempts: Int = 0,
    val update: String = "", val error: String = "", val verified: Boolean = false,
)
data class StrategyChoice(val id: String, val name: String)

// Owned exclusively by PluginVpnService, never by the activity.
class EngineController(private val context: Context) {
    private var engine: Engine? = null
    fun start(fd: Int, profile: String, settings: PluginSettings) {
        check(engine == null)
        val next = Mobile.newEngine()
        try {
            val data = File(context.filesDir, "engine").apply { mkdirs() }
            val fake = File(data, "google.bin")
            context.assets.open("tls_clienthello_www_google_com.bin").use { input -> fake.outputStream().use { input.copyTo(it) } }
            val pub = context.assets.open("config-public.txt").bufferedReader().use { it.readText().trim() }
            next.startVPN(fd.toLong(), File(context.applicationInfo.nativeLibraryDir, "libciadpi.so").absolutePath,
                fake.absolutePath, data.absolutePath, profile,
                "https://raw.githubusercontent.com/sonjiwu2/youtube-plugin-android/main/updates/strategies.json",
                pub, settings.strategy, settings.dns)
            engine = next
        } catch (error: Exception) { next.stop(); throw error }
    }
    fun stop() { engine?.stop(); engine = null }
    fun diagnostics(): String = engine?.diagnostics() ?: "Движок остановлен"
    fun status(): EngineStatus {
        val json = JSONObject(engine?.status() ?: "{}")
        return EngineStatus(json.optString("state", "stopped"), json.optString("strategy"), json.optString("strategyId"),
            json.optLong("totalBytes"), json.optLong("uploadedBytes"), json.optInt("attempts"),
            json.optString("update"), json.optString("error"), json.optBoolean("verified"))
    }
    fun strategies(): List<StrategyChoice> = parseStrategies(engine?.strategies() ?: Mobile.newEngine().strategies())
    companion object {
        fun parseStrategies(raw: String): List<StrategyChoice> {
            val json = JSONArray(raw)
            return listOf(StrategyChoice("auto", "Автоподбор")) + (0 until json.length()).map {
                json.getJSONObject(it).let { value -> StrategyChoice(value.getString("id"), value.getString("name")) }
            }.filter { it.id != "auto" }
        }
    }
}
