package ru.razgon.app

import android.Manifest
import android.annotation.SuppressLint
import android.app.*
import android.content.Intent
import android.content.pm.PackageManager
import android.content.res.Configuration
import android.graphics.Bitmap
import android.net.*
import android.os.Build
import android.os.Bundle
import android.util.Rational
import android.view.View
import android.view.ViewGroup
import android.webkit.*
import android.widget.FrameLayout
import androidx.activity.ComponentActivity
import androidx.activity.compose.BackHandler
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.draw.clipToBounds
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.core.app.NotificationCompat
import androidx.core.content.ContextCompat
import androidx.lifecycle.lifecycleScope
import androidx.webkit.WebSettingsCompat
import androidx.webkit.WebViewFeature
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import java.util.Locale
import org.json.JSONObject

class MainActivity : ComponentActivity() {
    private val controller by lazy { EngineController(applicationContext) }
    private val transition = Mutex()
    private val preferences by lazy { getSharedPreferences("settings", MODE_PRIVATE) }
    private var status by mutableStateOf(EngineStatus())
    private var profile by mutableStateOf(NetworkProfile("", "", false))
    private var accepted by mutableStateOf(false)
    private var ready by mutableStateOf(false)
    private var loading by mutableStateOf(true)
    private var pageError by mutableStateOf("")
    private var showInfo by mutableStateOf(false)
    private var inPip by mutableStateOf(false)
    private var fullScreen by mutableStateOf(false)
    private var webView: WebView? = null
    private var lastUrl = HOME
    private var customView: View? = null
    private var customCallback: WebChromeClient.CustomViewCallback? = null
    private var polling: Job? = null
    private var started = false
    private var generation = 0
    private var retryPending = false
    private var observedStrategy = ""
    private var strategySince = 0L
    @Volatile private var lastVideoUrl = ""
    private var measuring by mutableStateOf(false)
    private var benchmark by mutableStateOf("")
    private val networks by lazy { getSystemService(ConnectivityManager::class.java) }
    private val networkCallback = object : ConnectivityManager.NetworkCallback() {
        override fun onAvailable(network: Network) = networkChanged()
        override fun onLost(network: Network) = networkChanged()
        override fun onLinkPropertiesChanged(network: Network, properties: LinkProperties) = networkChanged()
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        accepted = preferences.getBoolean("onboarded", false)
        lastUrl = savedInstanceState?.getString("url")?.takeIf { allowedPage(Uri.parse(it)) } ?: HOME
        networks.registerDefaultNetworkCallback(networkCallback)
        setContent { App() }
    }
    override fun onStart() { super.onStart(); started = true; if (accepted) restart() }
    override fun onStop() {
        started = false; generation++; ready = false; polling?.cancel(); destroyBrowser(); cancelNotification(); lastVideoUrl = ""
        lifecycleScope.launch { transition.withLock { controller.stop() } }
        super.onStop()
    }
    override fun onDestroy() { networks.unregisterNetworkCallback(networkCallback); destroyBrowser(); super.onDestroy() }
    override fun onSaveInstanceState(outState: Bundle) { outState.putString("url", lastUrl); super.onSaveInstanceState(outState) }
    private fun networkChanged() = runOnUiThread {
        if (networkProfile(this).key != profile.key && started && accepted) restart()
    }

    private fun restart(resetToHome: Boolean = false) {
        if (!started || !accepted) return
        val token = ++generation
        ready = false; polling?.cancel(); destroyBrowser(); status = EngineStatus(); pageError = ""; profile = networkProfile(this)
        if (resetToHome) lastUrl = HOME
        lifecycleScope.launch {
            transition.withLock {
                controller.stop()
                if (!started || token != generation) return@withLock
                if (!profile.online) { status = EngineStatus(state = "offline"); return@withLock }
                try {
                    controller.start(profile.key)
                    if (!started || token != generation) { controller.stop(); return@withLock }
                    ready = true; loading = true; observedStrategy = ""; strategySince = System.currentTimeMillis()
                    poll(token)
                } catch (error: Exception) {
                    status = EngineStatus(state = "error", error = error.message ?: "Не удалось запустить движок")
                }
            }
        }
    }
    private fun poll(token: Int) {
        polling = lifecycleScope.launch {
            while (started && generation == token) {
                val next = controller.status()
                if (generation != token) break
                if (BuildConfig.DEBUG && next.error.isNotEmpty() && next.error != status.error)
                    android.util.Log.d("RazgonEngine", next.error)
                status = next
                if (next.state == "blocked") { ready = false; destroyBrowser() }
                else if (next.strategyId != observedStrategy) {
                    val changed = observedStrategy.isNotEmpty()
                    observedStrategy = next.strategyId; strategySince = System.currentTimeMillis()
                    if (changed) { pageError = ""; webView?.reload() }
                } else if (loading && next.state != "ready" && System.currentTimeMillis() - strategySince > 30_000 && !retryPending) nextStrategy()
                updateNotification(); delay(1_000)
            }
        }
    }
    private fun nextStrategy() {
        if (!ready || retryPending) return
        retryPending = true
        lifecycleScope.launch {
            try { controller.next(); pageError = ""; loading = true; strategySince = System.currentTimeMillis() }
            finally { retryPending = false }
        }
    }
    private fun destroyBrowser() {
        hideCustomView()
        webView?.let {
            it.url?.let { url -> if (allowedPage(Uri.parse(url))) lastUrl = url }
            it.stopLoading(); it.onPause(); (it.parent as? ViewGroup)?.removeView(it); it.removeAllViews(); it.destroy()
        }
        webView = null
    }

    @SuppressLint("SetJavaScriptEnabled")
    private fun createBrowser(): WebView = WebView(this).apply {
        webView = this
        val night = resources.configuration.uiMode and Configuration.UI_MODE_NIGHT_MASK == Configuration.UI_MODE_NIGHT_YES
        setBackgroundColor(if (night) android.graphics.Color.BLACK else android.graphics.Color.WHITE)
        clipToOutline = true
        settings.apply {
            javaScriptEnabled = true; domStorageEnabled = true; allowFileAccess = false; allowContentAccess = false
            mixedContentMode = WebSettings.MIXED_CONTENT_NEVER_ALLOW; mediaPlaybackRequiresUserGesture = true
            javaScriptCanOpenWindowsAutomatically = false; setSupportMultipleWindows(false)
            if (Build.VERSION.SDK_INT >= 26) safeBrowsingEnabled = true
        }
        CookieManager.getInstance().setAcceptCookie(true)
        CookieManager.getInstance().setAcceptThirdPartyCookies(this, false)
        if (WebViewFeature.isFeatureSupported(WebViewFeature.ALGORITHMIC_DARKENING)) WebSettingsCompat.setAlgorithmicDarkeningAllowed(settings, true)
        webViewClient = object : WebViewClient() {
            override fun shouldInterceptRequest(view: WebView, request: WebResourceRequest): WebResourceResponse? {
                val host = request.url.host.orEmpty().lowercase()
                if (request.url.scheme == "https" && (host == "googlevideo.com" || host.endsWith(".googlevideo.com")) && request.url.path?.contains("videoplayback") == true) lastVideoUrl = request.url.toString()
                return null
            }
            override fun shouldOverrideUrlLoading(view: WebView, request: WebResourceRequest): Boolean =
                request.url.scheme != "https" || request.isForMainFrame && !allowedPage(request.url)
            override fun onPageStarted(view: WebView, url: String?, favicon: Bitmap?) { loading = true }
            override fun onPageFinished(view: WebView, url: String?) { loading = false; if (pageError.isEmpty()) CookieManager.getInstance().flush() }
            override fun onReceivedError(view: WebView, request: WebResourceRequest, error: WebResourceError) {
                if (request.isForMainFrame && started && ready) {
                    loading = false; pageError = "Страница не загрузилась. Проверяем следующую стратегию…"
                    lifecycleScope.launch { delay(1_500); if (started && pageError.isNotEmpty()) nextStrategy() }
                }
            }
            override fun onRenderProcessGone(view: WebView, detail: RenderProcessGoneDetail): Boolean {
                ready = false; destroyBrowser(); status = EngineStatus(state = "error", error = "WebView завершил работу. Нажмите «Повторить»."); return true
            }
        }
        webChromeClient = object : WebChromeClient() {
            override fun onPermissionRequest(request: PermissionRequest) { request.deny() }
            override fun onShowCustomView(view: View, callback: CustomViewCallback) {
                if (customView != null) { callback.onCustomViewHidden(); return }
                customView = view; customCallback = callback; fullScreen = true
                addContentView(view, FrameLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT))
            }
            override fun onHideCustomView() = hideCustomView()
        }
        loadUrl(lastUrl)
    }
    private fun hideCustomView() {
        (customView?.parent as? ViewGroup)?.removeView(customView); customView = null
        val callback = customCallback; customCallback = null; callback?.onCustomViewHidden(); fullScreen = false
    }
    private fun enterPip() {
        if (Build.VERSION.SDK_INT >= 26 && packageManager.hasSystemFeature(PackageManager.FEATURE_PICTURE_IN_PICTURE) && ready)
            enterPictureInPictureMode(PictureInPictureParams.Builder().setAspectRatio(Rational(16, 9)).build())
    }
    override fun onUserLeaveHint() { if (fullScreen && ready) enterPip(); super.onUserLeaveHint() }
    override fun onPictureInPictureModeChanged(isInPictureInPictureMode: Boolean, newConfig: Configuration) {
        super.onPictureInPictureModeChanged(isInPictureInPictureMode, newConfig); inPip = isInPictureInPictureMode
    }
    private fun updateNotification() {
        val manager = getSystemService(NotificationManager::class.java)
        if (Build.VERSION.SDK_INT >= 26) manager.createNotificationChannel(NotificationChannel("engine", "Состояние подключения", NotificationManager.IMPORTANCE_LOW))
        if (Build.VERSION.SDK_INT >= 33 && ContextCompat.checkSelfPermission(this, Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED) return
        val intent = PendingIntent.getActivity(this, 0, Intent(this, MainActivity::class.java), PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT)
        manager.notify(42, NotificationCompat.Builder(this, "engine").setSmallIcon(R.drawable.ic_launcher_foreground)
            .setContentTitle("Разгон · ${profile.label}").setContentText(statusText(status.state)).setContentIntent(intent)
            .setOnlyAlertOnce(true).setOngoing(true).setSilent(true).build())
    }
    private fun cancelNotification() = getSystemService(NotificationManager::class.java).cancel(42)

    @Composable private fun App() {
        val scheme = if (isSystemInDarkTheme()) darkColorScheme(primary = Color(0xFFB5F768), primaryContainer = Color(0xFF2E4221), onPrimaryContainer = Color(0xFFE0F2D0), background = Color(0xFF101410), surface = Color(0xFF171D17))
            else lightColorScheme(primary = Color(0xFF315F17), primaryContainer = Color(0xFFE0EBCF), onPrimaryContainer = Color(0xFF142309), background = Color(0xFFF4F6EF), surface = Color.White)
        MaterialTheme(colorScheme = scheme) {
            BackHandler { when { customView != null -> hideCustomView(); showInfo -> showInfo = false; webView?.canGoBack() == true -> webView?.goBack(); else -> finish() } }
            Surface(Modifier.fillMaxSize()) {
                if (!accepted) Welcome() else Column(Modifier.fillMaxSize().systemBarsPadding()) {
                    if (!inPip && !fullScreen) {
                        Row(Modifier.fillMaxWidth().padding(horizontal = 20.dp, vertical = 12.dp), verticalAlignment = Alignment.CenterVertically) {
                            Column(Modifier.weight(1f)) {
                                Text("РАЗГОН", style = MaterialTheme.typography.titleLarge, fontWeight = FontWeight.Black)
                                Text(profile.label.ifEmpty { "Личное подключение" }, style = MaterialTheme.typography.labelMedium)
                            }
                            TextButton(onClick = { showInfo = true }) { Text("О приложении") }
                        }
                        Card(Modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, bottom = 12.dp), shape = RoundedCornerShape(22.dp), colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.primaryContainer)) {
                            Row(Modifier.padding(16.dp), verticalAlignment = Alignment.CenterVertically) {
                                Column(Modifier.weight(1f)) {
                                    Text(statusText(status.state), fontWeight = FontWeight.SemiBold, style = MaterialTheme.typography.titleSmall)
                                    Text(status.strategy.ifEmpty { "Подготовка подключения" }, style = MaterialTheme.typography.bodySmall)
                                }
                                Column(horizontalAlignment = Alignment.End) {
                                    Text(if (status.speed > 0) String.format(Locale.US, "%.1f", status.speed * 8 / 1_000_000) else "—", style = MaterialTheme.typography.headlineSmall, fontWeight = FontWeight.Bold)
                                    Text("Мбит/с · видео", style = MaterialTheme.typography.labelSmall)
                                }
                            }
                        }
                    }
                    Box(Modifier.weight(1f).fillMaxWidth()) {
                        if (ready) AndroidView(factory = { createBrowser() }, modifier = Modifier.fillMaxSize().clipToBounds())
                        if (!ready || pageError.isNotEmpty()) ErrorOrLoading()
                        if (ready && loading && pageError.isEmpty()) LinearProgressIndicator(Modifier.fillMaxWidth())
                    }
                    if (!inPip && !fullScreen) Row(Modifier.fillMaxWidth().padding(horizontal = 8.dp), horizontalArrangement = Arrangement.SpaceEvenly) {
                        TextButton(onClick = { pageError = ""; webView?.loadUrl(HOME) }, enabled = ready) { Text("Главная") }
                        TextButton(onClick = { if (ready) { pageError = ""; webView?.reload() } else restart() }) { Text("Обновить") }
                        TextButton(onClick = { nextStrategy() }, enabled = ready) { Text("Стратегия") }
                        if (Build.VERSION.SDK_INT >= 26) TextButton(onClick = { enterPip() }, enabled = ready) { Text("В окне") }
                    }
                }
                if (showInfo) InfoDialog()
            }
        }
    }
    @Composable private fun Welcome() {
        Column(Modifier.fillMaxSize().systemBarsPadding().padding(28.dp).verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.Center) {
            Text("↗", style = MaterialTheme.typography.displayLarge, color = MaterialTheme.colorScheme.primary)
            Spacer(Modifier.height(24.dp)); Text("Видео.\nВ своём темпе.", style = MaterialTheme.typography.displaySmall, fontWeight = FontWeight.Bold)
            Spacer(Modifier.height(20.dp)); Text("Разгон открывает мобильную версию YouTube и подбирает подключение для вашей сети.", style = MaterialTheme.typography.bodyLarge)
            Spacer(Modifier.height(24.dp)); Text("Без root и VPN\nТолько трафик внутри приложения\nDNS через зашифрованное соединение\nДвижок останавливается при выходе", lineHeight = 28.sp)
            Spacer(Modifier.height(24.dp)); Text("Работа зависит от оператора. Блокировку по IP и белые списки обойти может быть невозможно.", style = MaterialTheme.typography.bodySmall)
            Spacer(Modifier.height(28.dp))
            Button(onClick = {
                preferences.edit().putBoolean("onboarded", true).apply(); accepted = true; restart()
                if (Build.VERSION.SDK_INT >= 33) requestPermissions(arrayOf(Manifest.permission.POST_NOTIFICATIONS), 7)
            }, modifier = Modifier.fillMaxWidth().height(54.dp)) { Text("Открыть YouTube") }
        }
    }
    @Composable private fun ErrorOrLoading() {
        val isError = status.state in listOf("blocked", "offline", "error") || pageError.isNotEmpty()
        Surface(Modifier.fillMaxSize()) {
            Column(Modifier.padding(32.dp), verticalArrangement = Arrangement.Center, horizontalAlignment = Alignment.CenterHorizontally) {
                if (!isError) CircularProgressIndicator()
                Spacer(Modifier.height(20.dp))
                Text(when (status.state) { "blocked" -> "В вашей сети обход сейчас недоступен"; "offline" -> "Вы не в сети"; "error" -> "Не удалось подключиться"; else -> if (pageError.isNotEmpty()) "Проверяем подключение" else "Готовим подключение" }, style = MaterialTheme.typography.headlineSmall)
                Spacer(Modifier.height(12.dp))
                Text(when (status.state) { "blocked" -> "Проверены все доступные стратегии. Возможна блокировка по IP или белый список. Попробуйте другую сеть или повторите позже."; "offline" -> "Подключитесь к Wi-Fi или мобильной сети. Приложение продолжит автоматически."; "error" -> status.error; else -> pageError.ifEmpty { "Подбираем стратегию. Обычно это занимает несколько секунд." } })
                if (isError) { Spacer(Modifier.height(20.dp)); Button(onClick = { restart() }) { Text("Повторить") } }
            }
        }
    }
    @Composable private fun InfoDialog() {
        AlertDialog(onDismissRequest = { showInfo = false }, title = { Text("Разгон · ${BuildConfig.VERSION_NAME}") }, text = {
            Column(Modifier.verticalScroll(rememberScrollState())) {
                Text("Трафик: ${String.format(Locale.US, "%.1f", status.bytes / 1048576.0)} МБ\nСтратегий проверено: ${status.attempts}\n${status.update}")
                Spacer(Modifier.height(16.dp)); Text("Индикатор показывает скорость первых чанков видео. Для сравнения «до / после» запустите видео и нажмите кнопку ниже. Замер загрузит два небольших фрагмента: напрямую и с Разгоном.")
                Button(onClick = {
                    measuring = true
                    lifecycleScope.launch {
                        try {
                            val result = JSONObject(controller.measure(lastVideoUrl))
                            fun speed(key: String): String = result.optDouble(key).takeIf { it > 0 }?.let { String.format(Locale.US, "%.2f Мбит/с", it * 8 / 1_000_000) } ?: "нет данных"
                            benchmark = "До: ${speed("Before")}\nС Разгоном: ${speed("After")}\n${result.optString("Error")}".trim()
                        } catch (_: Exception) { benchmark = "Замер прерван. Повторите при воспроизведении видео." }
                        finally { measuring = false }
                    }
                }, enabled = ready && !measuring && lastVideoUrl.isNotEmpty()) { Text(if (measuring) "Измеряем…" else "Сравнить скорость") }
                if (benchmark.isNotEmpty()) Text(benchmark)
                Spacer(Modifier.height(16.dp)); Text("Образовательный open-source проект. Не связан с Google или YouTube. Вход в Google во встроенном браузере может быть ограничен самим Google.")
                Spacer(Modifier.height(16.dp)); Text("ByeDPI · MIT, © 2024 hufrea\nFlowseal / zapret · MIT\nGo · BSD-3-Clause\nAndroidX / Compose · Apache-2.0\nПолные лицензии включены в APK и репозиторий.")
                Spacer(Modifier.height(16.dp))
                TextButton(onClick = { CookieManager.getInstance().removeAllCookies(null); CookieManager.getInstance().flush(); webView?.clearCache(true); showInfo = false; restart(resetToHome = true) }) { Text("Очистить данные сайтов") }
            }
        }, confirmButton = { TextButton(onClick = { showInfo = false }) { Text("Понятно") } })
    }
    companion object {
        private const val HOME = "https://m.youtube.com"
        private fun allowedPage(uri: Uri): Boolean {
            val host = uri.host?.lowercase() ?: return false
            return uri.scheme == "https" && listOf("youtube.com", "youtu.be", "google.com").any { host == it || host.endsWith(".$it") }
        }
        private fun statusText(state: String) = when (state) { "ready" -> "Подключение активно"; "selecting" -> "Подбираем стратегию"; "blocked" -> "Стратегии не сработали"; "offline" -> "Нет подключения"; "error" -> "Ошибка подключения"; else -> "Подключаемся…" }
    }
}
