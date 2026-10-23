#!/usr/bin/env bash
set -euo pipefail
TOOLS="$ANDROID_HOME/cmdline-tools/latest/bin"
if [ -x "$TOOLS/android" ]; then
  "$TOOLS/android" sdk install 'ndk/28.2.13676358'
  "$TOOLS/android" sdk install 'cmake/3.31.6'
else
  "$TOOLS/sdkmanager" 'ndk;28.2.13676358' 'cmake;3.31.6'
fi
# Платформу compileSdk и build-tools устанавливает AGP: его sdklib новее SDK CLI runner.
