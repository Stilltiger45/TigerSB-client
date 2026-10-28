<p align="center">
  <img src="docs/assets/banner.jpg" alt="TigerSB — VPN-клиент" width="100%">
</p>

<p align="center">
  <a href="https://github.com/Stilltiger45/TigerSB-client/releases/tag/V0.1"><img alt="Release" src="https://img.shields.io/github/v/release/Stilltiger45/TigerSB-client?style=for-the-badge&label=release&labelColor=1c1c1c&color=FF6A00"></a>
  <a href="LICENSE"><img alt="License GPL-3.0" src="https://img.shields.io/badge/license-GPL--3.0-FF6A00?style=for-the-badge&labelColor=1c1c1c"></a>
  <img alt="Android 7+" src="https://img.shields.io/badge/Android-7%2B-FF6A00?style=for-the-badge&logo=android&logoColor=white&labelColor=1c1c1c">
  <img alt="Windows скоро" src="https://img.shields.io/badge/Windows-%D1%81%D0%BA%D0%BE%D1%80%D0%BE-555555?style=for-the-badge&labelColor=1c1c1c">
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
  🇷🇺 <b>Русский</b> &nbsp;|&nbsp; <a href="README.md">🇬🇧 English</a>
</p>

---

**TigerSB** — VPN-приложение, в котором ничего не нужно настраивать: вставил ключ — и подключился. Оно само выбирает самый быстрый сервер, само решает, какие сайты открывать через VPN, а какие напрямую, и само держит связь при смене сети. Работает со своей панелью [TigerSB-panel](https://github.com/Stilltiger45/TigerSB-panel) и с подписками других VPN (Marzban, 3x-ui, Remnawave и другие), в том числе с конфигами Clash (YAML) и sing-box (JSON).

<p align="center">
  <img src="docs/assets/screens.jpg" alt="Экраны приложения: ключ, режимы «Умная», «Базовая», «Все» и «О приложении»" width="100%">
</p>

**Оглавление:** [Для пользователей](#для-пользователей) · [Для разработчиков](#для-разработчиков)

# Для пользователей

## Быстрый старт

1. **Скачайте приложение:** [TigerSB-android.apk](https://github.com/Stilltiger45/TigerSB-client/releases/latest/download/TigerSB-android.apk) (Android 7+). Для старых 32-битных телефонов: [TigerSB-android-old-phones.apk](https://github.com/Stilltiger45/TigerSB-client/releases/latest/download/TigerSB-android-old-phones.apk).
2. **Установите:** откройте файл на телефоне и разрешите установку.
3. **Добавьте ключ** одним из способов:
   - откройте ссылку `tigersb://…` из Telegram, почты или панели — приложение откроется с уже вставленным ключом;
   - отсканируйте QR-код;
   - вставьте ключ вручную: подойдут `tigersb://`, ссылка на подписку `https://` или ссылки `vless://`, `vmess://`, `trojan://`, `ss://`, `hysteria2://`, `tuic://`, `mierus://`, `mieru://`, `naive+https://`.
4. **Нажмите кнопку подключения.** Всё.

Ключ выдаёт тот, кто держит сервер: в [TigerSB-panel](https://github.com/Stilltiger45/TigerSB-panel) он на вкладке «Пользователи». Приложение обновляется само: меню справа → «Проверить обновление».

Приложение для Windows появится следующим этапом поверх того же ядра.

## Режимы

- **Умная** (по умолчанию) — каждый сайт открывается через тот сервер, который пускает его быстрее всех, а если не пускает ни один — напрямую или с обходом блокировок.
- **Базовая** — российские сайты, банки и госуслуги напрямую, всё остальное через VPN.
- **Все** — весь трафик через VPN, кроме локальной сети.

## Что ещё умеет

- **Приложения мимо VPN.** MAX, ВК, Яндекс, банки, госприложения, маркетплейсы и приложения операторов связи всегда идут мимо VPN и не видят туннель и адрес сервера. Свои приложения можно добавить в список.
- **Надёжная связь.** Упавший сервер заменяется за секунды, при переходе между Wi-Fi и мобильной сетью VPN не отключается.
- **Несколько профилей.** Имя профиля вверху экрана открывает список: переключение одним нажатием, у каждого «Обновить», «Ссылка» и «Удалить».
- **Удобства.** Скорость, трафик и пинг на главном экране, плитка в шторке, протокол в уведомлении, автообновление профиля и списков блокировок, «Отправить журнал» в меню, светлая и тёмная тема в «О приложении».

# Для разработчиков

## Компоненты

| Компонент | Зачем | Лицензия |
|---|---|---|
| [Xray-core](https://github.com/XTLS/Xray-core) 26.3.27 | ядро VPN: VLESS (Reality, XHTTP, gRPC), VMess, Trojan, Shadowsocks, Hysteria 2; маршрутизация, балансировщик, замер задержки | MPL-2.0 |
| [sing-quic](https://github.com/SagerNet/sing-quic) из sing-box | протокол TUIC v5, встроен в Xray как обычный исходящий | GPL-3.0 |
| [mieru](https://github.com/enfein/mieru) | клиент Mieru (TCP и UDP), встроен в Xray как обычный исходящий | GPL-3.0 |
| свой код (`internal/naive`) | клиент NaïveProxy: HTTP/2 CONNECT с отбивкой поверх TLS с отпечатком Chrome, встроен в Xray как обычный исходящий | GPL-3.0 |
| [gVisor](https://gvisor.dev) | сетевой стек системного VPN (TUN) | Apache-2.0 |
| [Go](https://go.dev) 1.26 и gomobile | ядро клиента, консольная утилита `tigersb-core`, библиотека для Android | BSD-3-Clause |
| [Flutter](https://flutter.dev) 3.47 и Dart | интерфейс приложения | BSD-3-Clause |
| [Kotlin](https://kotlinlang.org) 2.4 | системный VPN Android (VpnService), слежение за сетью, Keystore | Apache-2.0 |

Код по папкам: `pkg/profile` — формат профиля и ключа (его же использует панель), `internal/` — ядро (подписки, сборка конфига Xray, умная маршрутизация, TUIC, Mieru и NaïveProxy, замеры, автонастройка сети), `mobile/` — API для Android через gomobile, `app/` — приложение на Flutter и Kotlin, `cmd/tigersb-core` — консольная утилита.

## Как это работает

- **Одно ядро на всех платформах.** Xray-core запускается внутри процесса приложения, без отдельных программ. На Android трафик приходит через системный VPN (TUN на gVisor), локальных портов нет.
- **Умная маршрутизация.** Каждый новый сайт проверяется через все серверы подписки, побеждает самый быстрый чистый ответ; отказ (403/451) — пробуем напрямую. Если не помог ни один сервер, сайт пробуется через обход DPI (фрагментация и шумы вместо zapret, набор подбирается сам при каждом включении, смене сети и сбое канала), и только потом идёт напрямую. Протоколы Xray поверх TCP в приоритете, Hysteria 2 и TUIC — когда TCP не справился, и всегда с шумом перед QUIC. Подробно: [docs/profile.md](docs/profile.md#режим-auto-проверка-каждого-сайта).
- **Режим по спискам** (`main`, в консольной утилите) разводит трафик по профилю TigerSB на три направления: заблокированное — через выход за границей, остальное — через выход в РФ (провайдер не видит трафик, сайты видят российский IP), банки, госуслуги и вузы — напрямую.
- **Устойчивость.** В «Базовой» и «Все» протокол выбирается по сглаженным замерам и не прыгает между серверами. Канал проверяется двумя GET к t.me и YouTube по одному соединению; после двух неудач подряд соединения к серверам открываются заново, после четырёх ядро перезапускается. При смене сети туннель не пересобирается, а без интернета клиент ждёт, а не перезапускается. MTU (1480, 1460 или 1280) подбирается по тому, какой пакет доходит до сервера без фрагментации; на мобильной сети TCP-сегменты подгоняются под этот путь, а NAT оператора держится открытым.

## Безопасность и защита

- **Подписанный профиль.** Профиль TigerSB подписан Ed25519, а публичный ключ панели закреплён в ключе `tigersb://` (`pk`). Профиль с чужой подписью отклоняется, поэтому подменить серверы по дороге нельзя. Для ссылки `https://` без `pk` закрепляется ключ первого полученного профиля. Подписка скачивается только по HTTPS.
- **Закрепление сертификата.** Для серверов с самоподписанным сертификатом (`insecure=1` в ссылке) клиент при импорте запоминает SHA-256 сертификата и дальше принимает только его, вместо небезопасного `allowInsecure`.
- **Хранение ключа.** Файл состояния с ключом подписки зашифрован AES-256-GCM, а ключ шифрования хранится в Android Keystore. Резервные копии приложения выключены (`allowBackup=false`).
- **Защита экрана.** `FLAG_SECURE` запрещает скриншоты и запись экрана, касания через чужие окна не принимаются, оверлеи других приложений скрываются.
- **Изоляция приложений.** MAX, ВК, Яндекс, банки, госприложения, маркетплейсы и приложения операторов связи исключены из туннеля системно (`addDisallowedApplication`) и не видят адрес сервера. Панель не может выключить этот список.
- **Обновления.** Приложение берёт `version.json` и APK только из релиза этого репозитория на GitHub и передаёт APK системному установщику. Android ставит обновление, только если оно подписано тем же ключом, что и установленная версия.
- **Лимит устройств.** При скачивании подписки клиент присылает `x-hwid` и данные устройства, как принято у Remnawave и похожих панелей, чтобы сервер мог считать устройства.

## Консольная утилита

```bash
go build -o tigersb-core ./cmd/tigersb-core

./tigersb-core import 'tigersb://add?url=...&pk=...'   # или https://…, vless://…, hysteria2://…, tuic://…, mierus://…, naive+https://…
./tigersb-core run                                     # SOCKS5 127.0.0.1:10808, HTTP 127.0.0.1:10809
```

<details>
<summary><b>Все команды и режимы</b></summary>

| Команда | Что делает |
|---|---|
| `tigersb-core import <ключ>` | добавить ключ и скачать профиль |
| `tigersb-core update` | обновить профиль по подписке |
| `tigersb-core show` | показать выходы, транспорты и размер списков |
| `tigersb-core run [-mode auto\|main\|all\|off]` | запустить VPN с локальными портами SOCKS и HTTP |
| `tigersb-core config [-mode …]` | напечатать конфиг Xray, который будет запущен |
| `tigersb-core keygen` | создать ключи подписи профиля (для панели) |
| `tigersb-core sign -key … -in …` | подписать профиль |
| `tigersb-core link -url … -pk …` | собрать ключ `tigersb://` |

Режимы: `auto` — проверка каждого сайта через все серверы (по умолчанию), `basic` — российские сайты, банки и вузы напрямую, остальное через VPN, `main` — три направления по спискам, `all` — всё через VPN, `off` — без VPN.

Если профиль использует `geosite:` или `geoip:`, `run` сам скачивает `geosite.dat` и `geoip.dat` в каталог состояния и оставляет в них только нужные списки. Свой каталог с файлами можно указать через `-assets`.

Состояние (ключ и последний профиль) хранится в `%AppData%\TigerSB` на Windows и `~/.config/TigerSB` на Linux, файл доступен только пользователю.

</details>

## Сборка и тесты

```bash
go test ./...
CGO_ENABLED=0 go build -o tigersb-core ./cmd/tigersb-core
```

Тесты собирают конфиги для всех режимов и проверяют, что их принимает настоящий Xray-core.

Android-приложение лежит в `app/` (Flutter, системный VPN на Kotlin в `app/android/app/src/main/kotlin`). Ядро для него собирается из пакета `mobile/` через gomobile:

```bash
scripts/build-core-android.sh        # нужны Android SDK и NDK
cd app && flutter build apk --split-per-abi
```

Обычно это делать не нужно: GitHub Actions собирает APK на каждый pull request (вкладка Actions, артефакт `TigerSB-android`) и выкладывает релиз по тегу. Как выпустить версию и задать ключ подписи APK: [docs/releases.md](docs/releases.md). Формат профиля и ключа: [docs/profile.md](docs/profile.md).

## Лицензии

Клиент распространяется под **GPL-3.0**, см. [LICENSE](LICENSE). Причина: клиент TUIC взят из sing-box (библиотеки sing-quic и sing под GPL-3.0), поэтому и весь клиент открыт под этой лицензией.

- **Остальные зависимости** — MPL-2.0 (Xray-core), Apache-2.0, BSD, MIT и LGPL, все совместимы с GPL-3.0. `scripts/check-licenses.sh` проверяет их на каждом pull request.
- **Полный список** с версиями — в [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md), полные тексты видны в приложении: «О приложении» → «Лицензии компонентов».
- Серверная часть, [TigerSB-panel](https://github.com/Stilltiger45/TigerSB-panel), тоже под GPL-3.0.
