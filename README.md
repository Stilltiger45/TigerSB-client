<p align="center">
  <img src="docs/assets/banner.jpg" alt="TigerSB VPN client" width="100%">
</p>

<p align="center">
  <a href="https://github.com/Stilltiger45/TigerSB-client/releases/tag/V0.1"><img alt="Release" src="https://img.shields.io/github/v/release/Stilltiger45/TigerSB-client?style=for-the-badge&label=release&labelColor=1c1c1c&color=FF6A00"></a>
  <a href="LICENSE"><img alt="License GPL-3.0" src="https://img.shields.io/badge/license-GPL--3.0-FF6A00?style=for-the-badge&labelColor=1c1c1c"></a>
  <img alt="Android 7+" src="https://img.shields.io/badge/Android-7%2B-FF6A00?style=for-the-badge&logo=android&logoColor=white&labelColor=1c1c1c">
  <img alt="Windows soon" src="https://img.shields.io/badge/Windows-soon-555555?style=for-the-badge&labelColor=1c1c1c">
</p>

<p align="center">
  <a href="https://github.com/XTLS/Xray-core"><img alt="Xray-core 26.3.27" src="https://img.shields.io/badge/Xray--core-26.3.27-FF8A1F?style=for-the-badge&labelColor=1c1c1c"></a>
  <a href="https://github.com/SagerNet/sing-quic"><img alt="sing-box TUIC" src="https://img.shields.io/badge/sing--box-TUIC-FF8A1F?style=for-the-badge&labelColor=1c1c1c"></a>
  <a href="https://gvisor.dev"><img alt="gVisor TUN" src="https://img.shields.io/badge/gVisor-TUN-FF8A1F?style=for-the-badge&labelColor=1c1c1c"></a>
</p>

<p align="center">
  <img alt="Go 1.26" src="https://img.shields.io/badge/Go-1.26-FFB861?style=for-the-badge&logo=go&logoColor=white&labelColor=1c1c1c">
  <img alt="Flutter 3.47" src="https://img.shields.io/badge/Flutter-3.47-FFB861?style=for-the-badge&logo=flutter&logoColor=white&labelColor=1c1c1c">
  <img alt="Kotlin 2.4" src="https://img.shields.io/badge/Kotlin-2.4-FFB861?style=for-the-badge&logo=kotlin&logoColor=white&labelColor=1c1c1c">
  <img alt="gomobile" src="https://img.shields.io/badge/gomobile-Android-FFB861?style=for-the-badge&labelColor=1c1c1c">
</p>

<p align="center">
  <a href="README.ru.md">🇷🇺 Русский</a> &nbsp;|&nbsp; 🇬🇧 <b>English</b>
</p>

---

**TigerSB** is a VPN client built on Xray: paste a key and you are connected. It works with its own panel, [TigerSB-panel](https://github.com/Stilltiger45/TigerSB-panel), and with subscriptions from other VPNs (Marzban, 3x-ui, Remnawave and others).

<p align="center">
  <img src="docs/assets/screens.jpg" alt="App screens: key, Smart, Basic and All modes, About" width="100%">
</p>

<sub>The app interface is in Russian.</sub>

## Features

- **One key.** Paste a key, scan a QR code or open a `tigersb://` link and the VPN turns on. A subscription link from another VPN works too: the client picks the fastest server in it.
- **Three routing modes:**
  - **Smart** (default): every site is tried through every server in the subscription and opened through the fastest one that lets it in. If none does, the site is tried again through DPI evasion (fragmentation and noise instead of zapret, with the preset tuned on every connect, network change and channel failure), and only then goes direct. Xray protocols over TCP come first; Hysteria 2 and TUIC are used only when TCP fails, and always send noise before QUIC.
  - **Basic**: Russian sites (.ru, .рф, Russian IPs), banks and government services go direct, everything else goes through the VPN.
  - **All**: all traffic goes through the VPN except the local network.
- **Apps outside the VPN.** MAX, VK, Yandex, banking and government apps are excluded from the VPN, so they cannot see the tunnel or the server address. You can add your own apps to the list.
- **Security.** Subscriptions are checked against an Ed25519 signature and cannot be spoofed. The key is stored encrypted with Android Keystore and is left out of backups. The screen is protected from screenshots, screen recording and taps through overlay windows.
- **Stable connection.** In Basic and All the protocol is chosen from smoothed measurements without flapping between servers, and a dead server is left within seconds. The channel is checked like a proxy ping in other clients: two GETs to t.me and YouTube over one kept-alive connection, the better time counts; a failed check is repeated after 3 seconds, two failures in a row reopen the connections to the servers and four restart the core. When the phone switches between Wi-Fi and mobile data, gets a new address or the signal comes back, the tunnel stays up: only the connections to the servers are reopened, so apps reconnect at once. While the phone's network has no internet at all the client waits instead of restarting, and picks up within seconds of its return. The MTU (1480, 1460 or 1280) follows how large a packet reaches the server without fragmenting, and on mobile data an LTE mode clamps TCP segments to that path and keeps the operator's NAT open.
- **Several profiles.** The profile name at the top opens a list: tap a profile to use it; each has Refresh, Link (copies the key) and Delete, and "Add profile" sits at the bottom. A tigersb:// link opened when a profile already exists offers to add one more.
- **Extras.** Speed, traffic and ping panel, quick settings tile, current protocol in the notification, automatic profile and block-list updates, "Send log" in the menu, and an About screen with third-party licenses.

## Cores and technologies

| Component | Role | License |
|---|---|---|
| [Xray-core](https://github.com/XTLS/Xray-core) 26.3.27 | VPN core: VLESS (Reality, XHTTP, gRPC), VMess, Trojan, Shadowsocks, Hysteria 2; routing, balancer, latency tests | MPL-2.0 |
| [sing-quic](https://github.com/SagerNet/sing-quic) from sing-box | TUIC v5, plugged into Xray as a regular outbound | GPL-3.0 |
| [gVisor](https://gvisor.dev) | network stack for the system VPN (TUN) | Apache-2.0 |
| [Go](https://go.dev) 1.26 and gomobile | client core, the `tigersb-core` CLI, Android library | BSD-3-Clause |
| [Flutter](https://flutter.dev) 3.47 and Dart | app interface | BSD-3-Clause |
| [Kotlin](https://kotlinlang.org) 2.4 | Android system VPN (VpnService), network watcher, Keystore | Apache-2.0 |

Supported links: `tigersb://`, `https://` (subscription), `vless://`, `vmess://`, `trojan://`, `ss://`, `hysteria2://` (`hy2://`), `tuic://`.

## Install on Android

Download `TigerSB-android.apk` from [Releases](https://github.com/Stilltiger45/TigerSB-client/releases), open it on the phone and allow the install. Android 7 or newer is required. For old 32-bit phones use `TigerSB-android-old-phones.apk`.

A Windows app on the same core is the next stage.

How to publish a new version: [docs/releases.md](docs/releases.md) (in Russian).

## How routing works

Smart routing works with any subscription, your own or someone else's. For example, a Russian site that blocks foreign IPs automatically goes through a server in Russia or a cascade. Details are in [docs/profile.md](docs/profile.md#режим-auto-проверка-каждого-сайта) (in Russian).

The core also has a list-based mode driven by the TigerSB profile (`main`, in the CLI) that splits traffic three ways:

- **Blocked sites** go through the VPN with an exit abroad.
- **Everything else** goes through the VPN with an exit in Russia: the ISP and public Wi-Fi see nothing, and sites see a Russian IP.
- **Banks, government services and university sites** open directly, because many of them break with any VPN.

## Command-line tool

```bash
go build -o tigersb-core ./cmd/tigersb-core

./tigersb-core import 'tigersb://add?url=...&pk=...'   # or https://…, vless://…, hysteria2://…, tuic://…
./tigersb-core run                                     # SOCKS5 127.0.0.1:10808, HTTP 127.0.0.1:10809
```

Then point the browser or the system at the proxy `127.0.0.1:10808` (SOCKS5) or `127.0.0.1:10809` (HTTP).

| Command | What it does |
|---|---|
| `tigersb-core import <key>` | add a key and download the profile |
| `tigersb-core update` | refresh the profile from the subscription |
| `tigersb-core show` | show exits, transports and list sizes |
| `tigersb-core run [-mode auto\|main\|all\|off]` | run the VPN with local SOCKS and HTTP ports |
| `tigersb-core config [-mode …]` | print the Xray config that would run |
| `tigersb-core keygen` | create profile signing keys (for the panel) |
| `tigersb-core sign -key … -in …` | sign a profile |
| `tigersb-core link -url … -pk …` | build a `tigersb://` key |

Modes: `auto` tries every site through every server (default), `basic` sends Russian sites, banks and universities direct and the rest through the VPN, `main` uses the three-way lists, `all` sends everything through the VPN, `off` disables the VPN.

If the profile uses `geosite:` or `geoip:`, `run` downloads `geosite.dat` and `geoip.dat` into the state folder and keeps only the lists it needs. Use `-assets` to point at your own folder.

State (the key and the last profile) lives in `%AppData%\TigerSB` on Windows and `~/.config/TigerSB` on Linux, readable only by the user.

## Profile and key format

Described in [docs/profile.md](docs/profile.md) (in Russian). Profiles are signed with Ed25519, and the client accepts only a signature from the pinned key.

## Development

```bash
go test ./...
CGO_ENABLED=0 go build -o tigersb-core ./cmd/tigersb-core
```

The tests build configs for every mode and check that the real Xray-core accepts them.

The Android app lives in `app/` (Flutter, with the system VPN in Kotlin under `app/android/app/src/main/kotlin`). Its core is built from the `mobile/` package with gomobile:

```bash
scripts/build-core-android.sh        # needs the Android SDK and NDK
cd app && flutter build apk --split-per-abi
```

Usually you don't need to: GitHub Actions builds the APK for every pull request (Actions tab, artifact `TigerSB-android`) and publishes the release from a tag.

## License

GPL-3.0, see [LICENSE](LICENSE). The TUIC client comes from sing-box (the sing-quic library), which is GPL-3.0, so the whole client is released under that license.

Third-party Go libraries and their licenses are listed in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md), and the full texts are in the app under «О приложении» (About) → «Лицензии компонентов» (Component licenses). The server side, [TigerSB-panel](https://github.com/Stilltiger45/TigerSB-panel), is GPL-3.0 too.
