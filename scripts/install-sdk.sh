#!/usr/bin/env bash
set -euo pipefail
TOOLS="$ANDROID_HOME/cmdline-tools/latest/bin"
if [ -x "$TOOLS/android" ]; then
  "$TOOLS/android" sdk install 'ndk/28.2.13676358'
  "$TOOLS/android" sdk install 'cmake/3.31.6'
  "$TOOLS/android" sdk install 'platforms/android-37'
  "$TOOLS/android" sdk install 'build-tools/36.0.0'
else
  "$TOOLS/sdkmanager" 'ndk;28.2.13676358' 'cmake;3.31.6' 'platforms;android-37' 'build-tools;36.0.0'
fi
