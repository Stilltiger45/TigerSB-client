# Сторонние компоненты

TigerSB-client распространяется под GPL-3.0 (см. [LICENSE](LICENSE)). Ниже Go-зависимости, которые входят в ядро. Их лицензии совместимы с GPL-3.0 и требуют сохранять свои уведомления об авторских правах. Полные тексты лежат в [app/assets/licenses.json](app/assets/licenses.json) (создаёт `go run ./scripts/licensetexts`, CI проверяет, что файл свежий) и показываются в приложении: меню профиля → «О приложении» → «Лицензии компонентов». Там же лицензии библиотек Dart/Flutter (Flutter и mobile_scanner), их собирает сам Flutter.

Таблица создана командой `scripts/check-licenses.sh --notices`, CI проверяет те же лицензии на каждом pull request.

| Модуль | Версия | Лицензия |
|---|---|---|
| github.com/andybalholm/brotli | v1.0.6 | MIT |
| github.com/apernet/quic-go | v0.59.1-0.20260217092621-db4786c77a22 | MIT |
| github.com/cloudflare/circl | v1.6.3 | BSD |
| github.com/enfein/mieru/v3 | v3.39.0 | GPL-3.0 |
| github.com/ghodss/yaml | v1.0.1-0.20220118164431-d8423dcdf344 | MIT |
| github.com/gofrs/uuid/v5 | v5.5.1 | MIT |
| github.com/google/btree | v1.1.3 | Apache-2.0 |
| github.com/gorilla/websocket | v1.5.3 | BSD |
| github.com/juju/ratelimit | v1.0.2 | LGPL |
| github.com/klauspost/compress | v1.17.9 | Apache-2.0 |
| github.com/klauspost/cpuid/v2 | v2.3.0 | MIT |
| github.com/metacubex/utls | v1.8.7 | BSD |
| github.com/miekg/dns | v1.1.72 | BSD |
| github.com/pelletier/go-toml | v1.9.5 | Apache-2.0 |
| github.com/pires/go-proxyproto | v0.11.0 | Apache-2.0 |
| github.com/quic-go/qpack | v0.6.0 | MIT |
| github.com/refraction-networking/utls | v1.8.3-0.20260301010127-aa6edf4b11af | BSD |
| github.com/sagernet/quic-go | v0.61.0-sing-box-mod.6 | MIT |
| github.com/sagernet/sing | v0.9.0-beta.4 | GPL-3.0 |
| github.com/sagernet/sing-quic | v0.7.1 | GPL-3.0 |
| github.com/sagernet/sing-shadowsocks | v0.2.7 | GPL-3.0 |
| github.com/vishvananda/netlink | v1.3.1 | Apache-2.0 |
| github.com/vishvananda/netns | v0.0.5 | Apache-2.0 |
| github.com/xtls/reality | v0.0.0-20260322125925-9234c772ba8f | MPL-2.0 |
| github.com/xtls/xray-core | v1.260327.0 | MPL-2.0 |
| go4.org/netipx | v0.0.0-20231129151722-fdeea329fbba | BSD |
| golang.org/x/crypto | v0.58.0 | BSD |
| golang.org/x/exp | v0.0.0-20250911091902-df9299821621 | BSD |
| golang.org/x/net | v0.61.0 | BSD |
| golang.org/x/sync | v0.24.0 | BSD |
| golang.org/x/sys | v0.49.0 | BSD |
| golang.org/x/text | v0.43.0 | BSD |
| golang.org/x/time | v0.12.0 | BSD |
| golang.zx2c4.com/wireguard | v0.0.0-20250521234502-f333402bd9cb | MIT |
| google.golang.org/genproto/googleapis/rpc | v0.0.0-20251202230838-ff82c1b0f217 | Apache-2.0 |
| google.golang.org/grpc | v1.79.3 | Apache-2.0 |
| google.golang.org/protobuf | v1.36.11 | BSD |
| gopkg.in/yaml.v2 | v2.4.0 | Apache-2.0 |
| gvisor.dev/gvisor | v0.0.0-20260122175437-89a5d21be8f0 | Apache-2.0 |
| lukechampine.com/blake3 | v1.4.1 | MIT |
