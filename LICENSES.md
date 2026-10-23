# Сторонние компоненты и лицензии

Собственный код проекта распространяется под MIT (см. `LICENSE`).

| Компонент | Откуда | Лицензия | Как используется | Обязательства |
|---|---|---|---|---|
| ciadpi (ByeDPI) | [hufrea/byedpi](https://github.com/hufrea/byedpi) | MIT, © 2024 hufrea | исходники C собираются NDK в исполняемый файл внутри APK | сохранить копирайт и текст лицензии в дистрибутиве; открывать свои исходники **не требуется** |
| TLS-фейки `tls_clienthello_*.bin` | [Flowseal/zapret-discord-youtube](https://github.com/Flowseal/zapret-discord-youtube) | MIT, © 2016-2026 bol-van, © 2024-2026 Flowseal | бинарные данные в assets | сохранить копирайт |
| Списки доменов (идеи состава) | Flowseal, zapret | MIT | собственный hostlist, составленный по их спискам | указать источник |
| Методики desync | [bol-van/zapret](https://github.com/bol-van/zapret) | MIT, © 2016-2024 bol-van | только идеи, код не заимствуется | — |
| AndroidX, Jetpack Compose, Media3 | Google | Apache-2.0 | зависимости Gradle | уведомление в «О приложении» |
| Go stdlib, `golang.org/x/*` | The Go Authors | BSD-3-Clause | зависимости Go-ядра | сохранить уведомление |

## Что сознательно **не** используется

* **ByeDPIAndroid, ByeByeDPI** — GPL-3.0. Их код (JNI, сервисы, Kotlin) не копируется:
  иначе весь APK пришлось бы распространять под GPL-3.0. Из этих проектов взяты только
  архитектурные идеи (ciadpi в отдельном процессе, CMake-сборка под ABI).
* **hev-socks5-tunnel** не нужен: проект не использует VpnService.

## Зафиксированные исходники

ciadpi: `ba532298de7b28cfe854aea83d061369d13ca290` (v17.3), MIT перепроверена перед сборкой.
Полный upstream находится в `native/byedpi/`; локальная CMake-обвязка — в `native/CMakeLists.txt`.
Лицензия также включена в APK: `assets/licenses/byedpi.txt`.

TLS-образец Google и лицензия Flowseal взяты из коммита `176f56d`;
лицензия включена в `assets/licenses/flowseal.txt`. APK содержит также собственную MIT,
BSD-3-Clause Go и Apache-2.0 AndroidX. Исходники GPL Android-портов не включены.
