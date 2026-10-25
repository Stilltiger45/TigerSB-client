#!/usr/bin/env bash
# Installs the APK on a running emulator, opens the app and checks that the
# key screen is drawn. Leaves logcat, a screenshot and the UI dump in $2, and
# repeats the interesting part of logcat as annotations on the check run, so
# it can be read from the PR without downloading anything.
set -uo pipefail
apk=$1
out=$2
mkdir -p "$out"

# Prints text as one annotation of the given level, in chunks GitHub accepts.
annotate() {
  local level=$1 title=$2 text=$3
  local i=0
  while [ -n "$text" ] && [ $i -lt 4 ]; do
    local chunk=${text:0:3500}
    text=${text:3500}
    chunk=${chunk//'%'/'%25'}
    chunk=${chunk//$'\r'/}
    chunk=${chunk//$'\n'/'%0A'}
    echo "::$level title=$title ($i)::$chunk"
    i=$((i + 1))
  done
}

adb install -r "$apk"
adb logcat -c
adb shell am start -W -n app.tigersb/.MainActivity
sleep 25
adb exec-out screencap -p > "$out/screen.png"
adb shell uiautomator dump /sdcard/ui.xml >/dev/null 2>&1
adb pull /sdcard/ui.xml "$out/ui.xml" >/dev/null 2>&1
adb logcat -d -v brief > "$out/logcat.txt"
# The emulator's GL driver writes to stderr, which the Go core sends to
# logcat as GoLog; drop that noise from what gets reported.
noise='s_glBindAttribLocation|Choreographer|OpenGLRenderer|EGL_emulation|HostConnection|eglCodecCommon'

pid=$(adb shell pidof app.tigersb | tr -d '\r')
echo "app pid: ${pid:-not running}"

if grep -q "Подключить" "$out/ui.xml" 2>/dev/null; then
  echo "Экран ключа на месте"
  exit 0
fi

texts=$(grep -oE '(text|content-desc)="[^"]+"' "$out/ui.xml" 2>/dev/null | sort -u | head -40 | tr '\n' ' ')
annotate warning "UI" "pid=${pid:-none}; texts: ${texts:-none}"
annotate error "crash" "$(grep -A30 -E 'FATAL EXCEPTION|Fatal signal|ANR in|panic:' "$out/logcat.txt" | head -150)"
if [ -n "$pid" ]; then
  annotate warning "app log" "$(grep -E "\( *$pid\)" "$out/logcat.txt" | grep -vE "$noise" | tail -120)"
fi
annotate notice "tags" "$(grep -E 'flutter|GoLog|TigerSB|AndroidRuntime|DEBUG' "$out/logcat.txt" | grep -vE "$noise" | tail -80)"
echo "::error::Экран ключа не появился"
exit 1
