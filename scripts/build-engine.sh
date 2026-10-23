#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
export ANDROID_HOME="${ANDROID_HOME:-${ANDROID_SDK_ROOT:-$HOME/Android/Sdk}}"
export ANDROID_NDK_HOME="$ANDROID_HOME/ndk/28.2.13676358"
CMAKE="$ANDROID_HOME/cmake/3.31.6/bin/cmake"
for ABI in arm64-v8a armeabi-v7a x86_64; do
  "$CMAKE" -S "$ROOT/native" -B "$ROOT/native/out/$ABI" -G Ninja \
    -DCMAKE_TOOLCHAIN_FILE="$ANDROID_NDK_HOME/build/cmake/android.toolchain.cmake" \
    -DANDROID_ABI="$ABI" -DANDROID_PLATFORM=android-24 -DCMAKE_BUILD_TYPE=Release
  "$CMAKE" --build "$ROOT/native/out/$ABI"
  mkdir -p "$ROOT/app/src/main/jniLibs/$ABI"
  cp "$ROOT/native/out/$ABI/libciadpi.so" "$ROOT/app/src/main/jniLibs/$ABI/"
done
cd "$ROOT/core"
go install golang.org/x/mobile/cmd/gomobile@v0.0.0-20261011034528-197770444867
go install golang.org/x/mobile/cmd/gobind@v0.0.0-20261011034528-197770444867
export PATH="$(go env GOPATH)/bin:$PATH"
gomobile init
mkdir -p "$ROOT/app/libs"
gomobile bind -target=android/arm64,android/arm,android/amd64 -androidapi 24 \
  -javapkg ru.razgon -o "$ROOT/app/libs/engine.aar" ./mobile
