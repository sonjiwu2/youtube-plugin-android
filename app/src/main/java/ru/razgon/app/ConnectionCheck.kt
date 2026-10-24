package ru.razgon.app

import android.content.Context
import android.os.SystemClock
import android.util.AtomicFile
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.MutableStateFlow
import org.json.JSONArray
import org.json.JSONObject
import java.io.File
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale
import java.util.TimeZone
import kotlin.math.max

data class CheckState(val running: Boolean = false, val remaining: Int = 0,
    val summary: String = "Проверка ещё не запускалась", val report: String = "")

object ConnectionCheck {
    val state = MutableStateFlow(CheckState())
    private fun file(context: Context) = AtomicFile(File(context.filesDir, "last-connection-check.json"))
    fun load(context: Context) {
        if (state.value.running || state.value.report.isNotEmpty()) return
        runCatching {
            val report = file(context).openRead().bufferedReader().use { it.readText() }
            if (!state.value.running && state.value.report.isEmpty()) state.value = CheckState(summary = JSONObject(report).getString("summary"), report = report)
        }
    }
    private fun save(context: Context, report: JSONObject) {
        val text = report.toString(2)
        val storage = file(context)
        val stream = storage.startWrite()
        try { stream.write(text.toByteArray(Charsets.UTF_8)); storage.finishWrite(stream) }
        catch (error: Exception) { storage.failWrite(stream); throw error }
        state.value = CheckState(summary = report.getString("summary"), report = text)
    }

    suspend fun run(context: Context, controller: EngineController, settings: SettingsStore) = coroutineScope {
        state.value = CheckState(true, 45, "Откройте ролик в обычном YouTube")
        val started = SystemClock.elapsedRealtime()
        val config = settings.read()
        val initial = withContext(Dispatchers.IO) { controller.status() }
        var previous = initial
        var previousAt = started
        var down = 0L; var up = 0L; var video = 0L; var peak = 0.0; var active = 0
        val samples = JSONArray()
        val endpoints = async(Dispatchers.IO) { runCatching { JSONObject(controller.checkConnectivity()) }
            .getOrElse { JSONObject().put("error", it.message ?: "Проверка недоступна") } }
        var interrupted = false
        try {
            while (SystemClock.elapsedRealtime() - started < 45_000 && settings.read().enabled) {
                delay(1_000)
                val current = withContext(Dispatchers.IO) { controller.status() }
                val now = SystemClock.elapsedRealtime()
                fun delta(next: Long, old: Long) = if (next >= old) next - old else next
                val received = delta(current.downloaded, previous.downloaded)
                down += received; up += delta(current.uploaded, previous.uploaded)
                video += delta(current.videoBytes, previous.videoBytes)
                peak = max(peak, received * 1000.0 / max(1, now - previousAt))
                if (received > 0) active++
                samples.put(JSONObject().put("second", (now-started)/1000).put("received", received)
                    .put("videoReceived", delta(current.videoBytes, previous.videoBytes))
                    .put("strategyId", current.strategyId).put("state", current.state))
                state.value = CheckState(true, max(0, 45 - ((now-started)/1000).toInt()),
                    "Получено видео: ${sizeText(video)}")
                previous = current; previousAt = now
            }
            interrupted = !settings.read().enabled
        } catch (cancelled: CancellationException) {
            interrupted = true
            throw cancelled
        } finally {
            withContext(NonCancellable + Dispatchers.IO) {
                val network = if (interrupted) { endpoints.cancel(); JSONObject().put("interrupted", true) }
                    else runCatching { endpoints.await() }.getOrElse { JSONObject().put("error", it.message) }
                val summary = when {
                    interrupted -> "Проверка прервана: подключение выключено"
                    video >= 131_072 -> "Видеоданные поступают в YouTube"
                    down > 0 -> "Трафик есть, загрузка видео не подтверждена"
                    else -> "Трафик YouTube не обнаружен. Откройте ролик и повторите проверку"
                }
                val report = JSONObject().put("schema", 1).put("appVersion", BuildConfig.VERSION_NAME)
                    .put("build", BuildConfig.VERSION_CODE).put("at", SimpleDateFormat("yyyy-MM-dd'T'HH:mm:ss'Z'", Locale.US)
                        .apply { timeZone = TimeZone.getTimeZone("UTC") }.format(Date()))
                    .put("summary", summary).put("interrupted", interrupted)
                    .put("durationSeconds", (SystemClock.elapsedRealtime()-started)/1000)
                    .put("strategyPreference", config.strategy).put("dns", config.dns)
                    .put("startStrategyId", initial.strategyId).put("endStrategyId", previous.strategyId)
                    .put("downloadedBytes", down).put("uploadedBytes", up).put("videoBytes", video)
                    .put("videoDataReceived", !interrupted && video >= 131_072)
                    .put("peakBytesPerSecond", peak).put("activeSamples", active)
                    .put("network", network).put("samples", samples)
                runCatching { save(context, report) }.onFailure {
                    state.value = CheckState(summary = summary, report = report.toString(2))
                }
            }
        }
    }

    fun details(report: String): String = runCatching {
        val json = JSONObject(report)
        val checks = json.optJSONObject("network")?.optJSONArray("checks") ?: JSONArray()
        val timestamp = SimpleDateFormat("yyyy-MM-dd'T'HH:mm:ss'Z'", Locale.US).apply { timeZone = TimeZone.getTimeZone("UTC") }.parse(json.getString("at"))
        val date = SimpleDateFormat("dd.MM.yyyy HH:mm", Locale.getDefault()).format(timestamp!!)
        val lines = (0 until checks.length()).map { index ->
            val check = checks.getJSONObject(index)
            val name = if (check.optString("host") == "m.youtube.com") "Страница YouTube" else "API YouTube"
            "$name: ${if (check.optBoolean("ok")) "доступен" else "ошибка"}"
        }
        (listOf("Проверено: $date") + lines + listOf("Видеоданные: ${sizeText(json.optLong("videoBytes"))}",
            "Приём: ${sizeText(json.optLong("downloadedBytes"))}",
            "Передача: ${sizeText(json.optLong("uploadedBytes"))}",
            "Пиковая скорость: ${speedText(json.optDouble("peakBytesPerSecond", 0.0))} Мбит/с",
            "Время проверки: ${json.optLong("durationSeconds")} с")).joinToString("\n")
    }.getOrDefault("")
}
