#!/usr/bin/env bash
# Builds the Go core for Android as app/android/app/libs/tigersb-core.aar.
# Needs Go, the Android SDK and NDK (ANDROID_HOME, ANDROID_NDK_HOME).
set -euo pipefail
cd "$(dirname "$0")/.."

# gomobile and gobind at the versions pinned in go.mod.
go install golang.org/x/mobile/cmd/gomobile golang.org/x/mobile/cmd/gobind
export PATH="$(go env GOPATH)/bin:$PATH"
gomobile init

mkdir -p app/android/app/libs
# TARGETS lets CI add android/amd64 for the emulator.
gomobile bind \
  -target="${TARGETS:-android/arm64,android/arm}" \
  -androidapi 24 \
  -javapkg app.tigersb \
  -trimpath \
  -ldflags "-s -w -buildid=" \
  -o app/android/app/libs/tigersb-core.aar \
  ./mobile
ls -lh app/android/app/libs/tigersb-core.aar
