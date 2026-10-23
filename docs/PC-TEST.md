# Проверка ядра на ПК

Эта проверка запускает только собственный локальный прокси; настройки Windows не меняются.
Нужен Go 1.26+ и ciadpi 17.3 для Windows из https://github.com/hufrea/byedpi/releases.

```powershell
cd core
go test ./...
go run ./cmd/razgon -ciadpi C:\путь\ciadpi.exe
```

В консоли появится адрес `127.0.0.1:<порт>`. Откройте отдельный профиль Chrome:

```powershell
& 'C:\Program Files\Google\Chrome\Application\chrome.exe' --user-data-dir="$env:TEMP\razgon-browser" --proxy-server="http://127.0.0.1:<порт>" https://m.youtube.com
```

Не используйте основной профиль: уже запущенный Chrome может проигнорировать параметры.
Включите видео и проверьте скорость. В консоли каждые 5 секунд выводится стратегия и скорость
зашифрованного потока `googlevideo.com`. Ctrl+C останавливает движок и закрывает сокеты.
Прямой режим `-direct` служит только для диагностики CONNECT/DoH и не включает desync.
В Android он не используется.

Дополнительный живой тест (из папки `core`):

```powershell
$env:RAZGON_LIVE_CIADPI = 'C:\путь\ciadpi.exe'
$env:RAZGON_LIVE_FAKE = (Resolve-Path ..\app\src\main\assets\tls_clienthello_www_google_com.bin).Path
go test -v -run TestLiveProxy
```

Открытие главной страницы подтверждает цепочку CONNECT/SOCKS/DoH/TLS. Реальный разгон видео
подтверждается только воспроизведением в вашей сети: тест главной страницы этого не доказывает.
