param([string]$Sdk = $env:ANDROID_HOME)
$ErrorActionPreference = 'Stop'
if (!$Sdk) { $Sdk = "$env:LOCALAPPDATA\Android\Sdk" }
$root = Split-Path $PSScriptRoot -Parent
$env:ANDROID_HOME = $Sdk
$env:ANDROID_NDK_HOME = Join-Path $Sdk 'ndk\28.2.13676358'
$cmake = Join-Path $Sdk 'cmake\3.31.6\bin\cmake.exe'
if (!(Test-Path $cmake)) { throw 'Установите CMake 3.31.6 через Android SDK Manager' }
foreach ($abi in @('arm64-v8a', 'armeabi-v7a', 'x86_64')) {
    & $cmake -S "$root\native" -B "$root\native\out\$abi" -G Ninja "-DCMAKE_TOOLCHAIN_FILE=$env:ANDROID_NDK_HOME\build\cmake\android.toolchain.cmake" "-DANDROID_ABI=$abi" -DANDROID_PLATFORM=android-24 -DCMAKE_BUILD_TYPE=Release
    if ($LASTEXITCODE) { throw "CMake $abi" }
    & $cmake --build "$root\native\out\$abi"
    if ($LASTEXITCODE) { throw "Native $abi" }
    New-Item -ItemType Directory -Force "$root\app\src\main\jniLibs\$abi" | Out-Null
    Copy-Item "$root\native\out\$abi\libciadpi.so" "$root\app\src\main\jniLibs\$abi\libciadpi.so"
}
Push-Location "$root\core"
try {
    & go install golang.org/x/mobile/cmd/gomobile@v0.0.0-20261011034528-197770444867
    if ($LASTEXITCODE) { throw 'gomobile install' }
    & go install golang.org/x/mobile/cmd/gobind@v0.0.0-20261011034528-197770444867
    if ($LASTEXITCODE) { throw 'gobind install' }
    $gobin = (& go env GOPATH) + '\bin'
    $env:PATH = "$gobin;$env:PATH"
    & "$gobin\gomobile.exe" init
    if ($LASTEXITCODE) { throw 'gomobile init' }
    New-Item -ItemType Directory -Force "$root\app\libs" | Out-Null
    & "$gobin\gomobile.exe" bind '-target=android/arm64,android/arm,android/amd64' -androidapi 24 -javapkg ru.razgon -o "$root\app\libs\engine.aar" ./mobile
    if ($LASTEXITCODE) { throw 'gomobile bind' }
} finally { Pop-Location }
