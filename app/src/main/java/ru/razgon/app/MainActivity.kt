package ru.razgon.app

import android.Manifest
import android.app.Activity
import android.content.Intent
import android.content.ClipData
import android.content.ClipboardManager
import android.content.SharedPreferences
import android.content.pm.PackageManager
import android.net.Uri
import android.net.VpnService
import android.os.Build
import android.os.Bundle
import android.os.PowerManager
import android.provider.Settings
import android.widget.Toast
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.*
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.drawscope.scale
import androidx.compose.ui.graphics.vector.PathParser
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.core.content.ContextCompat
import androidx.core.view.WindowCompat
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.lifecycleScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

class MainActivity : ComponentActivity() {
    private val store by lazy { SettingsStore(this) }
    private var config by mutableStateOf(PluginSettings())
    private var batteryAllowed by mutableStateOf(false)
    private var dialog by mutableStateOf("")
    private var message by mutableStateOf("")
    private var licenseText by mutableStateOf("")
    private val listener = SharedPreferences.OnSharedPreferenceChangeListener { _, _ -> config = store.read() }
    private val permission = registerForActivityResult(ActivityResultContracts.StartActivityForResult()) {
        if (it.resultCode == Activity.RESULT_OK) enableConnection()
        else message = "Подключение не включено: Android не получил разрешение на локальный VPN."
    }
    private val notifications = registerForActivityResult(ActivityResultContracts.RequestPermission()) { requestVpn() }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        config = store.read()
        store.preferences.registerOnSharedPreferenceChangeListener(listener)
        lifecycleScope.launch {
            PluginState.strategies.value = withContext(Dispatchers.IO) { EngineController(this@MainActivity).strategies() }
            withContext(Dispatchers.IO) { ConnectionCheck.load(this@MainActivity) }
        }
        setContent { Dashboard() }
        handleIntent(intent)
    }
    override fun onNewIntent(intent: Intent) { super.onNewIntent(intent); handleIntent(intent) }
    private fun handleIntent(intent: Intent) { if (intent.getBooleanExtra("enable", false)) { intent.removeExtra("enable"); requestEnable() } }
    override fun onResume() {
        super.onResume()
        config = store.read()
        batteryAllowed = getSystemService(PowerManager::class.java).isIgnoringBatteryOptimizations(packageName)
        if (config.enabled) {
            if (VpnService.prepare(this) == null) runCatching { PluginVpnService.start(this) }.onFailure { message = it.message ?: "Не удалось запустить подключение" }
            else { store.boolean("enabled", false); message = "Разрешение VPN отозвано. Включите подключение снова." }
        }
    }
    override fun onDestroy() { store.preferences.unregisterOnSharedPreferenceChangeListener(listener); super.onDestroy() }

    private fun requestEnable() {
        if (runCatching { packageManager.getApplicationInfo(PluginVpnService.YOUTUBE, 0) }.isFailure) {
            message = "Для работы плагина установите официальное приложение YouTube."; return
        }
        if (Build.VERSION.SDK_INT >= 33 && ContextCompat.checkSelfPermission(this, Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED) notifications.launch(Manifest.permission.POST_NOTIFICATIONS)
        else requestVpn()
    }
    private fun requestVpn() {
        val intent = VpnService.prepare(this)
        if (intent != null) permission.launch(intent) else enableConnection()
    }
    private fun enableConnection() {
        store.boolean("enabled", true)
        PluginState.mutable.value = ConnectionState(state = "starting")
        runCatching { PluginVpnService.start(this) }.onFailure { store.boolean("enabled", false); message = it.message ?: "Не удалось включить подключение" }
    }
    private fun setBoolean(key: String, value: Boolean) { store.boolean(key, value) }
    private fun setChoice(key: String, value: String) {
        store.text(key, value)
        if (key in listOf("strategy", "dns") && config.enabled) PluginVpnService.start(this, reconfigure = true)
        dialog = ""
    }
    private fun launch(intent: Intent) { runCatching { startActivity(intent) }.onFailure { message = "Android не смог открыть этот экран." } }
    private fun openYoutube() {
        val intent = packageManager.getLaunchIntentForPackage(PluginVpnService.YOUTUBE)
        if (intent != null) launch(intent) else message = "Установите официальное приложение YouTube."
    }
    private fun diagnosticsReport(): String {
        val state = PluginState.mutable.value
        return "Youtube Plugin ${BuildConfig.VERSION_NAME} (${BuildConfig.VERSION_CODE})\nAndroid ${Build.VERSION.RELEASE} (API ${Build.VERSION.SDK_INT})\n${Build.MANUFACTURER} ${Build.MODEL}\nСостояние: ${state.state}\nСтратегия: ${config.strategy}\nDNS: ${config.dns}\n${state.error}\n${PluginState.diagnostics}\n\nПроверка YouTube:\n${ConnectionCheck.state.value.report.ifEmpty { "Ещё не запускалась" }}"
    }
    private fun copyDiagnostics() {
        getSystemService(ClipboardManager::class.java).setPrimaryClip(ClipData.newPlainText("Youtube Plugin", diagnosticsReport()))
        Toast.makeText(this, "Отчёт скопирован", Toast.LENGTH_SHORT).show()
    }
    private fun shareDiagnostics() {
        val report = diagnosticsReport()
        launch(Intent.createChooser(Intent(Intent.ACTION_SEND).apply { type = "text/plain"; putExtra(Intent.EXTRA_TEXT, report) }, "Отчёт о подключении"))
    }
    @Composable private fun Dashboard() {
        val dark = when (config.theme) { "dark" -> true; "light" -> false; else -> isSystemInDarkTheme() }
        SideEffect {
            WindowCompat.getInsetsController(window, window.decorView).apply {
                isAppearanceLightStatusBars = !dark
                isAppearanceLightNavigationBars = !dark
            }
        }
        val scheme = if (dark) darkColorScheme(primary = Color(0xFFAAA5FF), onPrimary = Color(0xFF201A70),
            primaryContainer = Color(0xFF2D2A51), background = Color(0xFF101117), surface = Color(0xFF1B1D27),
            surfaceVariant = Color(0xFF252735), onSurface = Color(0xFFF3F2FA), onSurfaceVariant = Color(0xFFAAAABA), outlineVariant = Color(0xFF30323E))
        else lightColorScheme(primary = Color(0xFF6156F6), onPrimary = Color.White, primaryContainer = Color(0xFFEFEDFF),
            background = Color(0xFFF6F7FC), surface = Color.White, surfaceVariant = Color(0xFFF3F4F9),
            onSurface = Color(0xFF101322), onSurfaceVariant = Color(0xFF73788C), outlineVariant = Color(0xFFE9EBF3))
        MaterialTheme(colorScheme = scheme) {
            val state by PluginState.connection.collectAsStateWithLifecycle()
            val choices by PluginState.strategies.collectAsStateWithLifecycle()
            val check by ConnectionCheck.state.collectAsStateWithLifecycle()
            Surface(Modifier.fillMaxSize(), color = scheme.background) {
                Column(Modifier.fillMaxSize().systemBarsPadding().verticalScroll(rememberScrollState()).padding(horizontal = 20.dp).padding(bottom = 24.dp)) {
                    Row(Modifier.fillMaxWidth().padding(top = 18.dp, bottom = 22.dp), verticalAlignment = Alignment.CenterVertically) {
                        Text("Youtube Plugin", fontSize = 28.sp, fontWeight = FontWeight.Bold, modifier = Modifier.weight(1f))
                        IconButton(onClick = { dialog = "theme" }, modifier = Modifier.semantics { contentDescription = "Оформление" }) { Glyph(if (dark) "moon" else "sun", color = scheme.onSurfaceVariant) }
                    }
                    TrafficCard(state)
                    SectionLabel("Сеть")
                    SettingsCard {
                        SettingRow("wifi", "Стратегия", if (config.strategy == "auto") "Подбор для вашей сети" else state.strategy.ifEmpty { "Ручной выбор" },
                            value = if (config.strategy == "auto") "Автоподбор" else "Вручную", onClick = { dialog = "strategy" })
                        RowDivider()
                        SettingRow("shield", "DNS", "Зашифрованные запросы", value = dnsName(config.dns), onClick = { dialog = "dns" })
                        RowDivider()
                        SettingRow("pulse", "Проверка YouTube", "Доступность и видеотрафик",
                            value = if (check.running) "${check.remaining} с" else "Проверить", onClick = { dialog = "check" })
                    }
                    SectionLabel("Работа приложения")
                    SettingsCard {
                        SettingRow("power", "Автозапуск", "После перезагрузки телефона", checked = config.autoStart, onToggle = { setBoolean("autoStart", it) })
                        RowDivider()
                        SettingRow("refresh", "Восстановление связи", "При смене сети и обрывах", checked = config.reconnect, onToggle = { setBoolean("reconnect", it) })
                        RowDivider()
                        SettingRow("battery", "Работа в фоне", "Ограничения батареи", value = if (batteryAllowed) "Разрешена" else "Настроить", onClick = { dialog = "battery" })
                        RowDivider()
                        SettingRow("bell", "Скорость в уведомлении", checked = config.notificationSpeed, onToggle = { setBoolean("notificationSpeed", it) })
                        RowDivider()
                        SettingRow("palette", "Оформление", value = themeName(config.theme), onClick = { dialog = "theme" })
                    }
                    Spacer(Modifier.height(16.dp))
                    SettingsCard {
                        SettingRow("play", "Открыть YouTube", onClick = { openYoutube() })
                        RowDivider()
                        SettingRow("info", "О приложении", value = BuildConfig.VERSION_NAME, onClick = { dialog = "about" })
                    }
                }
                when (dialog) {
                    "theme" -> ChoiceDialog("Оформление", config.theme, listOf("system" to "Как в системе", "light" to "Светлое", "dark" to "Тёмное")) { setChoice("theme", it) }
                    "dns" -> ChoiceDialog("Зашифрованный DNS", config.dns, listOf("auto" to "Автоматически", "google" to "Google", "cloudflare" to "Cloudflare")) { setChoice("dns", it) }
                    "strategy" -> ChoiceDialog("Стратегия подключения", config.strategy, choices.map { it.id to it.name }) { setChoice("strategy", it) }
                    "battery" -> AlertDialog(onDismissRequest = { dialog = "" }, title = { Text("Работа в фоне") }, text = {
                        Text("Чтобы Android не прерывал подключение, разрешите Youtube Plugin работать без ограничений батареи. На некоторых телефонах также нужно разрешить автозапуск в настройках производителя.")
                    }, confirmButton = { TextButton(onClick = { dialog = ""; launch(Intent(Settings.ACTION_IGNORE_BATTERY_OPTIMIZATION_SETTINGS)) }) { Text("Настройки батареи") } }, dismissButton = { TextButton(onClick = { dialog = "" }) { Text("Закрыть") } })
                    "about" -> AlertDialog(onDismissRequest = { dialog = "" }, title = { Text("Youtube Plugin ${BuildConfig.VERSION_NAME}") }, text = {
                        Column(Modifier.verticalScroll(rememberScrollState())) {
                            Text("Open-source приложение для обхода ограничений YouTube. Включите подключение и смотрите видео в обычном YouTube со своим аккаунтом.")
                            Spacer(Modifier.height(12.dp))
                            Text("Локальный VPN обрабатывает трафик YouTube на телефоне. Внешнего сервера нет, IP остаётся прежним. HTTPS проверяет и расшифровывает сам YouTube.")
                            Spacer(Modifier.height(12.dp))
                            Text("Подключение сохраняется после закрытия этого экрана. Принудительная остановка Android, отзыв разрешения или другой VPN отключают сервис. Результат обхода зависит от оператора.")
                            TextButton(onClick = { dialog = "diagnostics" }) { Text("Диагностика подключения") }
                            TextButton(onClick = { dialog = "check" }) { Text("Проверить работу YouTube") }
                            TextButton(onClick = { launch(Intent(Intent.ACTION_VIEW, Uri.parse("https://github.com/sonjiwu2/youtube-plugin-android"))) }) { Text("Репозиторий на GitHub") }
                            TextButton(onClick = {
                                lifecycleScope.launch {
                                    licenseText = withContext(Dispatchers.IO) { assets.list("licenses").orEmpty().sorted().joinToString("\n\n────────\n\n") { name -> assets.open("licenses/$name").bufferedReader().use { it.readText() } } }
                                    dialog = "licenses"
                                }
                            }) { Text("Лицензии компонентов") }
                        }
                    }, confirmButton = { TextButton(onClick = { dialog = "" }) { Text("Закрыть") } })
                    "licenses" -> AlertDialog(onDismissRequest = { dialog = "" }, title = { Text("Лицензии") }, text = { Text(licenseText, Modifier.verticalScroll(rememberScrollState()), fontSize = 12.sp) }, confirmButton = { TextButton(onClick = { dialog = "" }) { Text("Закрыть") } })
                    "check" -> AlertDialog(onDismissRequest = { dialog = "" }, title = { Text("Проверка YouTube") }, text = {
                        Column(Modifier.verticalScroll(rememberScrollState())) {
                            Text(if (check.running) "Идёт проверка · осталось ${check.remaining} с" else check.summary, fontWeight = FontWeight.SemiBold)
                            Spacer(Modifier.height(12.dp))
                            if (check.report.isNotEmpty()) {
                                Text(ConnectionCheck.details(check.report))
                                TextButton(onClick = { copyDiagnostics() }) { Text("Скопировать отчёт") }
                                Spacer(Modifier.height(12.dp))
                            }
                            Text("После запуска выберите ролик в обычном YouTube и оставьте его воспроизводиться 45 секунд. Затем вернитесь сюда и отправьте отчёт. Проверка продолжится в фоне.")
                            Spacer(Modifier.height(8.dp))
                            Text("Отчёт не содержит аккаунт, ссылки на ролики или содержимое трафика.", fontSize = 12.sp)
                            if (!config.enabled) Text("Сначала включите подключение.", color = scheme.error)
                        }
                    }, confirmButton = { TextButton(enabled = config.enabled && state.state !in listOf("stopped", "starting", "error"), onClick = {
                        if (!check.running) PluginVpnService.check(this@MainActivity)
                        openYoutube()
                    }) { Text(if (check.running) "Открыть YouTube" else "Начать и открыть YouTube") } }, dismissButton = {
                        Row {
                            if (check.report.isNotEmpty()) TextButton(onClick = { shareDiagnostics() }) { Text("Отправить отчёт") }
                            TextButton(onClick = { dialog = "" }) { Text("Закрыть") }
                        }
                    })
                    "diagnostics" -> AlertDialog(onDismissRequest = { dialog = "" }, title = { Text("Диагностика") }, text = {
                        Text(PluginState.diagnostics, Modifier.verticalScroll(rememberScrollState()), fontSize = 11.sp)
                    }, confirmButton = { TextButton(onClick = { shareDiagnostics() }) { Text("Поделиться") } }, dismissButton = { TextButton(onClick = { dialog = "" }) { Text("Закрыть") } })
                }
                if (message.isNotEmpty()) AlertDialog(onDismissRequest = { message = "" }, title = { Text("Youtube Plugin") }, text = { Text(message) }, confirmButton = { TextButton(onClick = { message = "" }) { Text("Понятно") } })
            }
        }
    }

    @Composable private fun TrafficCard(state: ConnectionState) {
        val colors = MaterialTheme.colorScheme
        Surface(shape = RoundedCornerShape(26.dp), color = colors.surface, modifier = Modifier.fillMaxWidth()) {
            Column(Modifier.padding(20.dp)) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Column(Modifier.weight(1f)) {
                        Text("Подключение", fontWeight = FontWeight.Bold, fontSize = 23.sp)
                        Spacer(Modifier.height(5.dp))
                        Text(if (state.state == "ready" && config.enabled) "Трафик YouTube · HTTPS" else stateText(if (config.enabled && state.state == "stopped") "starting" else state.state), color = colors.onSurfaceVariant, fontSize = 13.sp)
                    }
                    Switch(checked = config.enabled, onCheckedChange = { if (it) requestEnable() else PluginVpnService.disable(this@MainActivity) }, colors = SwitchDefaults.colors(checkedThumbColor = Color.White), modifier = Modifier.semantics { contentDescription = "Подключение YouTube" })
                }
                if (state.error.isNotEmpty() && config.enabled) {
                    Spacer(Modifier.height(12.dp))
                    Text(state.error, color = colors.error, fontSize = 12.sp, maxLines = 4)
                }
                Spacer(Modifier.height(22.dp))
                Row(Modifier.fillMaxWidth()) {
                    SpeedMetric("down", "Приём", state.downSpeed, colors.primary, Modifier.weight(1f))
                    SpeedMetric("up", "Передача", state.upSpeed, Color(0xFF16B9B1), Modifier.weight(1f))
                }
                Spacer(Modifier.height(18.dp))
                TrafficGraph(state.history, colors.primary)
                Spacer(Modifier.height(18.dp))
                HorizontalDivider(color = colors.outlineVariant)
                Spacer(Modifier.height(14.dp))
                Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                    Text("За сеанс ", fontSize = 12.sp, color = colors.onSurfaceVariant)
                    Text(sizeText(state.downloaded + state.uploaded), fontWeight = FontWeight.SemiBold, fontSize = 12.sp, modifier = Modifier.weight(1f))
                    Glyph("clock", 16, colors.onSurfaceVariant)
                    Spacer(Modifier.width(5.dp))
                    Text(durationText(state.seconds), color = colors.onSurfaceVariant, fontSize = 12.sp)
                }
            }
        }
    }
    @Composable private fun SpeedMetric(icon: String, label: String, bytes: Double, color: Color, modifier: Modifier) {
        Row(modifier, verticalAlignment = Alignment.CenterVertically) {
            Box(Modifier.size(35.dp).background(color.copy(alpha = .08f), CircleShape), contentAlignment = Alignment.Center) { Glyph(icon, 23, color) }
            Spacer(Modifier.width(9.dp))
            Column {
                Text(label, color = MaterialTheme.colorScheme.onSurfaceVariant, fontSize = 12.sp)
                Row(verticalAlignment = Alignment.Bottom) {
                    Text(speedText(bytes), fontSize = 22.sp, fontWeight = FontWeight.Bold)
                    Text(" Мбит/с", fontSize = 10.sp, modifier = Modifier.padding(bottom = 3.dp))
                }
            }
        }
    }
    @Composable private fun ChoiceDialog(title: String, selected: String, choices: List<Pair<String, String>>, choose: (String) -> Unit) {
        AlertDialog(onDismissRequest = { dialog = "" }, title = { Text(title) }, text = {
            Column(Modifier.verticalScroll(rememberScrollState())) {
                choices.forEach { (id, name) -> Row(Modifier.fillMaxWidth().clickable { choose(id) }.padding(vertical = 4.dp), verticalAlignment = Alignment.CenterVertically) {
                    RadioButton(selected == id, onClick = { choose(id) }); Text(name, fontSize = 15.sp)
                } }
            }
        }, confirmButton = { TextButton(onClick = { dialog = "" }) { Text("Закрыть") } })
    }
}

private fun dnsName(value: String) = when (value) { "google" -> "Google"; "cloudflare" -> "Cloudflare"; else -> "Автоматически" }
private fun themeName(value: String) = when (value) { "dark" -> "Тёмное"; "light" -> "Светлое"; else -> "Системное" }
@Composable private fun SectionLabel(text: String) { Text(text, Modifier.padding(start = 8.dp, top = 25.dp, bottom = 12.dp), color = MaterialTheme.colorScheme.onSurfaceVariant, fontWeight = FontWeight.SemiBold, fontSize = 15.sp) }
@Composable private fun SettingsCard(content: @Composable ColumnScope.() -> Unit) { Surface(shape = RoundedCornerShape(23.dp), color = MaterialTheme.colorScheme.surface) { Column(Modifier.fillMaxWidth(), content = content) } }
@Composable private fun RowDivider() { HorizontalDivider(Modifier.padding(horizontal = 17.dp), color = MaterialTheme.colorScheme.outlineVariant, thickness = .7.dp) }
@Composable private fun SettingRow(icon: String, title: String, subtitle: String = "", value: String = "", checked: Boolean? = null, onToggle: ((Boolean) -> Unit)? = null, onClick: (() -> Unit)? = null) {
    val colors = MaterialTheme.colorScheme
    Row(Modifier.fillMaxWidth().heightIn(min = 75.dp).clickable { if (checked != null) onToggle?.invoke(!checked) else onClick?.invoke() }.padding(horizontal = 16.dp, vertical = 14.dp), verticalAlignment = Alignment.CenterVertically) {
        Box(Modifier.size(38.dp).background(colors.surfaceVariant, CircleShape), contentAlignment = Alignment.Center) { Glyph(icon, 22, colors.onSurfaceVariant) }
        Spacer(Modifier.width(13.dp))
        Column(Modifier.weight(1f)) {
            Text(title, fontWeight = FontWeight.SemiBold, fontSize = 15.sp, lineHeight = 19.sp)
            if (subtitle.isNotEmpty()) { Spacer(Modifier.height(3.dp)); Text(subtitle, color = colors.onSurfaceVariant, fontSize = 11.sp, lineHeight = 15.sp) }
        }
        Spacer(Modifier.width(8.dp))
        if (checked != null) Switch(checked, onCheckedChange = onToggle, colors = SwitchDefaults.colors(checkedThumbColor = Color.White), modifier = Modifier.semantics { contentDescription = title })
        else {
            if (value.isNotEmpty()) Text(value, fontSize = 11.sp, color = colors.onSurfaceVariant, modifier = Modifier.widthIn(max = 100.dp))
            Spacer(Modifier.width(4.dp)); Glyph("chevron", 16, colors.onSurfaceVariant)
        }
    }
}
@Composable private fun TrafficGraph(samples: List<Float>, color: Color) {
    Canvas(Modifier.fillMaxWidth().height(56.dp).semantics { contentDescription = "График приёма трафика за последнюю минуту" }) {
        val values = List((60 - samples.size).coerceAtLeast(0)) { 0f } + samples.takeLast(60)
        val maximum = (values.maxOrNull() ?: 0f).coerceAtLeast(125_000f)
        val line = Path()
        values.forEachIndexed { index, value ->
            val x = index * size.width / 59f
            val y = size.height - 2 - value / maximum * (size.height - 6)
            if (index == 0) line.moveTo(x, y) else line.lineTo(x, y)
        }
        val fill = Path().apply { addPath(line); lineTo(size.width, size.height); lineTo(0f, size.height); close() }
        drawPath(fill, Brush.verticalGradient(listOf(color.copy(alpha = .19f), color.copy(alpha = .02f))))
        drawPath(line, color, style = Stroke(2.dp.toPx(), cap = StrokeCap.Round, join = StrokeJoin.Round))
    }
}
@Composable private fun Glyph(name: String, dimension: Int = 24, color: Color = MaterialTheme.colorScheme.onSurface) {
    val data = when (name) {
        "wifi" -> "M3,8 Q12,0 21,8 M6,12 Q12,6 18,12 M9,16 Q12,13 15,16 M12,20 L12,20.1"
        "shield" -> "M12,3 L20,6 V12 Q20,18 12,22 Q4,18 4,12 V6 Z"
        "pulse" -> "M2,12 H7 L10,4 L14,20 L17,12 H22"
        "power" -> "M12,2 V12 M6,5 A9,9 0,1 0,18 5"
        "refresh" -> "M20,10 A8,8 0,0 0,5 6 M5,2 V7 H10 M4,14 A8,8 0,0 0,19 18 M19,22 V17 H14"
        "battery" -> "M9,4 V2 H15 V4 M7,5 H17 V22 H7 Z M10,9 V18 M14,9 V18"
        "bell" -> "M4,18 H20 L18,15 V10 A6,6 0,0 0,6 10 V15 Z M10,21 Q12,23 14,21 M12,2 V4"
        "palette" -> "M12,3 A9,9 0,1 0,12 21 H14 Q16,21 15,18 Q13,15 17,15 H19 Q22,15 21,11 A9,9 0,0 0,12 3 M7,10 L7,10.1 M11,7 L11,7.1 M16,8 L16,8.1 M6,15 L6,15.1"
        "moon" -> "M20,15 A9,9 0,1 1,9 3 A8,8 0,0 0,20 15"
        "sun" -> "M17,12 A5,5 0,1 1,7 12 A5,5 0,1 1,17 12 M12,1 V3 M12,21 V23 M1,12 H3 M21,12 H23 M4,4 L6,6 M18,18 L20,20 M4,20 L6,18 M18,6 L20,4"
        "down" -> "M12,3 V21 M4,13 L12,21 L20,13"
        "up" -> "M12,21 V3 M4,11 L12,3 L20,11"
        "clock" -> "M21,12 A9,9 0,1 1,3 12 A9,9 0,1 1,21 12 M12,6 V12 L16,15"
        "chevron" -> "M9,5 L16,12 L9,19"
        "play" -> "M5,3 H15 M21,9 V19 Q21,21 19,21 H5 Q3,21 3,19 V5 Q3,3 5,3 M9,8 L17,12 L9,16 Z"
        else -> "M21,12 A9,9 0,1 1,3 12 A9,9 0,1 1,21 12 M12,10 V17 M12,7 L12,7.1"
    }
    val path = remember(data) { PathParser().parsePathString(data).toPath() }
    Canvas(Modifier.size(dimension.dp)) { scale(size.width / 24, size.height / 24, pivot = Offset.Zero) { drawPath(path, color, style = Stroke(1.8f, cap = StrokeCap.Round, join = StrokeJoin.Round)) } }
}
