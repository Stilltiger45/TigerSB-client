#!/usr/bin/env bash
# Long run of the VPN on an emulator: connects the app to a local test server
# (scripts/soak) and keeps traffic going in each mode for $SOAK_MINUTES,
# checking every 20 seconds that the app lives, the tunnel is up and sites
# open, and recording the app's memory, threads and open files. Everything is
# left in $2; failures and the app's log are repeated as annotations.
set -uo pipefail
apk=$1
out=$2
minutes=${SOAK_MINUTES:-12}
modes=${SOAK_MODES:-"Умная Базовая Все"}
mkdir -p "$out"
port=20443
key="vless://5783a3e7-e373-51cd-8642-c83782b807c5@10.0.2.2:$port?security=none#Soak"

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

# Taps the centre of the first UI element whose text or description is $1.
tap() {
  adb shell uiautomator dump /sdcard/ui.xml >/dev/null 2>&1
  adb pull /sdcard/ui.xml "$out/ui.xml" >/dev/null 2>&1
  local b
  b=$(grep -oE "(text|content-desc)=\"$1\"[^>]*bounds=\"\[[0-9]+,[0-9]+\]\[[0-9]+,[0-9]+\]\"" "$out/ui.xml" | head -1 |
    grep -oE '\[[0-9]+,[0-9]+\]\[[0-9]+,[0-9]+\]')
  if [ -z "$b" ]; then
    b=$(grep -oE "bounds=\"\[[0-9]+,[0-9]+\]\[[0-9]+,[0-9]+\]\"[^>]*(text|content-desc)=\"$1\"" "$out/ui.xml" | head -1 |
      grep -oE '\[[0-9]+,[0-9]+\]\[[0-9]+,[0-9]+\]')
  fi
  if [ -z "$b" ]; then
    echo "no element $1"
    annotate warning "no element $1" "$(grep -oE '(text|content-desc)="[^"]+"' "$out/ui.xml" | head -40)"
    return 1
  fi
  read -r x1 y1 x2 y2 < <(echo "$b" | tr -c '0-9' ' ')
  adb shell input tap $(((x1 + x2) / 2)) $(((y1 + y2) / 2))
}

# One HTTP request from the emulator's shell, through the VPN like any app.
http() {
  adb shell "printf 'GET $2 HTTP/1.1\r\nHost: $1\r\nConnection: close\r\n\r\n' | toybox nc -w 8 $1 80 2>&1 | head -1" |
    tr -d '\r' | grep -oE 'HTTP/1.[01] [0-9]+' || echo FAIL
}

go build -o soak-server ./scripts/soak
./soak-server -port $port > "$out/server.log" 2>&1 &
server=$!
trap 'kill $server' EXIT

adb root >/dev/null 2>&1; sleep 3
adb install -r "$apk"
adb shell pm grant app.tigersb android.permission.POST_NOTIFICATIONS
adb shell appops set app.tigersb ACTIVATE_VPN allow
adb logcat -c
adb logcat -v time > "$out/logcat.txt" 2>&1 &
logcat=$!
trap 'kill $server $logcat' EXIT

adb shell am start -W -n app.tigersb/.MainActivity
sleep 15
adb shell uiautomator dump /sdcard/ui.xml >/dev/null 2>&1
adb pull /sdcard/ui.xml "$out/ui.xml" >/dev/null 2>&1
field=$(grep -oE 'class="android.widget.EditText"[^>]*bounds="\[[0-9]+,[0-9]+\]\[[0-9]+,[0-9]+\]"' "$out/ui.xml" | head -1 |
  grep -oE '\[[0-9]+,[0-9]+\]\[[0-9]+,[0-9]+\]')
read -r x1 y1 x2 y2 < <(echo "$field" | tr -c '0-9' ' ')
adb shell input tap $(((x1 + x2) / 2)) $(((y1 + y2) / 2))
adb shell "input text '$key'"
adb shell input keyevent 111 # close the keyboard
sleep 1
tap "Подключить" || exit 1
sleep 5
# The first connect asks to leave the app out of battery saving.
tap "Allow" || tap "ALLOW" || true
sleep 15

log="$out/soak.txt"
echo "time mode pid tun_addrs google ya.ru example rss_kb threads fds" > "$log"
failed=0
for mode in $modes; do
  tap "$mode" || { failed=1; continue; }
  sleep 10
  end=$((SECONDS + minutes * 60))
  while [ $SECONDS -lt $end ]; do
    pid=$(adb shell pidof app.tigersb | tr -d '\r')
    # A reconnect may bring the tunnel up as tun1 while tun0 goes away.
    tun=$(adb shell ip -o addr show 2>/dev/null | grep -cE ' tun[0-9]+ +inet')
    g=$(http connectivitycheck.gstatic.com /generate_204)
    y=$(http ya.ru /)
    e=$(http example.com /)
    rss=- threads=- fds=-
    if [ -n "$pid" ]; then
      rss=$(adb shell "grep VmRSS /proc/$pid/status" | grep -oE '[0-9]+')
      threads=$(adb shell "grep Threads /proc/$pid/status" | grep -oE '[0-9]+')
      fds=$(adb shell "ls /proc/$pid/fd | wc -l" | tr -d '\r')
    fi
    line="$(date +%T) $mode ${pid:-none} $tun $g $y $e $rss $threads $fds"
    echo "$line" | tee -a "$log"
    case "$line" in *FAIL* | *none*) failed=1 ;; esac
    if [ "$tun" = 0 ]; then failed=1; fi
    sleep 20
  done
done

# Wi-Fi off and on again: the VPN must move to mobile data and back by itself.
check() {
  local pid g y e
  pid=$(adb shell pidof app.tigersb | tr -d '\r')
  g=$(http connectivitycheck.gstatic.com /generate_204)
  y=$(http ya.ru /)
  e=$(http example.com /)
  line="$(date +%T) $1 ${pid:-none} - $g $y $e"
  echo "$line" | tee -a "$log"
  case "$line" in *FAIL* | *none*) failed=1 ;; esac
}
for i in $(seq "${SOAK_SWITCHES:-4}"); do
  adb shell svc wifi disable
  sleep 25
  check mobile
  adb shell svc wifi enable
  sleep 30
  check wifi
done

kill $logcat
# Restarts of the core: the connect, a mode switch and a network switch make
# one each; anything far above that is a loop.
restarts=$(grep -c 'TigerSB.*подключено' "$out/logcat.txt")
echo "core starts: $restarts" | tee -a "$log"
if [ "$restarts" -gt $((4 + 2 * ${SOAK_SWITCHES:-4} + 4)) ]; then failed=1; fi
noise='s_glBindAttribLocation|Choreographer|OpenGLRenderer|EGL_emulation|HostConnection|eglCodecCommon'
annotate notice "soak" "$(cat "$log")"
annotate warning "app log" "$(grep -E 'GoLog|TigerSB|AndroidRuntime|FATAL|panic|Fatal signal|lowmemory|ActivityManager.*tigersb' "$out/logcat.txt" | grep -vE "$noise" | tail -150)"
annotate notice "server" "$(tail -60 "$out/server.log")"
if [ $failed = 1 ]; then
  echo "::error::VPN не выдержал длительный тест, см. soak.txt"
  exit 1
fi
echo "VPN проработал $minutes мин в каждом режиме"
