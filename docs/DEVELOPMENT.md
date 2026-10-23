# Разработка Youtube Plugin

Пакет `ru.razgon.app` сохранён для обновления существующих установок. Начиная с 0.2.0 приложение управляет локальным VPN для официального YouTube; встроенного браузера нет.

## Сетевая схема

```text
Официальный YouTube (отдельный UID)
  → Android VpnService, TUN (IPv4 + IPv6)
  → gVisor netstack / tun2socks adapter
  → Go: DNS, метрики, управление стратегиями
  → SOCKS5 на случайном localhost-порту
  → ciadpi → сервер YouTube
```

В production allowlist содержит только `com.google.android.youtube`. Пустой список запрещён: при отсутствии YouTube сервис выдаёт ошибку. UID плагина исключён из VPN, поэтому его исходящие сокеты и ciadpi не образуют петлю.

- TCP SYN подтверждается только после установки исходящего соединения. Отказ маршрута возвращает RST, позволяя клиенту выбрать другой адрес/семейство IP.
- TCP 443 передаётся в ciadpi без расшифровки TLS. Сертификаты проверяет YouTube. TCP 80 пересылается без desync.
- UDP/TCP 53 обслуживается локально через DoH Google/Cloudflare. Служебные DNS и неподдерживаемые назначения не уходят напрямую.
- UDP, включая QUIC, получает ICMP port-unreachable для перехода клиента на HTTPS/TCP. HTTPS/SVCB DNS-записи возвращаются пустыми.
- Если полученные адреса недоступны, автоматический DNS проверяет другого провайдера и обновляет адреса; явно выбранный DNS не подменяется. Адреса для TCP-подключения берутся только из соответствующего проверенного DNS-ответа.
- Учёт приёма/передачи отражает TLS-потоки локального движка, включая его служебные HTTPS-запросы обновления конфигурации. Отдельная контрольная проба доступности не проходит через эти счётчики. Скорость вычисляется по разности счётчиков и реальному времени, график содержит до 60 секунд.

`PluginVpnService` владеет TUN, Go-движком и foreground-уведомлением. Activity только меняет сохранённые настройки. `START_STICKY` запрашивает восстановление сервиса системой; при таком запуске учитывается сохранённое включение. Срок и возможность перезапуска определяет Android. BootReceiver запускает сервис после загрузки при включённом автозапуске; при обновлении APK сохраняет включённое подключение.

При смене сети движок и соединения пересоздаются, а TUN остаётся открытым. При недоступности сети или исчерпании стратегий автоматическое восстановление ждёт сеть / повторяет попытку с задержкой 15–300 секунд. Ручная стратегия самопроизвольно не заменяется другой. Отключение отменяет запросы и закрывает движок, TUN и уведомление. Канал stdin завершает native-процесс даже при SIGKILL родителя.

Паузы и битрейт нативного видеоплеера не считаются проверкой пропускной способности: работающая стратегия не заменяется только из-за медленной подачи очередного фрагмента.

Принудительная остановка приложения, отзыв VPN-разрешения, другой VPN и ограничения производителя могут остановить сервис. Android Always-on для этой конфигурации не предлагается: тумблер приложения должен сохранять управление отключением.

## Сборка

JDK 17, Go 1.26.3, Gradle 9.8.2, AGP 9.4.1, compileSdk 37, targetSdk 36, NDK `28.2.13676358`, CMake `3.31.6`. Версия gomobile закреплена в скриптах.

Windows, из корня проекта с установленным Android SDK:

```powershell
$env:JAVA_HOME = 'C:\Program Files\Java\jdk-17'
$env:ANDROID_HOME = "$env:LOCALAPPDATA\Android\Sdk"
cd core
go vet ./...
go test -race -timeout 120s ./...
cd ..
.\scripts\build-engine.ps1
.\gradlew.bat :app:assembleDebug :app:lintDebug :qa:probe:assembleDebug
```

Linux:

```bash
bash scripts/install-sdk.sh
(cd core && go vet ./... && go test -race -timeout 120s ./...)
bash scripts/build-engine.sh
bash gradlew :app:assembleDebug :app:lintDebug :qa:probe:assembleDebug
```

## Проверки устройства

`qa/probe` — отдельный тестовый APK с собственным UID, который выполняет DNS/HTTPS-запросы и сохраняет отчёт в `files/report.txt`. Только debug-сборка плагина может добавить его в allowlist; production APK его не обслуживает. Сначала установите probe, затем включите VPN debug-плагина и запустите ProbeActivity после запуска интерфейса TUN.

```text
adb shell am start -n ru.youtubeplugin.probe/.ProbeActivity
adb shell run-as ru.youtubeplugin.probe cat files/report.txt
adb shell run-as ru.razgon.app cat cache/vpn-diagnostics.json
```

Debug-диагностика находится в приватном кэше приложения. В release запись этого файла и тестовый UID отсутствуют. Проверяйте allowlist в `dumpsys connectivity`, рост счётчиков, фон после закрытия Activity, выключение, потерю/возврат сети, перезапуск процесса и загрузку устройства. Тест на конкретном российском операторе выполняется отдельно.

Linux-тест `core/mobile` передаёт настоящие IP-пакеты через socketpair: DNS, отказ QUIC, RST для недоступного назначения и владение файловыми дескрипторами. CI включает race detector.

## Выпуск

Push/PR: Go vet/race, форматирование, native/gomobile, debug/lint и сборка probe. Новый тег `v*` выпускает четыре подписанных APK и отдельный GitHub Release с текстом `docs/RELEASE.md`. Перед тегом согласуйте versionName с тегом и увеличьте versionCode.

Секреты Actions: `ANDROID_KEYSTORE_BASE64`, `ANDROID_KEYSTORE_PASSWORD`; alias `razgon`. Для локального release задайте `ANDROID_KEYSTORE_FILE` и `ANDROID_KEYSTORE_PASSWORD`, затем:

```powershell
.\gradlew.bat :app:assembleRelease -PsplitApk
```

Ключ подписи нужно сохранять для совместимости обновлений. Приватные ключи, пароли, APK, AAR и каталоги сборки в Git не входят.

## Подписанные стратегии

Адрес: `https://raw.githubusercontent.com/sonjiwu2/youtube-plugin-android/main/updates/strategies.json`, подпись — соседний `.sig`. Ed25519 публичный ключ находится в assets/config-public.txt. Проверяются подпись, версия и срок; есть встроенный резерв и проверяемый кэш. Обновления запрашиваются при запуске и каждые шесть часов.

Владелец ключа редактирует `core/presets.json`, запускает `scripts/publish-strategies.ps1`, коммитит JSON и подпись из `updates/`. Скрипт использует игнорируемый `signing/private-config.key`. Удалённая конфигурация ограничена параметрами desync и не может задавать команды или произвольные файлы. Служебные releases для стратегий не нужны.

[Лицензии](../LICENSES.md) · [Проверки выпуска](VALIDATION.md)
