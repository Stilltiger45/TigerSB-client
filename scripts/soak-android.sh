#!/usr/bin/env bash
# Long run of the VPN on an emulator: connects the app to a local test server
# (scripts/soak) and keeps traffic going in each mode for $SOAK_MINUTES,
# checking every 20 seconds that the app lives, the tunnel is up and sites
# open, and recording the app's memory, threads and open files. Everything is
# left in $2; failures and the app's log are repeated as annotations.
# SOAK_MODES picks the modes. SOAK_PROFILE=mix adds Hysteria 2 and TUIC to the
# plain VLESS server (scripts/soak/mix.txt). The servers sit behind a stand-in
# for DPI (scripts/soak/dpi.go): SOAK_DPI is its level, off (0), weak (1:
# plain VLESS and QUIC told by the first packet are cut) or strong (QUIC is
# cut altogether), or several levels in turn, each for SOAK_MINUTES in every
# mode. Meanwhile the emulator downloads and uploads 4 MB on every check and
# keeps a stream going both ways (scripts/soakload).
# SOAK_SCENARIOS adds the tests of an unsteady network, run in the mode that
# sends everything through the VPN:
#   switches  Wi-Fi off and on SOAK_SWITCHES times, quickly;
#   outage    no internet at all for SOAK_OUTAGE_MINUTES (10), then back at once;
#   mobile    SOAK_MOBILE_MINUTES (30) on mobile data with the signal cut for a
#             minute halfway, then Wi-Fi back on.
#   toggle    the VPN off and on SOAK_TOGGLES (5) times with its button: how
#             long the tunnel takes to go and the sites to open again.
# After each event a site is opened through the VPN every second: how soon it
# opens and the longest stretch it did not go to net.txt.
# SOAK_PROFILE=awg is a subscription (scripts/soak/awg.txt) with an
# AmneziaWG 3 server, whose tunnel the runner routes to the internet, and a
# NaïveProxy server that never answers, as a dead server of a subscription.
set -uo pipefail
apk=$1
out=$2
minutes=${SOAK_MINUTES:-12}
modes=${SOAK_MODES:-"Умная Базовая Все"}
mkdir -p "$out"
port=20443
key="vless://5783a3e7-e373-51cd-8642-c83782b807c5@10.0.2.2:$port?security=none#Soak"
case "${SOAK_PROFILE:-vless}" in mix | awg)
  # A subscription must come over HTTPS that the phone trusts.
  key="https://raw.githubusercontent.com/${GITHUB_REPOSITORY}/${GITHUB_SHA}/scripts/soak/${SOAK_PROFILE}.txt"
  ;;
esac

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

# One HTTP request from the emulator's shell, through the VPN like any app;
# $3 is how many seconds it may take (8).
http() {
  adb shell "printf 'GET $2 HTTP/1.1\r\nHost: $1\r\nConnection: close\r\n\r\n' | toybox nc -w ${3:-8} $1 80 2>&1 | head -1" |
    tr -d '\r' | grep -oE 'HTTP/1.[01] [0-9]+' || echo FAIL
}

go build -o soak-server ./scripts/soak
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o soakload ./scripts/soakload
phases=${SOAK_DPI:-off}
dpi=
case " $phases " in *" 1 "* | *" weak "* | *" strong "*) dpi=1 ;; esac
awg=
if [ "${SOAK_PROFILE:-vless}" = awg ]; then
  # The AmneziaWG server's tunnel is a TUN device: let it make one, and send
  # what comes out of it to the internet.
  sudo setcap cap_net_admin+ep soak-server
  awg="-awg 20446"
fi
./soak-server -port $port -dpi "${phases%% *}" $awg > "$out/server.log" 2>&1 &
server=$!
trap 'kill $server' EXIT
if [ -n "$awg" ]; then
  for _ in $(seq 20); do ip link show awg0 >/dev/null 2>&1 && break; sleep 0.5; done
  sudo ip addr add 10.66.0.1/24 dev awg0
  sudo ip link set awg0 up
  sudo sysctl -qw net.ipv4.ip_forward=1
  sudo iptables -t nat -A POSTROUTING -s 10.66.0.0/24 ! -o awg0 -j MASQUERADE
  # Docker on the runner drops forwarded packets unless told otherwise.
  sudo iptables -I FORWARD 1 -i awg0 -j ACCEPT
  sudo iptables -I FORWARD 1 -o awg0 -j ACCEPT
  ip -o addr show awg0 | tee -a "$out/server.log"
fi

adb root >/dev/null 2>&1; sleep 3
adb install -r "$apk"
adb push soakload /data/local/tmp/soakload >/dev/null
adb shell chmod 755 /data/local/tmp/soakload
adb shell pm grant app.tigersb android.permission.POST_NOTIFICATIONS
adb shell appops set app.tigersb ACTIVATE_VPN allow
adb logcat -c
adb logcat -v time > "$out/logcat.txt" 2>&1 &
logcat=$!
trap 'kill $server $logcat' EXIT

adb shell am start -W -n app.tigersb/.MainActivity
# A slow emulator draws the first screen late: wait for the key's field, or
# the key goes nowhere and "Подключить" does nothing.
field=
for _ in $(seq 30); do
  sleep 3
  adb shell uiautomator dump /sdcard/ui.xml >/dev/null 2>&1
  adb pull /sdcard/ui.xml "$out/ui.xml" >/dev/null 2>&1
  field=$(grep -oE 'class="android.widget.EditText"[^>]*bounds="\[[0-9]+,[0-9]+\]\[[0-9]+,[0-9]+\]"' "$out/ui.xml" | head -1 |
    grep -oE '\[[0-9]+,[0-9]+\]\[[0-9]+,[0-9]+\]')
  [ -n "$field" ] && break
done
if [ -z "$field" ]; then
  annotate error "no key field" "$(grep -oE '(text|content-desc)="[^"]+"' "$out/ui.xml" | head -40)"
  exit 1
fi
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

# After an event, opens a site through the VPN every second for $2 seconds
# and notes how soon it opened and the longest stretch it did not; more than
# $3 seconds for either fails the run.
measure() {
  local label=$1 window=$2 limit=$3
  local start=$SECONDS first=- gap=0 down= polls=0 fails=0 now
  while [ $((SECONDS - start)) -lt "$window" ]; do
    polls=$((polls + 1))
    now=$SECONDS
    if [ "$(http example.com / 3)" != FAIL ]; then
      [ "$first" = - ] && first=$((now - start))
      [ -n "$down" ] && gap=$((now - down > gap ? now - down : gap))
      down=
    else
      fails=$((fails + 1))
      [ -z "$down" ] && down=$now
    fi
    sleep 1
  done
  [ -n "$down" ] && gap=$((SECONDS - down > gap ? SECONDS - down : gap))
  local verdict=ok
  if [ "$first" = - ] || [ "$first" -gt "$limit" ] || [ "$gap" -gt "$limit" ]; then
    verdict=FAIL
    failed=1
  fi
  echo "$(date +%T) $label: открылся через ${first} с, самый долгий перерыв ${gap} с, неудач $fails из $polls — $verdict" | tee -a "$net"
}

# The runner's emulator or adb can drop out in the middle of a run, which
# says nothing about the VPN: note what the host saw, try to get adb back,
# and if the emulator is gone, stop and say so.
dead=
emulator_back() {
  adb get-state >/dev/null 2>&1 && return 0
  {
    echo "$(date +%T) adb lost the emulator"
    adb devices
    pgrep -af qemu-system | cut -c1-200 || echo "no emulator process"
    free -m
    uptime
    echo "--- kernel"
    sudo dmesg -T | tail -15
    # The emulator leaves a crash report when it dies of itself.
    echo "--- crash reports"
    find /tmp/android-* ~/.android -newer "$apk" \( -name '*crash*' -o -name '*.dmp' \) 2>/dev/null | head
  } >> "$out/emulator.txt" 2>&1
  adb kill-server >/dev/null 2>&1
  adb start-server >/dev/null 2>&1
  if timeout 90 adb wait-for-device && adb root >/dev/null 2>&1 && sleep 3 &&
    timeout 30 adb wait-for-device && adb get-state >/dev/null 2>&1; then
    echo "$(date +%T) adb back" >> "$out/emulator.txt"
    adb logcat -v time >> "$out/logcat.txt" 2>&1 &
    logcat=$!
    trap 'kill $server $logcat' EXIT
    return 0
  fi
  echo "$(date +%T) the emulator did not come back" >> "$out/emulator.txt"
  dead=1
  failed=1
  return 1
}

log="$out/soak.txt"
net=$out/net.txt
stream=$out/stream.txt
echo "time mode pid tun_addrs google ya.ru example rss_kb threads fds download upload" > "$log"
failed=0
# One run of the bulk client on the emulator, through the VPN like any app.
load() {
  adb shell "/data/local/tmp/soakload $*" | tr -d '\r'
}
first=1
for phase in $phases; do
  emulator_back || break
  if [ -z "$first" ]; then
    # DPI comes on in the middle of a session: it cuts what passed, and the
    # VPN has to find a way by itself.
    curl -s "http://127.0.0.1:20490/dpi?level=$phase"
    measure "DPI $phase включён" 120 90
  fi
  first=
  for mode in $modes; do
    tap "$mode" || { failed=1; continue; }
    sleep 10
    adb shell "nohup /data/local/tmp/soakload -mode stream -for ${minutes}m > /data/local/tmp/stream.log 2>&1 &"
    end=$((SECONDS + minutes * 60))
    while [ $SECONDS -lt $end ]; do
      emulator_back || break 3
      pid=$(adb shell pidof app.tigersb | tr -d '\r')
      # A reconnect may bring the tunnel up as tun1 while tun0 goes away.
      tun=$(adb shell ip -o addr show 2>/dev/null | grep -cE ' tun[0-9]+ +inet')
      g=$(http connectivitycheck.gstatic.com /generate_204)
      y=$(http ya.ru /)
      e=$(http example.com /)
      dl=$(load -mode dl -size 4194304 -timeout 30s)
      ul=$(load -mode ul -size 4194304 -timeout 30s)
      # What a failed transfer got before it broke goes to loadfail.txt.
      case "$dl$ul" in *FAIL*) echo "$(date +%T) $mode/$phase $dl | $ul" >> "$out/loadfail.txt" ;; esac
      case "$dl" in FAIL*) dl=FAIL ;; *) dl=$(echo "$dl" | grep -oE '[0-9.]+ МБ/с' | tr -d ' ') ;; esac
      case "$ul" in FAIL*) ul=FAIL ;; *) ul=$(echo "$ul" | grep -oE '[0-9.]+ МБ/с' | tr -d ' ') ;; esac
      rss=- threads=- fds=-
      if [ -n "$pid" ]; then
        rss=$(adb shell "grep VmRSS /proc/$pid/status" | grep -oE '[0-9]+')
        threads=$(adb shell "grep Threads /proc/$pid/status" | grep -oE '[0-9]+')
        fds=$(adb shell "ls /proc/$pid/fd | wc -l" | tr -d '\r')
      fi
      line="$(date +%T) $mode/$phase ${pid:-none} $tun $g $y $e $rss $threads $fds ${dl:-FAIL} ${ul:-FAIL}"
      echo "$line" | tee -a "$log"
      case "$line" in *FAIL* | *none*) failed=1 ;; esac
      if [ "$tun" = 0 ]; then failed=1; fi
      sleep 20
    done
    sleep 15 # the stream ends with the mode's minutes
    emulator_back || break 2
    echo "--- $mode, DPI $phase" >> "$stream"
    adb shell cat /data/local/tmp/stream.log | tr -d '\r' >> "$stream"
    last=$(tail -1 "$stream")
    echo "$(date +%T) $mode/$phase $last" | tee -a "$log"
    case "$last" in "ok поток"*) ;; *) failed=1 ;; esac
  done
done

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

# No internet at all on the phone's network: nothing leaves the emulator but
# through the tunnel, which then has nowhere to go. adb keeps working.
cut() {
  for t in iptables ip6tables; do
    adb shell "$t -I OUTPUT 1 -o tun+ -j ACCEPT; $t -I OUTPUT 2 -o lo -j ACCEPT; $t -I OUTPUT 3 -p tcp --sport 5555 -j ACCEPT; $t -I OUTPUT 4 -j DROP"
  done
}
uncut() {
  for t in iptables ip6tables; do
    adb shell "$t -D OUTPUT -j DROP; $t -D OUTPUT -p tcp --sport 5555 -j ACCEPT; $t -D OUTPUT -o lo -j ACCEPT; $t -D OUTPUT -o tun+ -j ACCEPT"
  done
}

scenarios=${SOAK_SCENARIOS:-switches}
[ -n "$dead" ] && scenarios=none
switches=${SOAK_SWITCHES:-4}
case " $scenarios " in *" switches "* | *" outage "* | *" mobile "* | *" toggle "*)
  tap "Все" || failed=1
  sleep 10
  ;;
esac

# Wi-Fi off and on again, quickly: the VPN must move to mobile data and back
# by itself, keeping the tunnel.
case " $scenarios " in *" switches "*)
  for i in $(seq "$switches"); do
    adb shell svc wifi disable
    measure "Wi-Fi→мобильная $i" 15 15
    adb shell svc wifi enable
    measure "мобильная→Wi-Fi $i" 15 15
  done
  ;;
esac

# The VPN off and on with its button, as a person does: the tunnel must go
# within SOAK_OFF_LIMIT_MS (3000) and the sites open again soon after.
case " $scenarios " in *" toggle "*)
  for i in $(seq "${SOAK_TOGGLES:-5}"); do
    tap "Отключить VPN" || { failed=1; break; }
    began=$(date +%s%3N)
    off=-
    while [ $(($(date +%s%3N) - began)) -lt 30000 ]; do
      if ! adb shell ip -o link show 2>/dev/null | grep -qE ' tun[0-9]+'; then
        off=$(($(date +%s%3N) - began))
        break
      fi
      sleep 0.1
    done
    verdict=ok
    if [ "$off" = - ] || [ "$off" -gt "${SOAK_OFF_LIMIT_MS:-3000}" ]; then
      verdict=FAIL
      failed=1
    fi
    echo "$(date +%T) выключение $i: туннель исчез через ${off} мс — $verdict" | tee -a "$net"
    sleep 3
    tap "Подключить VPN" || { failed=1; break; }
    measure "включение $i" 20 15
  done
  ;;
esac

# The internet goes for minutes and comes back at once: the VPN must not
# churn meanwhile and must carry traffic again within seconds.
case " $scenarios " in *" outage "*)
  check "до обрыва"
  cut
  end=$((SECONDS + ${SOAK_OUTAGE_MINUTES:-10} * 60))
  while [ $SECONDS -lt $end ]; do
    sleep 60
    pid=$(adb shell pidof app.tigersb | tr -d '\r')
    echo "$(date +%T) без интернета: приложение ${pid:-none}" | tee -a "$log"
    [ -z "$pid" ] && failed=1
  done
  uncut
  measure "после ${SOAK_OUTAGE_MINUTES:-10} мин без интернета" 60 30
  ;;
esac

# Long on mobile data, the signal cut for a minute halfway (Android keeps the
# network), then Wi-Fi back.
case " $scenarios " in *" mobile "*)
  minutes_m=${SOAK_MOBILE_MINUTES:-30}
  adb shell svc wifi disable
  measure "Wi-Fi выключен" 15 15
  half=$((SECONDS + minutes_m * 30))
  end=$((SECONDS + minutes_m * 60))
  cutdone=
  while [ $SECONDS -lt $end ]; do
    check мобильная
    if [ -z "$cutdone" ] && [ $SECONDS -ge $half ]; then
      cutdone=1
      cut
      sleep 60
      uncut
      measure "мобильная: после обрыва сигнала на 1 мин" 60 30
    fi
    sleep 30
  done
  adb shell svc wifi enable
  measure "мобильная→Wi-Fi после $minutes_m мин" 30 15
  ;;
esac

kill $logcat
# Restarts of the core: the connect, a mode switch and a network switch make
# one each; anything far above that is a loop.
restarts=$(grep -c 'TigerSB.*подключено' "$out/logcat.txt")
echo "core starts: $restarts" | tee -a "$log"
toggles=0
case " $scenarios " in *" toggle "*) toggles=${SOAK_TOGGLES:-5} ;; esac
if [ "$restarts" -gt $((4 + 2 * switches + toggles + 4)) ]; then failed=1; fi

# The app's own journal and the core's crash trace, if any, go into the
# summary: the app log annotation is cut to its last lines.
files=/data/data/app.tigersb/files
adb shell ls -l $files > "$out/files.txt" 2>&1
adb shell cat $files/events.log > "$out/events.log" 2>&1
adb shell cat $files/crash.log > "$out/crash.log" 2>&1
{
  echo "--- process deaths and crashes"
  grep -E 'has died|Process app.tigersb|ANR in app.tigersb|FATAL EXCEPTION|Fatal signal|panic:|lowmemorykiller.*Kill' "$out/logcat.txt" | grep -vE 'uiautomator' | head -20
  echo "--- crash.log"
  head -40 "$out/crash.log"
  echo "--- files"
  cat "$out/files.txt"
  echo "--- DPI tuning"
  grep 'обход DPI' "$out/events.log" | tail -15
  echo "--- network"
  cat "$net" 2>/dev/null
} | cut -c1-300 | tee -a "$log"

# The Smart mode tunes its DPI preset on every core start.
tunes=$(grep -c 'обход DPI' "$out/events.log")
echo "DPI tunings: $tunes" | tee -a "$log"
# An AmneziaWG server has no DPI presets to tune.
case " $modes " in *" Умная "*) if [ "$tunes" = 0 ] && [ "${SOAK_PROFILE:-vless}" != awg ]; then failed=1; fi ;; esac
kill $server
sleep 1
if [ -n "$dpi" ]; then
  # Plain VLESS is cut, so the tuning must find the preset that splits the
  # connection start, and the DPI must have cut something and let through
  # the rest.
  if ! grep -q 'обход DPI.*набор 3' "$out/events.log"; then
    echo "no tuning picked preset 3" | tee -a "$log"
    failed=1
  fi
  box=$(grep -E '^.{20}dpi ' "$out/server.log" | tail -1)
  echo "DPI box: $box" | tee -a "$log"
  case "$box" in *"tcp blocked 0,"* | *"passed 0;"* | "") failed=1 ;; esac
fi
grep -E 'dpi |bulk:' "$out/server.log" | tail -8 | tee -a "$log"
noise='s_glBindAttribLocation|Choreographer|OpenGLRenderer|EGL_emulation|HostConnection|eglCodecCommon|GnssNetworkConnectivityHandler|registerNetworkAgent|uiautomator|RuntimeInit|boot image|lock profiling|Shutting down VM'
annotate notice "soak" "$(cat "$log")"
annotate warning "app log" "$(grep -E 'GoLog|TigerSB|AndroidRuntime|FATAL|panic|Fatal signal|lowmemory|ActivityManager.*tigersb' "$out/logcat.txt" | grep -vE "$noise" | tail -150)"
annotate notice "server" "$(tail -60 "$out/server.log")"
annotate notice "events" "$(grep -vE 'обход DPI' "$out/events.log" | tail -80)"
annotate notice "network" "$(cat "$net" 2>/dev/null)"
annotate notice "stream" "$(grep -E '^---|поток' "$stream" 2>/dev/null)"
[ -s "$out/loadfail.txt" ] && annotate warning "transfers" "$(cat "$out/loadfail.txt")"
annotate notice "tuning" "$(grep 'обход DPI' "$out/events.log" | tail -30)"
[ -s "$out/emulator.txt" ] && annotate warning "emulator" "$(cat "$out/emulator.txt")"
if [ -n "$dead" ]; then
  echo "::error::Эмулятор на раннере пропал посреди теста, это не сбой VPN; см. emulator.txt"
  exit 1
fi
if [ $failed = 1 ]; then
  echo "::error::VPN не выдержал длительный тест, см. soak.txt"
  exit 1
fi
echo "VPN проработал $minutes мин в каждом режиме"
