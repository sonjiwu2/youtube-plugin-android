# Ютуб Mod (кодовое имя «Разгон»)

Android-обёртка над мобильной веб-версией YouTube со встроенным обходом DPI-замедления.
Приложение открывает `m.youtube.com` в WebView, а весь трафик WebView пропускает через
локальный движок внутри своего же процесса. Root, VpnService и системный значок VPN не нужны.

> **Дисклеймер.** Это образовательный open-source проект, который исследует сетевые протоколы
> (TCP/TLS) и методы противодействия DPI. Он не является рекламой и не распространяет услуги
> обхода блокировок. Используя код, вы сами отвечаете за соблюдение законов своей страны.
> Проект не связан с Google LLC и YouTube; названия принадлежат их правообладателям.

## Как это работает

```
WebView (m.youtube.com)
  │  HTTP CONNECT 127.0.0.1:<случайный порт>
  ▼
Go-ядро  ── DoH-резолвер, hostlist, разбор SNI, замер скорости, автоподбор стратегии
  │  SOCKS5 127.0.0.1:<случайный порт>   (только целевые домены YouTube/Google)
  ▼
ciadpi (C) ── desync: split / disorder / OOB / TTL-fake / tlsrec
  ▼
серверы Google
```

* **Работает только для собственных соединений приложения.** Обход поднимается, когда открыта
  вкладка YouTube, и гасится, когда приложение уходит в фон.
* **DNS — только DoH** (Google, Cloudflare, Quad9, AdGuard по зашитым IP). Провайдерский DNS
  не используется никогда.
* **Нецелевые домены идут напрямую**, без desync.
* Подробный разбор методик, на которых всё построено: [docs/RESEARCH.md](docs/RESEARCH.md).

## Структура репозитория

| Путь | Что внутри |
|---|---|
| `app/` | Android-приложение (Kotlin, Jetpack Compose, androidx.webkit) |
| `core/` | Go-ядро: прокси, TLS/SNI, hostlist, DoH, стратегии, подпись. Не зависит от Android и тестируется на ПК |
| `docs/` | Исследование, дорожная карта |

## Сборка (черновик, дополняется по этапам)

Требования: JDK 17+, Android SDK (platform 37, build-tools 36), NDK r28c (`28.2.13676358`),
CMake 3.31, Go 1.22+.

```bash
# Go-ядро и тесты
cd core && go test ./...

# Debug-APK
./gradlew :app:assembleDebug
```

## Ограничения

* **Голос Discord и любой UDP/WebRTC** вне MVP: без VpnService UDP чужих приложений не перехватить.
* **Блокировка по IP** и **белые списки** (часть мобильных сетей) desync не обходит. В таком
  случае приложение честно сообщает, что обход в этой сети невозможен.
* **Стратегии устаревают.** ТСПУ меняется, поэтому стратегии приходят подписанным конфигом
  с GitHub Releases и обновляются без пересборки APK.
* Часть приёмов zapret (seqovl, ip-id, fooling ts/badseq) требует сырых пакетов и root.
  Чем они заменены, описано в [RESEARCH.md](docs/RESEARCH.md#61-что-требует-raw-пакетов-и-чем-заменяем--коротко).

## Благодарности

* [bol-van/zapret](https://github.com/bol-van/zapret): исследование DPI и движки nfqws/tpws.
* [Flowseal/zapret-discord-youtube](https://github.com/Flowseal/zapret-discord-youtube): наборы стратегий, списки доменов, TLS-фейки.
* [hufrea/byedpi](https://github.com/hufrea/byedpi): ciadpi, desync-движок, который работает без root.
* [ByeDPIAndroid](https://github.com/dovecoteescapee/ByeDPIAndroid) и [ByeByeDPI](https://github.com/romanvht/ByeByeDPI): идеи интеграции ciadpi в Android.

## Лицензия

MIT, см. [LICENSE](LICENSE). Сторонние компоненты перечислены в [LICENSES.md](LICENSES.md).
