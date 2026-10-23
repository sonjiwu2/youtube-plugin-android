# Исследование: методики обхода DPI-замедления и их перенос в userspace Android

Документ фиксирует, что именно делают первоисточники, какие приёмы из них применимы
в приложении без root и без VpnService, и чем заменяется то, что требует сырых пакетов.
Все выводы сделаны по исходникам, а не по пересказам.

## 0. Изученные источники

| Репозиторий | Коммит | Дата | Лицензия | Роль |
|---|---|---|---|---|
| [Flowseal/zapret-discord-youtube](https://github.com/Flowseal/zapret-discord-youtube) | `176f56d` | 2026-10-20 | MIT | Обвязка для Windows: готовые наборы аргументов `winws`, списки доменов, `.bin`-фейки, тестер стратегий |
| [bol-van/zapret](https://github.com/bol-van/zapret) | `c4d0b19` | 2026-10-04 | MIT | Сам движок: `nfqws`/`winws` (пакетный уровень) и `tpws` (потоковый прокси) |
| [hufrea/byedpi](https://github.com/hufrea/byedpi) | `ba53229` (v17.3) | 2026-03-27 | MIT | `ciadpi`: SOCKS5-прокси с desync на уровне сокетов, без root |
| [dovecoteescapee/ByeDPIAndroid](https://github.com/dovecoteescapee/ByeDPIAndroid) | `43db095` | 2024-09-05 | **GPL-3.0** | Android-обвязка ciadpi (Proxy / VPN через hev-socks5-tunnel) |
| [romanvht/ByeByeDPI](https://github.com/romanvht/ByeByeDPI) | `4ca350c` | 2026-10-14 | **GPL-3.0** | Форк ByeDPIAndroid: тестер стратегий, списки сайтов, отдельный процесс `:native` |

Обсуждения: [zapret #200](https://github.com/bol-van/zapret/discussions/200) «Обход замедления youtube в России»,
[byedpi #184](https://github.com/hufrea/byedpi/discussions/184) «рабочие командные строки для разных операторов»,
[net4people/bbs #490](https://github.com/net4people/bbs/issues/490) про блокировку «TCP 16-20».

> **Лицензионный вывод.** ByeDPIAndroid и ByeByeDPI распространяются под GPL-3.0, поэтому их код
> (JNI-обёртки, сервисы, Kotlin) мы **не копируем**: иначе весь APK пришлось бы открывать под GPL.
> Берём только идеи архитектуры. Ядро ciadpi — MIT, его можно встраивать с сохранением копирайта.
> Наборы аргументов (строки параметров) — функциональные данные; мы составляем свои пресеты
> и указываем, откуда взята каждая методика.

---

## 1. Что именно делает ТСПУ с YouTube (2024–2026)

Сведено из zapret #200, net4people #490 и новостей февраля 2026:

1. **Замедление по SNI.** Под фильтр попадают прежде всего `googlevideo.com` (видеопотоки),
   `youtubei.googleapis.com` (API), `i.ytimg.com`, `yt3.ggpht.com`. DPI читает имя сервера из
   первого TLS-рекорда (ClientHello). Для TLS 1.3 имя есть только в ClientHello, поэтому
   достаточно сломать его разбор: разрез на позиции 1–2 помогает, а разрезы «дальше» — нет
   (вывод автора zapret).
2. **TLS 1.2 хуже.** Имя видно и в сертификате ServerHello; там обход требует фейков или
   разбиения ответа сервера, что снижает скорость. WebView на Android ≥ 7 договаривается
   о TLS 1.3 с Google, так что для нас это второстепенно.
3. **Фейки с Google-SNI.** Часть кэш-серверов Google (GGC внутри сетей операторов) обходится
   только фейками, у которых SNI = `www.google.com`. Отсюда `tls_clienthello_www_google_com.bin`
   у Flowseal.
4. **QUIC.** Фейки QUIC помогают только там, где UDP/443 не режется целиком. У нас WebView
   ходит через HTTP CONNECT-прокси, а Chromium **не использует QUIC через HTTP-прокси**,
   поэтому весь YouTube гарантированно идёт по TCP/TLS, и UDP-стратегии не нужны.
5. **«TCP 16-20».** Отдельный механизм: после ~16–20 КБ (≈25 пакетов) ответа соединение с
   IP зарубежных хостингов (Hetzner, DigitalOcean и т. п.) «замерзает» — не RST, а тишина.
   Фрагментация внутри соединения не помогает. Для серверов Google это проявляется редко,
   но паттерн «пришло 16–20 КБ, дальше тишина» полезен как **признак блокировки** в нашем
   измерителе скорости.
6. **DNS (февраль 2026).** РКН убрал домены YouTube из НСДИ: резолверы, подчинённые НСДИ
   (обычно провайдерские), перестали отдавать корректные адреса. Поэтому DNS обязан идти мимо
   провайдера: DoH с зашитыми bootstrap-IP. Показательно, что Flowseal держит в своём
   hostlist сами DoH-серверы (`^dns.google`, `^cloudflare-dns.com`, …): их тоже приходится
   «разгонять».
7. **Блок по IP / белые списки.** Если операторская сеть пускает только белые списки IP/SNI
   (мобильные сети в отдельных регионах), desync бессилен. Это и есть экран
   «В вашей сети обход невозможен».

---

## 2. Flowseal: устройство обвязки

### 2.1 Структура запуска (`general*.bat`)

Каждый `.bat` запускает один `winws.exe` с несколькими **профилями**, разделёнными `--new`.
Профиль = фильтр (порт, протокол, hostlist или ipset) + стратегия. Для YouTube важны три профиля:

* `--filter-tcp=443 --hostlist=list-google.txt …` — **Google/YouTube по TCP** (главное для нас);
* `--filter-tcp=80,443 --hostlist=list-general.txt …` — Discord, Cloudflare и прочее;
* `--filter-udp=443 … --dpi-desync=fake --dpi-desync-fake-quic=quic_initial_www_google_com.bin` — QUIC.

Ниже — профиль `list-google.txt` из **всех** 22 вариантов (пути и списки опущены;
`ip-id=zero` указан почти везде):

| Вариант | Стратегия для Google, TCP 443 |
|---|---|
| general | `multisplit`, `split-pos=1`, `seqovl=681`, `seqovl-pattern=tls_clienthello_www_google_com.bin` |
| ALT | `fake,fakedsplit`, `repeats=6`, `fooling=ts`, `fakedsplit-pattern=0x00`, `fake-tls=google.bin` |
| ALT2 | `multisplit`, `split-pos=2`, `seqovl=652`, `pattern=google.bin` |
| ALT3 | `fake,hostfakesplit`, `fake-tls-mod=rnd,dupsid,sni=www.google.com`, `hostfakesplit-mod=host=www.google.com,altorder=1`, `fooling=ts` |
| ALT4 | `fake,multisplit`, `repeats=6`, `fooling=badseq`, `badseq-increment=1000`, `fake-tls=google.bin` |
| ALT5 | (TCP-профиля Google нет — только QUIC и general) |
| ALT6 | как general (`multisplit`, `pos=1`, `seqovl=681`, google-pattern) |
| ALT7 | `multisplit`, `split-pos=2,sniext+1`, `seqovl=679`, google-pattern |
| ALT8 | `fake`, `fake-tls-mod=none`, `repeats=6`, `fooling=badseq`, `increment=2` (встроенный фейк) |
| ALT9 | `hostfakesplit`, `repeats=4`, `fooling=ts`, `host=www.google.com` |
| ALT10 | `fake`, `repeats=6`, `fooling=ts`, `fake-tls=google.bin` |
| ALT11 | `fake,multisplit`, `pos=1`, `seqovl=681`, `fooling=ts`, `repeats=8`, google-pattern + google-fake |
| ALT12 | `hostfakesplit`, `fooling=ts`, `host=www.google.com` |
| ALT13 | то же, что ALT12, но **без** `ip-id=zero` |
| EXP | `hostfakesplit`, `fooling=ts`, `host=www.google.com` |
| FAKE TLS AUTO | `fake,multidisorder`, `split-pos=1,midsld`, `repeats=11`, `fooling=badseq`, `fake-tls=0x00000000` + `fake-tls=!` (сам ClientHello), `mod=rnd,dupsid,sni=www.google.com` |
| FAKE TLS AUTO ALT | `fake,fakedsplit`, `pos=1`, `fooling=badseq`, `increment=2`, `repeats=8`, `mod=rnd,dupsid,sni=www.google.com` |
| FAKE TLS AUTO ALT2 | `fake,multisplit`, `pos=1`, `seqovl=681`, `fooling=badseq`, `increment=10000000`, `repeats=8`, google-pattern, `mod=…sni=www.google.com` |
| FAKE TLS AUTO ALT3 | как ALT2, но `fooling=ts` |
| SIMPLE FAKE | `hostfakesplit`, `fooling=ts`, `host=www.google.com` |
| SIMPLE FAKE ALT | `fake`, `repeats=6`, `fooling=badseq`, `increment=2`, `fake-tls=google.bin` |
| SIMPLE FAKE ALT2 | `fake`, `repeats=6`, `fooling=ts`, `fake-tls=google.bin` |

Закономерности:

* **Размер `seqovl` = размеру фейка-паттерна.** `tls_clienthello_www_google_com.bin` весит ровно
  681 байт → `seqovl=681`; `tls_clienthello_4pda_to.bin` — 284 байта → `seqovl=568` (2×284).
  Смысл: перед настоящим ClientHello в ту же последовательность подкладывается целиком
  «чужой» ClientHello с SNI `www.google.com`. Сервер его отбрасывает (данные вне окна),
  а DPI видит Google.
* Почти все варианты — комбинации трёх идей: **(а)** показать DPI фейковый ClientHello с SNI
  Google и не дать ему дойти до сервера; **(б)** разрезать настоящий ClientHello у самого
  начала (позиция 1–2) или по SNI; **(в)** перемешать порядок сегментов.
* Способы «не дать фейку дойти» (`fooling`): `ts` (испорченный TCP timestamp), `badseq`
  (номер последовательности вне окна), `md5sig`, `ttl`/`autottl`. Все, кроме TTL,
  требуют сырых пакетов.

### 2.2 `.bin`-фейки (`bin/`)

| Файл | Размер | Что это | Где применяется |
|---|---|---|---|
| `tls_clienthello_www_google_com.bin` | 681 B | TLS ClientHello, SNI `www.google.com` | фейк и seqovl-паттерн для Google-профиля |
| `tls_clienthello_4pda_to.bin` | 284 B | ClientHello, SNI `4pda.to` | seqovl-паттерн general-профиля |
| `tls_clienthello_www_sferum_ru.bin` | 661 B | ClientHello, SNI `www.sferum.ru` (российский, «белый») | fake-http/fake-tls в ALT-вариантах |
| `tls_clienthello_sochi_park.bin` | 244 B | ClientHello, SNI `*.mail.ru` | ALT13 |
| `quic_initial_www_google_com.bin` | 1200 B | QUIC Initial с SNI Google | UDP 443 (нам не нужен) |
| `stun.bin`, `stun2.bin`, `ACTIVE_*_UDP.bin` | — | STUN/Discord/игровые UDP | голос Discord и игры (вне MVP) |

Для нас полезны TLS-фейки: в ciadpi их передаём через `-l <файл>` (`--fake-data`),
а подмену SNI делаем ключом `-n www.google.com` (`--fake-sni`).

### 2.3 Списки (`lists/`)

* `list-google.txt` (22 строки) — ровно то, что нужно YouTube: `googlevideo.com`, `youtube.com`,
  `youtu.be`, `youtubei.googleapis.com`, `youtube.googleapis.com`, `ytimg.com`, `ytimg.l.google.com`,
  `yt3.ggpht.com`, `yt4.ggpht.com`, `yt3.googleusercontent.com`, `jnn-pa.googleapis.com`,
  `youtube-nocookie.com`, `youtubekids.com`, `youtubeembeddedplayer.googleapis.com`,
  `wide-youtube.l.google.com`, `youtube-ui.l.google.com`, `yt-video-upload.l.google.com`,
  `play.google.com`, `fonts.googleapis.com`, `www.gstatic.com`, `google.ru` (+ discordapp).
* `list-general.txt` — Discord, Cloudflare и DoH-серверы. Префикс `^` означает
  **точное совпадение без поддоменов** (семантика zapret: «subdomains auto apply if not prefixed with `^`»).
* `list-exclude.txt` — российские и «безопасные» сайты, которые нельзя трогать (yandex, mail.ru,
  steam, nvidia…): desync на них может только навредить.
* `ipset-all.txt` — 33 тыс. подсетей; переключается между режимами `loaded` / `none`
  (заглушка `203.0.113.113/32`) / `any` (пустой файл = все IP).
* `ipset-exclude.txt` — частные сети (RFC1918, CGNAT, link-local), которые трогать нельзя.

**Наш hostlist** = объединение `list-google.txt` (без Discord) и наблюдаемых доменов
(`i9.ytimg.com`, `manifest.googlevideo.com`, `signaler-pa.youtube.com` из списков ByeByeDPI),
с поддержкой `^` и exclude-списка.

### 2.4 `service.bat` и тестер стратегий

* **Run Tests** (`utils/test zapret.ps1`): по очереди запускает каждый `general*.bat`, ждёт `winws`,
  затем параллельно (runspace pool, до 16 потоков) проверяет цели из `utils/targets.txt`
  (`www.youtube.com`, `youtu.be`, `i.ytimg.com`, `redirector.googlevideo.com`, …) через
  `curl -I` в трёх вариантах: HTTP/1.1, TLS 1.2, TLS 1.3. Плюс ping.
  **Оценка:** score = число успешных HTTP/TLS-проверок, при равенстве выигрывает больше успешных ping.
  Лучший конфиг печатается в конце. Измеряется **доступность**, а не скорость.
* **DPI checkers**: POST 64 КБ на хосты из набора hyperion-cs и пометка `LIKELY_BLOCKED`, если
  ушло >0, пришло 0 и истёк таймаут — детектор «TCP 16-20».
* **Game Filter** — расширение диапазонов портов TCP/UDP 1024-65535 для игр (вне MVP).
* **IPSet Filter** — переключение режимов `loaded` / `none` / `any` (см. 2.3).
* **Hosts** — сверяет системный `hosts` с `.service/hosts` (адреса GitHub и т. п.).
* **Диагностика**: проверяет, что включены TCP timestamps (иначе `fooling=ts` не работает),
  что включён Secure DNS (DoH) в системе или браузере, нет ли записей YouTube в `hosts`,
  конфликтующих VPN/Adguard/WinDivert.

**Что берём:** перебор стратегий и score-функцию, но меряем **скорость на живом трафике
`googlevideo.com`** (у Flowseal её нет, а замедление — именно про скорость), плюс
детектор «замерзания».

---

## 3. bol-van/zapret: семантика параметров

### 3.1 Два класса инструментов

* **nfqws / winws** — пакетный уровень (NFQUEUE / WinDivert). Перехватывают и переписывают
  настоящие TCP/IP-пакеты: могут ставить любой seq, ip_id, TCP-опции, слать поддельные сегменты.
  Нужны root (Linux/Android) или драйвер (Windows).
* **tpws** — потоковый уровень: прозрачный или SOCKS-прокси. Работает через обычные
  `send()` с `TCP_NODELAY`, `TTL`, `MSG_OOB`. **В режиме `--socks` root не нужен**
  (прямо сказано в документации, раздел Android).

### 3.2 Режимы desync (`--dpi-desync`)

До трёх режимов через запятую, по фазам: 0 — рукопожатие (`synack`, `syndata`, `--wssize`);
1 — фейки до данных (`fake`, `rst`, `rstack`); 2 — модификация самих данных (`multisplit` и т. д.).

* `multisplit` — разрез по позициям из `--dpi-desync-split-pos`.
* `multidisorder` — то же, но сегменты уходят в обратном порядке.
* `fakedsplit` / `fakeddisorder` — один разрез, вокруг частей подмешиваются фейки
  (паттерн `--dpi-desync-fakedsplit-pattern`, по умолчанию `0x00`), порядок задаёт `altorder`.
* `hostfakesplit` — подделывается только кусок с hostname: «до host → фейковый host → настоящий
  host → фейковый host → после». Шаблон фейкового хоста — `--dpi-desync-hostfakesplit-mod=host=www.google.com`.
* `fake` — перед данными отправляются фейковые пакеты (`--dpi-desync-fake-tls`), повтор `--dpi-desync-repeats`.

### 3.3 Маркеры позиций

`method`, `host`, `endhost`, `sld`, `endsld`, `midsld`, `sniext` и их смещения (`sniext+1`, `midsld`),
а также абсолютные числа и отрицательные (от конца). `sniext` — начало данных расширения SNI;
hostname начинается на 5 байт дальше (2 байта длины списка, 1 байт типа, 2 байта длины имени).

### 3.4 `seqovl`, `ip-id`, fooling

* **seqovl** — к первому сегменту спереди добавляется N байт с уменьшенным seq. ОС сервера
  принимает только часть, попавшую в окно; DPI видит «мусор» (у Flowseal — целый ClientHello Google).
* **`--ip-id=zero`** — ip_id = 0 во всех сгенерированных пакетах (часть DPI проверяет
  монотонность ip_id и ловит фейки по нему).
* **fooling**: `md5sig` (опция TCP MD5, Linux-серверы отбрасывают), `badsum`, `badseq` (seq вне окна,
  по умолчанию -10000), `ts` (TSval минус 600000 — сервер отбрасывает по PAWS; на Windows требует
  включённых timestamps), `ttl`/`autottl` (фейк «умирает» между DPI и сервером), `datanoack`, `hopbyhop`.
* **`--dpi-desync-fake-tls-mod`**: `rnd` (случайные random/session id), `dupsid` (копия session id
  оригинала), `sni=…` (подмена SNI в фейке), `padencap`, `rndsni`.

### 3.5 tpws (без root)

`--split-pos`, `--disorder` (нечётные сегменты с TTL=1, ОС потом перешлёт их — порядок 2,4,6,1,3,5),
`--oob` (байт вне полосы в конце первого сегмента), `--tlsrec` (два TLS-рекорда в одном сегменте),
`--mss`, `--hostcase`/`--domcase` для HTTP, профили `--new` + hostlist. Предупреждение автора:
больше ~8 разрезов на Linux может слиться в один сегмент из-за буфера сокета.

---

## 4. ByeDPI (ciadpi): язык аргументов и что он умеет в userspace

ciadpi — SOCKS4/5-прокси (по умолчанию `0.0.0.0:1080`). Всё делается на сокетах исходящих
соединений, поэтому root не нужен.

### 4.1 Позиции (`pos_t`)

`offset[:repeats:skip][+flags]`, флаги: `+s` — смещение от начала SNI-имени, `+h` — от начала Host,
`+n` — нулевое смещение; доп. флаги `+e` (конец имени), `+m` (середина имени).
Пример: `1+s` — первый байт имени SNI, `0+sm` — середина SNI, `1:3:5` — позиции 1, 6, 11.
Отрицательное число без флагов отсчитывается от конца пакета (`-1` — последний байт).

### 4.2 Приёмы

| Ключ | Что делает | Как реализовано в Linux/Android |
|---|---|---|
| `-s`, `--split` | разрез запроса по позиции | отдельные `send()` с `TCP_NODELAY` |
| `-d`, `--disorder` | часть уходит с TTL=1 и не доходит; ОС ретранслирует её после следующей | `setsockopt(IP_TTL / IPV6_UNICAST_HOPS)` |
| `-o`, `--oob` | часть с флагом URG и лишним OOB-байтом (`-e` задаёт байт) | `send(…, MSG_OOB)` |
| `-q`, `--disoob` | disorder + OOB | TTL + `MSG_OOB` |
| `-f`, `--fake` | первая часть сначала уходит **фейком** с малым TTL (`-t`, по умолчанию 8), затем ОС ретранслирует настоящие данные | `vmsplice(SPLICE_F_GIFT)` + `splice()` из mmap-буфера: в ядро отдаётся страница с фейком, после отправки её содержимое заменяется на оригинал, и ретрансмиссия несёт уже настоящие байты |
| `-t`, `--ttl` | TTL фейка | `IP_TTL` |
| `-S`, `--md5sig` | TCP MD5 на фейк | `TCP_MD5SIG`; **на Android часто выключен в ядре** |
| `-l`, `--fake-data` | свой фейк (файл или `:строка`) | данные |
| `-n`, `--fake-sni` | подмена SNI в фейке (`?`, `#`, `*` — случайные символы); можно несколько раз | данные |
| `-Q`, `--fake-tls-mod` | `rand` (random/session id/key share), `orig` (фейк = настоящий ClientHello), `msize=n` | данные |
| `-O`, `--fake-offset` | сдвиг начала фейковых данных | данные |
| `-r`, `--tlsrec` | разрез ClientHello на несколько TLS-рекордов (+5 байт заголовка) | данные |
| `-m`, `--tlsminor` | подмена minor-версии в заголовке рекорда | данные |
| `-M`, `--mod-http` | `hcsmix`, `dcsmix`, `rmspace` для HTTP | данные |
| `-Y`, `--drop-sack` | ядро игнорирует SACK (форсирует полную ретрансмиссию) | `SO_ATTACH_FILTER` (cBPF, root не нужен) |
| `-g`, `--def-ttl` | TTL всех исходящих | `IP_TTL` |
| `-F`, `--tfo` | TCP Fast Open | Linux ≥ 4.11 |
| `-a`, `--udp-fake` | число фейковых UDP | данные (нам не нужно) |

### 4.3 Группы и автоматика

`-A`, `--auto <torst|redirect|ssl_err|none>` делит аргументы на **группы**. Для каждого запроса
группы проверяются слева направо; группа применяется, если сработал её триггер и прошли
фильтры `-H` (hostlist), `-j` (ipset), `-V` (порты), `-K` (протоколы `t,h,u,i`). `-L` задаёт
кэширование выбранной группы по IP (`-u` — TTL кэша, 28 ч по умолчанию). `-T` — таймаут первого ответа.

**Ограничение для нашей задачи:** триггеры ciadpi реагируют на RST, таймаут и ошибку TLS.
**Замедление** (соединение живое, но 128 кбит/с) ciadpi не видит. Поэтому выбор стратегии по
скорости делает наш Go-слой, а ciadpi остаётся исполнителем одной строки аргументов.

### 4.4 Как ciadpi встроен в Android-порты (идеи, не код)

* ciadpi собирается CMake как `.so` со всеми `*.c` кроме `win_service.c`, с `-DANDROID_APP`
  (логирование в logcat), `-Wl,-z,max-page-size=16384` (16 КБ страницы, Android 15+).
* `main()` вызывается из JNI в отдельном потоке; остановка — `shutdown(server_fd)`.
* У ciadpi **глобальное состояние**, и второй вызов `main()` в том же процессе невозможен
  («proxy process already used»). Поэтому ByeByeDPI держит ciadpi в отдельном процессе `:native`
  и при перезапуске убивает этот процесс.
* Режим **Proxy**: приложение-клиент вручную указывает SOCKS5 127.0.0.1:1080.
  Режим **VPN**: VpnService + hev-socks5-tunnel заворачивают весь трафик в SOCKS5. Нам VPN не нужен.
* Тестер ByeByeDPI (`SiteCheckUtils`): для каждой стратегии из `proxytest_strategies.list`
  (60 строк) делает N запросов к сайтам через SOCKS и считает успешные. «Блок» — когда
  прочитано меньше, чем `Content-Length`. Снова доступность, а не скорость.

---

## 5. Свежие рабочие строки (2025–2026)

Строки из обсуждений с пометками. Это **сырьё для пресетов**, а не гарантия: стратегии
устаревают за недели (zapret #200: «работало месяцами, перестало ~29 мая 2026»).

### 5.1 ciadpi (byedpi #184 и др.)

| Дата | Оператор | Строка | Статус |
|---|---|---|---|
| 2026-08-25 | Tele2 | `-s0 -o1 -d1 -r1+s -Ar -o1 -At -f-1 -r1+s -As` | «хорошо работает» (автор) |
| 2026-06-11 | МГТС | `-o1 -a1 -r-5+se -H:"ggpht.com googlevideo.com youtube.com youtu.be ytimg.com"` | по словам автора |
| 2026-06-11 | Билайн | `-H:"googleapis.com" -s1 -r1+s -An -H:"ggpht.com googlevideo.com youtube.com youtu.be ytimg.com" -o1 -r-5+se` | советуют, не подтверждено |
| 2026-05-29 | — | `-d1 -d3+s -s6+s -d9+s -s12+s -d15+s -s20+s -d25+s -s30+s -d35+s -r1+s -S -a1 -As -d1 -d3+s … -S -a1` | работает на 30.05.2026 (автор); тот же «лесенкой» упоминают в zapret #200 для Android |
| 2026-05-11 | — (Linux) | `--timeout 2 --tfo --ttl 6 --round 2` | YouTube работает (автор) |
| 2025-11 / 2026-02 | Ростелеком, СЗ | `-Kt,h -d7 -s2 -An -Ku -a9 -An` | «давно работает», подтверждено в 02.2026 |
| 2025-09-23 | Билайн (проводной) | `-H yt.txt --oob 1 --tlsrec 5+s --auto=none …` | частично (не хватало доменов превью) |
| 2025-07 | Ростелеком, Метромакс, Tele2, Yota | `… -s0 -o1 -d1 -r1+s -Ar -o1 -At -f-1 -r1+s -As` | «работает на всех» (автор) |
| 2024-10 | Билайн → МГТС | `-s1 -q1 -Y -Ar -s5 -o1+s -At -f-1 -r1+s -As -s1 -o1+s -s-1 -An` | подтверждено на МГТС и Ростелекоме |

Повторяющиеся «кирпичики»: **`-o1`** (OOB на 1-м байте), **`-d1`**, **`-r1+s`** (TLS-рекорд
режется на начале SNI), **`-f-1`** (фейк всего запроса), **`-r-5+se`** (рекорд режется за 5 байт
до конца SNI), лесенка `-d1 -d3+s -s6+s …`, группы `-At` / `-Ar` / `-As` как запасные.
README ciadpi рекомендует для Linux `--disorder 1` и `--fake -1 --md5sig`.

### 5.2 nfqws (zapret #200)

* Базовая рекомендация автора (2024): TCP — `--dpi-desync=split2` (= `multisplit`, pos=2); QUIC — `fake, repeats=6`.
* Ростелеком (Ростов, Ярославль): `fake,split2 --dpi-desync-ttl=4 --dpi-desync-split-pos=1 --wssize 1:6 --dpi-desync-fooling=md5sig`.
* 2026-05: `fake --dpi-desync-repeats=6 --dpi-desync-fooling=ts` с Google-SNI-фейком — работало месяцами,
  перестало около 29.05.2026; `disorder --split-pos=2 --fooling=badsum` — перестало.

---

## 6. Главная таблица: методика → ciadpi → применимость без root

Обозначения: ✅ — делается в userspace; ⚠️ — частично или с оговорками; ❌ — нужны сырые пакеты.

| Методика Flowseal / zapret | Эквивалент ciadpi | Userspace без root | Комментарий |
|---|---|---|---|
| `multisplit --split-pos=1` | `-s1` | ✅ | отдельный `send()` с `TCP_NODELAY` |
| `multisplit --split-pos=2` (`split2`) | `-s2` | ✅ | |
| `multisplit --split-pos=2,sniext+1` | `-s2 -s-4+s` | ✅ | `sniext+1` = начало имени − 4 байта |
| `multisplit --split-pos=midsld` | `-s0+sm` | ✅ | `+sm` — середина SNI |
| `multidisorder --split-pos=1,midsld` | `-d1 -d0+sm` | ⚠️ | disorder через TTL=1 и ретрансмиссию: +~200 мс RTO на соединение |
| `disorder2` / `fakeddisorder` | `-d1` или `-d2` | ⚠️ | то же |
| `--dpi-desync-split-seqovl=681` + google-pattern | **нет**; замена: `-f-1 -l google.bin -t <ttl>` | ❌ → ⚠️ | seqovl требует сегмента с уменьшенным seq. Замена даёт ту же идею «DPI видит ClientHello Google первым», но фейк доставляется TTL-ом, а не seq |
| `fake --dpi-desync-fake-tls=google.bin` | `-f-1 -l tls_clienthello_www_google_com.bin` | ✅ | фейк через vmsplice; данные берутся из `.bin` |
| `--dpi-desync-fake-tls-mod=rnd,dupsid,sni=www.google.com` | `-f-1 -n www.google.com -Qr` | ✅ | `-Q rand`; аналога `dupsid` нет — для ТСПУ некритично |
| `--dpi-desync-fake-tls=!` (сам ClientHello как фейк) | `-Qo` (`orig`) | ✅ | |
| `--dpi-desync-repeats=N` | нет прямого; несколько `-f` в группе | ⚠️ | на сокетах фейк отправляется один раз на позицию |
| `--dpi-desync-ttl=N` | `-t N` | ✅ | TTL подбирается под сеть — это одно из измерений перебора |
| `--dpi-desync-autottl` | нет | ❌ | нужен TTL входящих пакетов; замена — перебор нескольких `-t` |
| `--dpi-desync-fooling=md5sig` | `-S` | ⚠️ | на многих Android-ядрах `TCP_MD5SIG` выключен; включаем только в пресетах-кандидатах |
| `--dpi-desync-fooling=ts` | нет | ❌ | нужно переписать TSval в TCP-опции. Замена — TTL-фейк |
| `--dpi-desync-fooling=badseq` / `badsum` / `datanoack` | нет | ❌ | замена — TTL-фейк или disorder |
| `hostfakesplit host=www.google.com` | `-f<pos>+s -n www.google.com` + `-s<pos>+s` | ⚠️ | фейковый hostname через fake-SNI + разрез по SNI; точного порядка «фейк-настоящий-фейк» нет |
| `fakedsplit --dpi-desync-fakedsplit-pattern=0x00` | `-f1 -l :\0` | ⚠️ | |
| `--ip-id=zero` | нет | ❌ | ip_id назначает ядро. Отказываемся (на Linux/Android ip_id и так стабилен в пределах соединения) |
| `--wssize 1:6` (окно в SYN-ACK) | нет | ❌ | замена — `-r` (tlsrec) + split |
| tpws `--oob` | `-o1` / `-o1+s` | ✅ | на ряде операторов (МГТС, Tele2) — основа рабочих строк |
| tpws `--tlsrec` | `-r1+s`, `-r-5+se` | ✅ | может ломать часть middlebox'ов; для Google безопасно |
| tpws `--mss` | нет (ciadpi) | ⚠️ | доступно через `TCP_MAXSEG` на нашем сокете, но режет скорость; не используем |
| `--hostlist` / `^domain` / `--hostlist-exclude` | `-H` (без `^`) | ✅ | hostlist ведёт **Go-слой**: в ciadpi уходят только целевые соединения |
| `--ipset` / `--ipset-exclude` | `-j` | ✅ | частные сети исключаем в Go-слое |
| `--filter-udp=443` + `fake-quic` | `-a N` (UDP-фейк) | — | не нужно: через HTTP-прокси WebView не использует QUIC |
| Голос Discord (`--filter-l7=discord,stun`) | — | — | вне MVP (UDP/WebRTC, нужен VpnService) |
| Run Tests (доступность curl'ом) | — | ✅ | у нас — замер скорости первых чанков googlevideo на живом трафике |
| Secure DNS / DoH в системе | — (ciadpi резолвит синхронно через систему) | ✅ | DoH делает Go-слой; в ciadpi передаём **уже IP** (SOCKS5 ATYP=IPv4/IPv6) |

### 6.1 Что требует raw-пакетов и чем заменяем — коротко

* **seqovl** (основа `general.bat`) → TTL-фейк с Google-ClientHello (`-f-1 -l google.bin -t N`) + разрез `-s1`.
* **ip-id=zero** → отказ; влияет на малую часть DPI.
* **fooling ts / badseq / badsum / datanoack** → TTL (`-t`), где можно — `md5sig`, иначе disorder/OOB.
* **autottl** → перебор нескольких TTL (4, 6, 8, 10) как разных стратегий.
* **wssize** → tlsrec.
* **repeats** → несколько фейков на разных позициях.
* **QUIC-фейки** → не нужны: QUIC через HTTP-прокси не используется.

---

## 7. Решения для проекта

1. **Исполнитель desync — ciadpi** (MIT), собранный NDK под `arm64-v8a`, `armeabi-v7a`, `x86_64`.
   Запускаем его **отдельным процессом**: бинарник кладётся в APK как `lib/<abi>/libciadpi.so`
   (это исполняемый файл, а не библиотека), Android распаковывает его в `nativeLibraryDir`,
   откуда разрешено исполнение даже при targetSdk ≥ 29 (W^X). Плюсы по сравнению с JNI-`main()`:
   нет проблемы глобального состояния, смена стратегии = перезапуск процесса, падение ciadpi не
   роняет приложение, можно держать **два экземпляра** (текущая стратегия + кандидат для пробы).
   Один и тот же Go-код управляет ciadpi и на ПК (`ciadpi`/`ciadpi.exe`), и на Android.
2. **Go-слой** (не зависит от Android): HTTP CONNECT-прокси для WebView → чтение первого TLS-рекорда →
   SNI → hostlist. Целевые соединения идут в ciadpi по SOCKS5 **с IP-адресом из DoH**,
   остальные — напрямую. ciadpi запускаем с `-N` (запрет доменных запросов), чтобы он никогда
   не трогал системный DNS.
3. **DNS** — только DoH: Google (`8.8.8.8`, `8.8.4.4`), Cloudflare (`1.1.1.1`, `1.0.0.1`),
   Quad9 (`9.9.9.9`, `149.112.112.112`), AdGuard; подключение по зашитым IP с правильным SNI,
   гонка нескольких провайдеров, кэш с TTL. Российские резолверы (Яндекс и т. п.) не используем:
   они подчиняются НСДИ. Последний фолбэк — зашитые в конфиг адреса фронтендов Google для
   ключевых доменов YouTube (обновляются через remote-config).
4. **Метрика скорости**: Go-слой видит байты ответа `*.googlevideo.com` на каждом соединении:
   скорость первых N КБ, время до первого байта, «замерзание» на 16–20 КБ, обрывы.
   Решение о смене стратегии — по скользящему окну нескольких соединений.
5. **Пресеты** — JSON-формат с полями «id, название, аргументы ciadpi, нужные `.bin`, приоритет»,
   переносим идеи из таблицы раздела 6 и строки из раздела 5. Фейк
   `tls_clienthello_www_google_com.bin` (MIT, Flowseal) кладём в assets.
6. **Без VpnService, root и QUIC**; голос Discord, игры, UDP — вне MVP (фиксируется в README).
7. **Резервный движок** на будущее — `tpws --socks` из zapret (тоже MIT, тоже без root):
   богаче маркеры позиций (`midsld`, `sniext`), но тяжелее сборка. Формат стратегий оставляем
   нейтральным (`engine: "ciadpi"`), чтобы добавить tpws без ломки конфигов.
