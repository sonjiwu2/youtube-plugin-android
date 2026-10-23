param([long]$Version = [DateTimeOffset]::UtcNow.ToUnixTimeSeconds(), [string]$Key = '')
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
if (!$Key) { $Key = Join-Path $root 'signing\private-config.key' }
if (!(Test-Path -LiteralPath $Key)) { throw 'Восстановите приватный ключ конфигурации из своей резервной копии' }
$config = Get-Content -LiteralPath "$root\core\presets.json" -Raw | ConvertFrom-Json
$config.version = $Version
$config | Add-Member -NotePropertyName expires -NotePropertyValue ([DateTime]::UtcNow.AddDays(90).ToString('yyyy-MM-ddTHH:mm:ssZ')) -Force
New-Item -ItemType Directory -Force "$root\signing" | Out-Null
$output = Join-Path $root 'signing\strategies.json'
[System.IO.File]::WriteAllText($output, ($config | ConvertTo-Json -Depth 10) + "`n", [System.Text.UTF8Encoding]::new($false))
Push-Location "$root\core"
try {
    & go run ./cmd/configsign -key $Key -file $output
    if ($LASTEXITCODE) { throw 'Подпись конфигурации не выполнена' }
} finally { Pop-Location }
& gh release view strategies --repo sonjiwu2/youtube-android-zapret *> $null
if ($LASTEXITCODE -eq 0) {
    & gh release upload strategies $output "$output.sig" --clobber --repo sonjiwu2/youtube-android-zapret
} else {
    & gh release create strategies $output "$output.sig" --target main --prerelease --title 'Подписанные стратегии Разгон' --notes 'Конфигурация Ed25519 для автоматического обновления приложения. APK находятся в релизах v*.' --repo sonjiwu2/youtube-android-zapret
}
if ($LASTEXITCODE) { throw 'Публикация конфигурации не выполнена' }
