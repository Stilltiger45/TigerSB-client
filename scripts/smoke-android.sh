#!/usr/bin/env bash
# Installs the APK on a running emulator, opens the app and checks that the
# key screen is drawn. Leaves logcat, a screenshot and the UI dump in $2.
set -uo pipefail
apk=$1
out=$2
mkdir -p "$out"

adb install -r "$apk"
adb logcat -c
adb shell am start -W -n app.tigersb/.MainActivity
sleep 20
adb exec-out screencap -p > "$out/screen.png"
adb shell uiautomator dump /sdcard/ui.xml >/dev/null 2>&1
adb pull /sdcard/ui.xml "$out/ui.xml" >/dev/null 2>&1
adb logcat -d > "$out/logcat.txt"
grep -E "AndroidRuntime|FATAL|GoLog|TigerSB|flutter|ANR" "$out/logcat.txt" | tail -80

if grep -q "Подключить" "$out/ui.xml" 2>/dev/null; then
  echo "Экран ключа на месте"
  exit 0
fi
echo "::error::Экран ключа не появился: см. артефакт TigerSB-android-smoke"
exit 1
