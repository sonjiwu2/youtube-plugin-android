package ru.razgon.app

import android.content.Context
import androidx.webkit.ProxyConfig
import androidx.webkit.ProxyController
import androidx.webkit.WebViewFeature
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.withContext
import kotlinx.coroutines.withTimeout
import kotlinx.coroutines.suspendCancellableCoroutine
import org.json.JSONObject
import ru.razgon.mobile.Mobile
import ru.razgon.mobile.Engine
import java.io.File
import java.util.concurrent.Executor
import kotlin.coroutines.resume

data class EngineStatus(
    val state: String = "starting", val strategy: String = "", val strategyId: String = "",
    val speed: Double = 0.0, val bytes: Long = 0, val attempts: Int = 0,
    val update: String = "", val error: String = "",
    val verified: Boolean = false,
)

class EngineController(private val context: Context) {
    private var engine: Engine? = null

    suspend fun start(profile: String) {
        check(WebViewFeature.isFeatureSupported(WebViewFeature.PROXY_OVERRIDE)) { "Обновите Android System WebView: эта версия не поддерживает локальный прокси." }
        val next = Mobile.newEngine()
        engine = next
        try {
            val port = withContext(Dispatchers.IO) {
                val data = File(context.filesDir, "engine").apply { mkdirs() }
                val fake = File(data, "google.bin")
                context.assets.open("tls_clienthello_www_google_com.bin").use { input -> fake.outputStream().use { input.copyTo(it) } }
                val pub = context.assets.open("config-public.txt").bufferedReader().use { it.readText().trim() }
                next.start(File(context.applicationInfo.nativeLibraryDir, "libciadpi.so").absolutePath,
                    fake.absolutePath, data.absolutePath, profile,
                    "https://raw.githubusercontent.com/sonjiwu2/youtube-android-zapret/main/updates/strategies.json", pub)
            }
            setProxy(port.toInt())
        } catch (error: Exception) {
            withContext(NonCancellable + Dispatchers.IO) { next.stop() }
            engine = null
            throw error
        }
    }

    private suspend fun setProxy(port: Int) = withTimeout(5_000) {
        check(WebViewFeature.isFeatureSupported(WebViewFeature.PROXY_OVERRIDE))
        suspendCancellableCoroutine { continuation ->
            val config = ProxyConfig.Builder().addProxyRule("http://127.0.0.1:$port").removeImplicitRules().build()
            ProxyController.getInstance().setProxyOverride(config, Executor { it.run() }) {
                if (continuation.isActive) continuation.resume(Unit)
            }
        }
    }

    suspend fun stop() {
        val current = engine
        engine = null
        // Оставляем override на закрытом порту: во время остановки прямого пути нет.
        if (current != null) withContext(NonCancellable + Dispatchers.IO) { current.stop() }
    }

    suspend fun next() = withContext(Dispatchers.IO) { engine?.next(); Unit }
    suspend fun measure(address: String): String {
        val current = engine ?: return "{\"Error\":\"Движок остановлен\"}"
        return withContext(Dispatchers.IO) { current.measure(address) }
    }
    suspend fun diagnostics(): String = withContext(Dispatchers.IO) { engine?.diagnostics() ?: "Движок остановлен" }
    suspend fun status(): EngineStatus = withContext(Dispatchers.IO) {
        val json = JSONObject(engine?.status() ?: "{\"state\":\"stopped\"}")
        EngineStatus(json.optString("state"), json.optString("strategy"), json.optString("strategyId"),
            json.optDouble("bytesPerSecond", 0.0), json.optLong("totalBytes"), json.optInt("attempts"),
            json.optString("update"), json.optString("error"), json.optBoolean("verified"))
    }
}
