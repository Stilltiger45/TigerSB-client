package subscription

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Stilltiger45/TigerSB-client/internal/engine"
	"github.com/Stilltiger45/TigerSB-client/internal/xrayconf"
	"github.com/Stilltiger45/TigerSB-client/pkg/profile"
)

const clashConfig = `mixed-port: 7890
mode: rule
proxies:
  - name: "🇳🇱 NL reality"
    type: vless
    server: nl.example.org
    port: 443
    uuid: 5783a3e7-e373-51cd-8642-c83782b807c5
    network: tcp
    tls: true
    udp: true
    flow: xtls-rprx-vision
    servername: www.example.com
    client-fingerprint: chrome
    reality-opts:
      public-key: Y4CRAOigwd_rCfP7TmrAzkDtnlNMBXxThbt1Uy8lHwY
      short-id: "6ba8"
  - name: DE vmess
    type: vmess
    server: de.example.org
    port: 8443
    uuid: 5783a3e7-e373-51cd-8642-c83782b807c5
    alterId: 0
    cipher: auto
    tls: true
    servername: de.example.org
    network: ws
    ws-opts:
      path: /vm
      headers:
        Host: cdn.example.org
  - {name: FI trojan, type: trojan, server: fi.example.org, port: 443, password: "p@ss#1", network: grpc, grpc-opts: {grpc-service-name: tun}, sni: fi.example.org}
  - name: SE ss
    type: ss
    server: 203.0.113.5
    port: 8388
    cipher: 2022-blake3-aes-128-gcm
    password: "YctPZ6U7xPPcU+gp3u+0tx/tRizJN9K8y+uKlW2qjlI="
  - name: US hy2
    type: hysteria2
    server: us.example.org
    port: 443
    ports: 443,20000-20010
    password: "hy/2 pass"
    obfs: salamander
    obfs-password: cry
    sni: us.example.org
    fingerprint: "AB:CD:EF:01:23:45:67:89:AB:CD:EF:01:23:45:67:89:AB:CD:EF:01:23:45:67:89:AB:CD:EF:01:23:45:67:89"
  - name: JP tuic
    type: tuic
    server: jp.example.org
    port: 8443
    uuid: 5783a3e7-e373-51cd-8642-c83782b807c5
    password: tp
    alpn: [h3]
    congestion-controller: bbr
    udp-relay-mode: quic
    reduce-rtt: true
  - name: KZ mieru
    type: mieru
    server: kz.example.org
    port-range: 2090-2099
    transport: TCP
    username: u
    password: p
    multiplexing: MULTIPLEXING_LOW
  - name: wg
    type: wireguard
    server: wg.example.org
    port: 51820
  - name: obfs ss
    type: ss
    server: ss.example.org
    port: 8388
    cipher: aes-128-gcm
    password: x
    plugin: obfs
proxy-groups:
  - name: auto
    type: url-test
    proxies: [wg]
rules:
  - MATCH,auto
`

const singBoxConfig = `{
  "log": {"level": "warn"},
  "outbounds": [
    {"type": "selector", "tag": "proxy", "outbounds": ["NL", "DE"]},
    {"type": "urltest", "tag": "auto", "outbounds": ["NL", "DE"]},
    {"type": "vless", "tag": "NL", "server": "nl.example.org", "server_port": 443,
     "uuid": "5783a3e7-e373-51cd-8642-c83782b807c5", "flow": "xtls-rprx-vision",
     "tls": {"enabled": true, "server_name": "www.example.com",
             "utls": {"enabled": true, "fingerprint": "chrome"},
             "reality": {"enabled": true, "public_key": "Y4CRAOigwd_rCfP7TmrAzkDtnlNMBXxThbt1Uy8lHwY", "short_id": "6ba8"}}},
    {"type": "vmess", "tag": "DE", "server": "de.example.org", "server_port": 8443,
     "uuid": "5783a3e7-e373-51cd-8642-c83782b807c5", "security": "auto", "alter_id": 0,
     "tls": {"enabled": true, "server_name": "de.example.org"},
     "transport": {"type": "grpc", "service_name": "vm"}},
    {"type": "trojan", "tag": "FI", "server": "fi.example.org", "server_port": 443, "password": "secret",
     "tls": {"enabled": true, "server_name": "fi.example.org", "alpn": ["h2", "http/1.1"]},
     "transport": {"type": "httpupgrade", "host": "fi.example.org", "path": "/up"}},
    {"type": "shadowsocks", "tag": "SE", "server": "203.0.113.5", "server_port": 8388,
     "method": "chacha20-ietf-poly1305", "password": "pw"},
    {"type": "hysteria2", "tag": "US", "server": "us.example.org", "server_port": 443,
     "server_ports": ["20000:20010"], "password": "hy",
     "obfs": {"type": "salamander", "password": "cry"},
     "tls": {"enabled": true, "server_name": "us.example.org"}},
    {"type": "tuic", "tag": "JP", "server": "jp.example.org", "server_port": 8443,
     "uuid": "5783a3e7-e373-51cd-8642-c83782b807c5", "password": "tp", "congestion_control": "bbr",
     "tls": {"enabled": true, "server_name": "jp.example.org", "alpn": ["h3"]}},
    {"type": "naive", "tag": "UK", "server": "uk.example.org", "server_port": 443,
     "username": "user", "password": "pass", "extra_headers": {"X-Test": "1"},
     "tls": {"enabled": true, "server_name": "uk.example.org"}},
    {"type": "naive", "tag": "UK quic", "server": "uk.example.org", "server_port": 443, "quic": true,
     "username": "user", "password": "pass",
     "tls": {"enabled": true, "server_name": "uk.example.org"}},
    {"type": "wireguard", "tag": "wg", "server": "wg.example.org", "server_port": 51820},
    {"type": "direct", "tag": "direct"},
    {"type": "block", "tag": "block"},
    {"type": "dns", "tag": "dns-out"}
  ],
  "route": {"final": "proxy"}
}`

// importBody serves body as a subscription and imports it.
func importBody(t *testing.T, body string) *profile.Profile {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
	defer srv.Close()
	_, p, err := (&Client{HTTP: srv.Client()}).Import(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// checkServers checks the transport names and that real Xray takes the
// config built from them.
func checkServers(t *testing.T, p *profile.Profile, names []string) map[string]string {
	t.Helper()
	ts := p.Exits[0].Transports
	got := map[string]string{}
	var order []string
	for _, tr := range ts {
		got[tr.Name] = string(tr.Xray)
		order = append(order, tr.Name)
	}
	if strings.Join(order, "|") != strings.Join(names, "|") {
		t.Fatalf("servers %q, want %q", order, names)
	}
	cfg, err := xrayconf.Build(p, xrayconf.Options{Inbounds: []xrayconf.Inbound{{Tag: "tun-in", Protocol: "tun"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Load(cfg); err != nil {
		t.Fatal(err)
	}
	return got
}

func wants(t *testing.T, name, xray string, parts ...string) {
	t.Helper()
	for _, s := range parts {
		if !strings.Contains(xray, s) {
			t.Errorf("%s: no %s in %s", name, s, xray)
		}
	}
}

func TestClashSubscription(t *testing.T) {
	p := importBody(t, clashConfig)
	got := checkServers(t, p, []string{"🇳🇱 NL reality", "DE vmess", "FI trojan", "SE ss", "US hy2", "JP tuic", "KZ mieru"})
	wants(t, "vless", got["🇳🇱 NL reality"], `"security":"reality"`, `"publicKey":"Y4CRAOigwd_rCfP7TmrAzkDtnlNMBXxThbt1Uy8lHwY"`, `"shortId":"6ba8"`, `"serverName":"www.example.com"`, `"flow":"xtls-rprx-vision"`)
	wants(t, "vmess", got["DE vmess"], `"protocol":"vmess"`, `"network":"ws"`, `"path":"/vm"`, `"host":"cdn.example.org"`, `"security":"tls"`)
	wants(t, "trojan", got["FI trojan"], `"password":"p@ss#1"`, `"network":"grpc"`, `"serviceName":"tun"`)
	wants(t, "ss", got["SE ss"], `"method":"2022-blake3-aes-128-gcm"`, `"password":"YctPZ6U7xPPcU+gp3u+0tx/tRizJN9K8y+uKlW2qjlI="`)
	wants(t, "hy2", got["US hy2"], `"auth":"hy/2 pass"`, `"type":"salamander"`, `"ports":"443,20000-20010"`, `"pinnedPeerCertSha256":"abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"`)
	wants(t, "tuic", got["JP tuic"], `"protocol":"tuic"`, `"congestionControl":"bbr"`, `"zeroRTTHandshake":true`)
	wants(t, "mieru", got["KZ mieru"], `"protocol":"mieru"`, `"port":"2090-2099"`, `"multiplexing":"MULTIPLEXING_LOW"`)
}

func TestSingBoxSubscription(t *testing.T) {
	p := importBody(t, singBoxConfig)
	got := checkServers(t, p, []string{"NL", "DE", "FI", "SE", "US", "JP", "UK", "UK quic"})
	wants(t, "vless", got["NL"], `"security":"reality"`, `"shortId":"6ba8"`, `"fingerprint":"chrome"`)
	wants(t, "vmess", got["DE"], `"network":"grpc"`, `"serviceName":"vm"`, `"security":"tls"`)
	wants(t, "trojan", got["FI"], `"network":"httpupgrade"`, `"path":"/up"`, `"alpn":["h2","http/1.1"]`)
	wants(t, "ss", got["SE"], `"method":"chacha20-ietf-poly1305"`)
	wants(t, "hy2", got["US"], `"ports":"20000-20010"`, `"type":"salamander"`)
	wants(t, "tuic", got["JP"], `"congestionControl":"bbr"`)
	wants(t, "naive", got["UK"], `"protocol":"naive"`, `"username":"user"`, `"X-Test":"1"`)
	wants(t, "naive quic", got["UK quic"], `"protocol":"naive"`, `"quic":true`, `"alpn":["h3"]`)
}

// Servers that skip certificate checks keep saying so: the import then pins
// their certificate (package certpin) or drops them.
func TestFormatsInsecure(t *testing.T) {
	c, _, _ := clashLinks([]byte("proxies:\n  - {name: a, type: trojan, server: a.example.org, port: 443, password: x, skip-cert-verify: true}\n"))
	s, _, _ := singBoxLinks([]byte(`{"outbounds": [{"type": "hysteria2", "tag": "a", "server": "a.example.org", "server_port": 443, "password": "x", "tls": {"enabled": true, "insecure": true}}]}`))
	for _, l := range append(c, s...) {
		if !strings.Contains(l, "insecure=1") {
			t.Errorf("no insecure in %s", l)
		}
	}
	if len(c)+len(s) != 2 {
		t.Errorf("links %v %v", c, s)
	}
}

func TestFormatsWithoutUsableServers(t *testing.T) {
	for name, body := range map[string]string{
		"clash":   "proxies:\n  - {name: sn, type: snell, server: sn.example.org, port: 51820}\n",
		"singbox": `{"outbounds": [{"type": "ssh", "tag": "sn"}, {"type": "direct", "tag": "direct"}]}`,
	} {
		links, ok, err := clashLinks([]byte(body))
		if name == "singbox" {
			links, ok, err = singBoxLinks([]byte(body))
		}
		if !ok || len(links) != 0 || err == nil || !(strings.Contains(err.Error(), "snell") || strings.Contains(err.Error(), "ssh")) {
			t.Errorf("%s: %v %v %v", name, links, ok, err)
		}
	}
}

func TestFormatsNotMistaken(t *testing.T) {
	for _, body := range []string{foreignLinks, "nothing here", "dmxlc3M6Ly8=", `[{"outbounds": [{"protocol": "vless"}]}]`, `{"outbounds": [{"protocol": "vless"}]}`} {
		if _, ok, _ := clashLinks([]byte(body)); ok && !strings.HasPrefix(body, "{") && !strings.HasPrefix(body, "[") {
			t.Errorf("clash took %q", body)
		}
		if _, ok, _ := singBoxLinks([]byte(body)); ok {
			t.Errorf("sing-box took %q", body)
		}
	}
}

const (
	wgPrivate = "eCtXsJZ27+4PbhDkHnB923tkUn2Gj59wZw5wFA75MnU="
	wgPublic  = "Cr8hWlKvtDt7nrvf+f0brNQQzabAqrjfBvas9pmowjo="
)

// WireGuard and AmneziaWG servers in Clash (Mihomo) and sing-box configs,
// and a subscription link that downloads an AmneziaWG .conf.
func TestWireGuardSubscriptions(t *testing.T) {
	clash := `proxies:
  - name: AWG
    type: wireguard
    server: awg.example.org
    port: 51820
    ip: 10.8.1.2
    ipv6: fd00::2
    private-key: ` + wgPrivate + `
    public-key: ` + wgPublic + `
    mtu: 1280
    dns: [1.1.1.1]
    amnezia-wg-option:
      jc: 4
      jmin: 40
      jmax: 70
      s1: 52
      s2: 71
      s3: 20
      s4: 10
      h1: 100000-200000
      h2: 300000-400000
      h3: 500000-600000
      h4: 700000-800000
      i1: "<r 16>"
  - name: WG peers
    type: wireguard
    ip: 10.8.1.3
    private-key: ` + wgPrivate + `
    peers:
      - server: 203.0.113.7
        port: 51820
        public-key: ` + wgPublic + `
        allowed-ips: ["0.0.0.0/0"]
`
	got := checkServers(t, importBody(t, clash), []string{"AWG", "WG peers"})
	wants(t, "clash awg", got["AWG"], `"protocol":"wireguard"`, `"endpoint":"awg.example.org:51820"`, `"address":["10.8.1.2/32","fd00::2/128"]`,
		`"H1":"100000-200000"`, `"S4":"10"`, `"Jmin":"40"`, `r 16\u003e`, `"secretKey":"`+wgPrivate+`"`)
	wants(t, "clash peers", got["WG peers"], `"endpoint":"203.0.113.7:51820"`, `"allowedIPs":["0.0.0.0/0"]`)

	singBox := `{
  "endpoints": [{"type": "wireguard", "tag": "SB endpoint", "address": ["10.8.1.2/32"], "private_key": "` + wgPrivate + `",
    "peers": [{"address": "sb.example.org", "port": 51820, "public_key": "` + wgPublic + `", "allowed_ips": ["0.0.0.0/0"], "persistent_keepalive_interval": 15}],
    "amnezia": {"jc": 3, "jmin": 10, "jmax": 30, "h1": "1-100"}}],
  "outbounds": [
    {"type": "wireguard", "tag": "SB outbound", "server": "203.0.113.8", "server_port": 2408, "local_address": ["172.16.0.2"],
      "private_key": "` + wgPrivate + `", "peer_public_key": "` + wgPublic + `", "mtu": 1280},
    {"type": "direct", "tag": "direct"}
  ]
}`
	got = checkServers(t, importBody(t, singBox), []string{"SB outbound", "SB endpoint"})
	wants(t, "sing-box endpoint", got["SB endpoint"], `"endpoint":"sb.example.org:51820"`, `"keepAlive":15`, `"Jc":"3"`, `"H1":"1-100"`)
	wants(t, "sing-box outbound", got["SB outbound"], `"endpoint":"203.0.113.8:2408"`, `"address":["172.16.0.2/32"]`, `"mtu":1280`)

	conf := "[Interface]\nPrivateKey = " + wgPrivate + "\nAddress = 10.8.1.2/32\nJc = 4\nS3 = 20\n\n[Peer]\nPublicKey = " + wgPublic + "\nEndpoint = 203.0.113.9:443\n"
	p := importBody(t, conf)
	got = checkServers(t, p, []string{"203.0.113.9"})
	wants(t, "conf", got["203.0.113.9"], `"S3":"20"`, `"endpoint":"203.0.113.9:443"`)
	if d := xrayconf.Describe(p.Exits[0].Transports[0].Xray); d != "AmneziaWG 2" {
		t.Errorf("described as %q", d)
	}
}
