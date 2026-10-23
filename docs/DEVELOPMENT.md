# Разработка и сопровождение Разгона

Мобильный YouTube в Android-приложении со встроенным userspace-движком ByeDPI.
Пакет: `ru.razgon.app`, Android 7.0+. Без root, VpnService и системного VPN-профиля.

**Скачать:** [GitHub Releases](https://github.com/sonjiwu2/youtube-android-zapret/releases/latest).
Для современного телефона выбирайте `arm64-v8a`, при сомнениях — `universal`.
Разрешите установку APK из выбранного браузера/файлового менеджера и откройте приложение.
Обновления APK устанавливаются поверх предыдущей версии: подпись сохраняется.

> Образовательный open-source проект; не является рекламой или распространением услуг
> обхода блокировок. Проект не связан с Google LLC или YouTube. Работа зависит от оператора
> и текущих правил DPI; универсальной гарантии обхода нет.

## Возможности

- WebView открывается только после запуска движка и подтверждения `ProxyController`.
- 18 встроенных стратегий: split, disorder, TLS-record, OOB, Google TLS fake и сочетания.
- Подбор по ошибкам соединений и скорости первых 256 КиБ `googlevideo.com`.
- Кэш успешной стратегии на 7 дней по типу сети, оператору и хэшу DNS/шлюза. SSID и геолокация не нужны.
- DoH Google/Cloudflare с bootstrap IPv4/IPv6, A/AAAA и ограниченным TTL-кэшем.
- Резервные IP только для точных имён фронтендов; адреса CDN не подменяются общим IP Google.
- Подписанные Ed25519 обновления, проверка срока и версии, встроенный резерв.
- Светлая/тёмная темы, полноэкранное видео, PiP на Android 8+, статусное уведомление и плитка запуска.
- Индикатор скорости видео; в «О приложении» есть ручное сравнение одного фрагмента до/после обработки.
- При выходе движок останавливается; при возврате или смене сети запускается заново.

## Сетевая схема

```text
WebView → HTTP CONNECT 127.0.0.1:<случайный порт>
        → Go: DoH, проверка SNI, hostlist, метрики, стратегии
        → SOCKS5 127.0.0.1:<случайный порт>
        → ciadpi → сервер Google
```

Целевые домены идут через ciadpi; остальные через тот же Go CONNECT-прокси без desync.
Все домены разрешаются через DoH. Системный DNS не используется для запросов движка.
`addDirect()` и неявные исключения localhost в WebView отключены. При остановке override
остаётся направленным на закрытый порт, поэтому прямого резервного пути у WebView нет.
TLS не расшифровывается, пользовательские сертификаты не устанавливаются.
Единственный отдельный прямой запрос — явно запущенный пользователем замер скорости «до»;
он также использует DoH, ограничен видеофрагментом и не сохраняет URL.

Если все стратегии исчерпаны, приложение показывает конечный экран ошибки. При отсутствии
сети показывает офлайн-состояние и продолжает после восстановления подключения.
Страница, загруженная с помощью desync, ещё не доказывает разгон конкретного видеопотока:
проверьте воспроизведение в своей сети.

## Сборка

Зафиксированный стек: JDK 17, Go 1.26.3, Gradle wrapper 9.8.2, AGP 9.4.1,
Android SDK 37, targetSdk 36, NDK `28.2.13676358`, CMake `3.31.6`.
Версия gomobile закреплена в скриптах и `core/go.mod`.

Установите SDK, NDK и CMake через Android Studio / SDK Manager. На новых Android CLI:

```powershell
android sdk install ndk/28.2.13676358
android sdk install cmake/3.31.6
android sdk install platforms/android-37.0
```

Windows PowerShell, из корня репозитория:

```powershell
$env:JAVA_HOME = 'C:\Program Files\Java\jdk-17'
$env:ANDROID_HOME = "$env:LOCALAPPDATA\Android\Sdk"
cd core
go vet ./...
go test -race ./...
cd ..
.\scripts\build-engine.ps1
.\gradlew.bat :app:assembleDebug :app:lintDebug
```

Linux/macOS с установленным SDK:

```bash
export ANDROID_HOME="$HOME/Android/Sdk"
bash scripts/install-sdk.sh
(cd core && go vet ./... && go test -race ./...)
bash scripts/build-engine.sh
bash gradlew :app:assembleDebug :app:lintDebug
```

`app/build/outputs/apk/debug/app-debug.apk` — результат debug-сборки.
Native/Go артефакты генерируются локально и не коммитятся.
Отдельная инструкция проверки ядра на ПК: [docs/PC-TEST.md](docs/PC-TEST.md).

## Release и CI

На push в `main` и PR GitHub Actions запускает Go vet, race-тесты, native/gomobile сборку,
Android lint и debug-сборку. По тегу `v*` публикует один подписанный universal APK,
который включает три ABI. При обновлении выпуска прежние файлы убираются.

Для своей копии репозитория создайте постоянный JKS с alias `razgon` и задайте GitHub Secrets:

- `ANDROID_KEYSTORE_BASE64` — keystore в Base64;
- `ANDROID_KEYSTORE_PASSWORD` — одинаковый пароль хранилища и alias.

Приватные ключи и пароли в Git не хранятся. В основном репозитории ключ CI уже настроен.
Для локального выпуска задайте `ANDROID_KEYSTORE_FILE`, `ANDROID_KEYSTORE_PASSWORD`:

```powershell
.\gradlew.bat :app:assembleRelease -PsplitApk
```

Без ключа release остаётся неподписанным; debug-ключ для production не подставляется.
Не теряйте свой keystore: без него обновление ранее установленных APK невозможно.

## Обновление стратегий без APK

Приложение использует стабильный адрес
`https://raw.githubusercontent.com/sonjiwu2/youtube-android-zapret/main/updates/strategies.json`
и соседний файл `strategies.json.sig`. Публичный ключ — `app/src/main/assets/config-public.txt`.
Проверка выполняется при запуске и каждые 6 часов, пока движок активен; подписанный кэш
проверяется заново при каждом запуске. Новые стратегии добавляются без сброса текущего видео.
Изменения резервных DNS-адресов применяются при следующем запуске движка.

Владелец ключа редактирует `core/presets.json`, затем выполняет:

```powershell
.\scripts\publish-strategies.ps1
```

Скрипт выставляет возрастающую версию и срок 90 дней, подписывает точные байты JSON и
сохраняет JSON и подпись в `updates/`. Для публикации закоммитьте оба файла и отправьте `main`.
Служебный релиз `strategies` скрыт и больше не используется приложением.
По умолчанию скрипт ищет локальный `signing/private-config.key`.
Файлы из `signing/` с секретами игнорируются; резервную копию храните отдельно.

Для собственного форка сначала создайте пару ключей:

```powershell
cd core
go run ./cmd/configsign -generate
```

Затем измените URL в `EngineController.kt`, положите в APK свой публичный ключ и пересоберите.
Приватный Ed25519-ключ не добавляйте ни в Git, ни в APK. Ротация публичного ключа требует
новой версии APK. Подписанные конфиги не могут задавать произвольные пути, команды или
параметры слушателя — допустим только ограниченный набор аргументов desync.

## Ограничения

- UDP, QUIC, WebRTC/голос Discord, другие приложения и системный трафик — вне MVP.
- IP-блокировки и белые списки могут сделать все встроенные стратегии бесполезными.
- Без доступного DoH произвольные имена CDN не разрешатся; запасные IP не заменяют DNS для CDN.
- Вход Google во встроенном браузере может отклоняться самим Google. Приложение не обходит эту политику.
- PiP доступен на поддерживаемых устройствах с Android 8+. При уходе из полноэкранного видео
  приложение переходит в PiP; после закрытия PiP движок останавливается.
- Отключённая история YouTube может давать пустую главную ленту: используйте поиск.
- Первый замер стратегии и скорость зависят от сервера CDN, радиоканала и нагрузки устройства.

## Исходники и лицензии

`core/` — Go-ядро и тесты; `native/` — закреплённые исходники ciadpi и CMake;
`app/` — Kotlin/Compose/WebView; `scripts/` — сборка и подпись;
`docs/RESEARCH.md` — исследование первоисточников; [проверки релиза](docs/VALIDATION.md).

Собственный код — MIT. [LICENSES.md](LICENSES.md), тексты сторонних лицензий включены в APK.
Благодарности [bol-van/zapret](https://github.com/bol-van/zapret),
[Flowseal](https://github.com/Flowseal/zapret-discord-youtube),
[hufrea/ByeDPI](https://github.com/hufrea/byedpi),
[ByeDPIAndroid](https://github.com/dovecoteescapee/ByeDPIAndroid) и
[ByeByeDPI](https://github.com/romanvht/ByeByeDPI). GPL-код Android-портов не заимствован.
