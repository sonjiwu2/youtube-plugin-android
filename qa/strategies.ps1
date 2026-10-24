param(
    [string]$Serial = 'emulator-5556',
    [string]$Sdk = "$env:LOCALAPPDATA\Android\Sdk",
    [string]$Only = '',
    [int]$WatchSeconds = 45,
    [string]$Output = 'tmp/strategy-matrix'
)
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
$adb = Join-Path $Sdk 'platform-tools/adb.exe'
if ($Serial -notmatch '^emulator-') { throw 'Use a dedicated disposable emulator, not a personal phone.' }
$folder = [IO.Path]::GetFullPath((Join-Path $root $Output))
New-Item -ItemType Directory -Force $folder | Out-Null
function Android([string[]]$Arguments) {
    $result = & $adb -s $Serial @Arguments 2>&1
    if ($LASTEXITCODE) { throw "$Arguments : $result" }
    return ($result -join "`n")
}
function Diagnostic {
    try { return (Android @('shell','run-as','ru.razgon.app','cat','cache/vpn-diagnostics.json') | ConvertFrom-Json) }
    catch { return $null }
}
function Player([string]$Target) {
    for ($retry = 0; $retry -lt 3; $retry++) {
        $dump = Android @('shell','uiautomator','dump','/sdcard/qa-player.xml')
        if ($dump -match 'dumped') {
            Android @('pull','/sdcard/qa-player.xml',$Target) | Out-Null
            [xml]$tree = Get-Content $Target -Raw
            $node = $tree.SelectNodes('//node') | Where-Object { $_.'content-desc' -match '^((\d+) minutes? )?(\d+) seconds? of ' } | Select-Object -First 1
            $seconds = $null
            if ($node -and $node.'content-desc' -match '^((\d+) minutes? )?(\d+) seconds? of ') { $seconds = 60 * [int]$Matches[2] + [int]$Matches[3] }
            return @{seconds=$seconds; description=$node.'content-desc'; tree=$tree}
        }
    }
    return @{seconds=$null; description='UI unavailable'}
}
$config = Get-Content (Join-Path $root 'core/presets.json') -Raw | ConvertFrom-Json
$strategies = $config.strategies | Where-Object { !$Only -or $_.id -in $Only.Split(',') }
$rows = @()
foreach ($strategy in $strategies) {
    $id = $strategy.id
    Write-Output "START $id"
    Android @('shell','am','force-stop','com.google.android.youtube') | Out-Null
    Android @('shell','am','force-stop','ru.razgon.app') | Out-Null
    # Clear disposable AVD caches while preserving consent and app settings.
    # --cache-only silently hangs on Play Store images without the internal permission.
    Android @('shell','pm','trim-caches','1000G','internal') | Out-Null
    Android @('shell','run-as','ru.razgon.app','rm','-f','cache/vpn-diagnostics.json') | Out-Null
    Android @('shell','am','start','-n','ru.razgon.app/.QaActivity','--es','strategy',$id) | Out-Null
    $deadline = [DateTime]::UtcNow.AddSeconds(30)
    $diag = $null
    do {
        Start-Sleep -Seconds 1
        $diag = Diagnostic
    } while ((!$diag -or $diag.status.strategyId -ne $id -or $diag.status.state -in @('connecting','selecting')) -and [DateTime]::UtcNow -lt $deadline)
    $samples = @()
    $startVideo = 0
    if ($diag) { $startVideo = [long]$diag.status.videoBytes }
    # Opening the real app is attempted even when the availability probe failed.
    Android @('shell','am','start','-a','android.intent.action.VIEW','-d','https://www.youtube.com/watch?v=YE7VzlLtp-4','-p','com.google.android.youtube') | Out-Null
    $started = [DateTime]::UtcNow
    $firstPlayer = $null
    while (([DateTime]::UtcNow - $started).TotalSeconds -lt $WatchSeconds) {
        Start-Sleep -Seconds 3
        $diag = Diagnostic
        if ($diag) { $samples += @{at=([DateTime]::UtcNow-$started).TotalSeconds; status=$diag.status} }
        if (!$firstPlayer -and ([DateTime]::UtcNow - $started).TotalSeconds -gt 10) {
            $firstPlayer = Player (Join-Path $folder "$id-first.xml")
            Android @('shell','screencap','-p','/sdcard/qa-first.png') | Out-Null
            Android @('pull','/sdcard/qa-first.png',(Join-Path $folder "$id-first.png")) | Out-Null
        }
    }
    $lastPlayer = Player (Join-Path $folder "$id-last.xml")
    Android @('shell','screencap','-p','/sdcard/qa-last.png') | Out-Null
    Android @('pull','/sdcard/qa-last.png',(Join-Path $folder "$id-last.png")) | Out-Null
    $diag = Diagnostic
    $videoBytes = [long]$diag.status.videoBytes - $startVideo
    $progress = $null
    if ($null -ne $firstPlayer.seconds -and $null -ne $lastPlayer.seconds) { $progress = $lastPlayer.seconds - $firstPlayer.seconds }
    $row = [ordered]@{
        id=$id; state=$diag.status.state; selected=$diag.status.strategyId;
        downloaded=[long]$diag.status.totalBytes; videoBytes=$videoBytes;
        videoStreams=$diag.status.videoStreams; startPosition=$firstPlayer.seconds; endPosition=$lastPlayer.seconds;
        progress=$progress; passed=($videoBytes -gt 131072 -and $progress -ge 12 -and $diag.status.strategyId -eq $id)
    }
    $rows += [pscustomobject]$row
    @{result=$row; samples=$samples; diagnostics=$diag} | ConvertTo-Json -Depth 15 | Set-Content -Encoding utf8 (Join-Path $folder "$id.json")
    $rows | ConvertTo-Json -Depth 5 | Set-Content -Encoding utf8 (Join-Path $folder 'results.json')
    Write-Output ($row | ConvertTo-Json -Compress)
}
