package profile

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestParseLinkProtocols(t *testing.T) {
	vmess := base64.StdEncoding.EncodeToString([]byte(`{"v":"2","ps":"DE vmess","add":"de.example.org","port":"443","id":"5783a3e7-e373-51cd-8642-c83782b807c5","aid":"0","net":"ws","host":"de.example.org","path":"/v","tls":"tls","sni":"de.example.org"}`))
	cases := []struct {
		link, protocol, name, network, security string
	}{
		{"trojan://secret@tr.example.org:443?type=grpc&serviceName=g#TR", "trojan", "TR", "grpc", "tls"},
		{"trojan://secret@1.2.3.4:443?security=none#plain", "trojan", "plain", "raw", ""},
		{"ss://" + base64.RawURLEncoding.EncodeToString([]byte("chacha20-ietf-poly1305:pass")) + "@1.2.3.4:8388#SS%20one", "shadowsocks", "SS one", "", ""},
		{"ss://2022-blake3-aes-128-gcm:a2V5a2V5a2V5a2V5a2V5aw==@1.2.3.4:8388#SS2022", "shadowsocks", "SS2022", "", ""},
		{"ss://" + base64.StdEncoding.EncodeToString([]byte("aes-256-gcm:pass@1.2.3.4:8388")) + "#old", "shadowsocks", "old", "", ""},
		{"vmess://" + vmess, "vmess", "DE vmess", "ws", "tls"},
	}
	for _, c := range cases {
		raw, name, err := ParseLink(c.link)
		if err != nil {
			t.Errorf("%s: %v", c.link, err)
			continue
		}
		var ob struct {
			Protocol string `json:"protocol"`
			Stream   *struct {
				Network  string `json:"network"`
				Security string `json:"security"`
			} `json:"streamSettings"`
		}
		if err := json.Unmarshal(raw, &ob); err != nil {
			t.Fatal(err)
		}
		if ob.Protocol != c.protocol || name != c.name {
			t.Errorf("%s: protocol %q name %q", c.link, ob.Protocol, name)
		}
		if c.network != "" && (ob.Stream == nil || ob.Stream.Network != c.network || ob.Stream.Security != c.security) {
			t.Errorf("%s: stream %+v", c.link, ob.Stream)
		}
	}
	for _, bad := range []string{"ss://onlybase64garbage@1.2.3.4:1", "ss://YWVzOnB3@1.2.3.4:8388?plugin=obfs", "vmess://notbase64!", "trojan://@1.2.3.4:443"} {
		if _, _, err := ParseLink(bad); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}

func TestFromLinksSkipsUnknown(t *testing.T) {
	p, skipped, err := FromLinks("Чужой VPN", []string{
		"vless://5783a3e7-e373-51cd-8642-c83782b807c5@1.2.3.4:443?security=none#NL",
		"wireguard://x@1.2.3.4:51820#WG",
		"trojan://secret@tr.example.org:443#DE",
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := p.Exits[0].Transports
	if skipped != 1 || len(ts) != 2 || ts[0].ID != "s1" || ts[1].Name != "DE" || ts[0].Prio != ts[1].Prio {
		t.Errorf("skipped %d, transports %+v", skipped, ts)
	}
	if _, _, err := FromLinks("x", []string{"wireguard://x@1.2.3.4:51820"}); err == nil || !strings.Contains(err.Error(), "нет серверов") {
		t.Errorf("got %v", err)
	}
}

func TestHysteria2Links(t *testing.T) {
	type stream struct {
		Network string `json:"network"`
		TLS     struct {
			ServerName    string   `json:"serverName"`
			ALPN          []string `json:"alpn"`
			Pin           string   `json:"pinnedPeerCertSha256"`
			AllowInsecure bool     `json:"allowInsecure"`
		} `json:"tlsSettings"`
		Hysteria struct {
			Version int    `json:"version"`
			Auth    string `json:"auth"`
		} `json:"hysteriaSettings"`
		Mask *struct {
			UDP []struct {
				Type     string `json:"type"`
				Settings struct {
					Password string `json:"password"`
				} `json:"settings"`
			} `json:"udp"`
			Quic *struct {
				Hop struct {
					Ports string `json:"ports"`
				} `json:"udpHop"`
			} `json:"quicParams"`
		} `json:"finalmask"`
	}
	type ob struct {
		Protocol string `json:"protocol"`
		Settings struct {
			Version int    `json:"version"`
			Address string `json:"address"`
			Port    int    `json:"port"`
		} `json:"settings"`
		Stream stream `json:"streamSettings"`
	}
	parse := func(link string) (ob, string) {
		t.Helper()
		raw, name, err := ParseLink(link)
		if err != nil {
			t.Fatalf("%s: %v", link, err)
		}
		var o ob
		if err := json.Unmarshal(raw, &o); err != nil {
			t.Fatal(err)
		}
		return o, name
	}

	pin := "AB:" + strings.Repeat("cd", 31)
	o, name := parse("hysteria2://user%3Apass@hy.example.org:8443/?sni=front.example.org&obfs=salamander&obfs-password=mask&pinSHA256=" + pin + "&insecure=1#HY%202")
	if o.Protocol != "hysteria" || o.Settings.Version != 2 || o.Settings.Address != "hy.example.org" || o.Settings.Port != 8443 || name != "HY 2" {
		t.Errorf("outbound %+v name %q", o, name)
	}
	s := o.Stream
	if s.Network != "hysteria" || s.Hysteria.Version != 2 || s.Hysteria.Auth != "user:pass" {
		t.Errorf("stream %+v", s)
	}
	if s.TLS.ServerName != "front.example.org" || len(s.TLS.ALPN) != 1 || s.TLS.ALPN[0] != "h3" {
		t.Errorf("tls %+v", s.TLS)
	}
	// A pin makes insecure=1 unnecessary.
	if s.TLS.Pin != "ab"+strings.Repeat("cd", 31) || s.TLS.AllowInsecure {
		t.Errorf("pin %q insecure %v", s.TLS.Pin, s.TLS.AllowInsecure)
	}
	if s.Mask == nil || len(s.Mask.UDP) != 1 || s.Mask.UDP[0].Type != "salamander" || s.Mask.UDP[0].Settings.Password != "mask" || s.Mask.Quic != nil {
		t.Errorf("finalmask %+v", s.Mask)
	}

	// Port lists hop; no port means 443; insecure without a pin is kept for
	// the importer to pin.
	o, _ = parse("hy2://secret@1.2.3.4:20000-20010,20020/?insecure=1")
	if o.Settings.Port != 20000 || o.Stream.Mask == nil || o.Stream.Mask.Quic == nil || o.Stream.Mask.Quic.Hop.Ports != "20000-20010,20020" {
		t.Errorf("hop %+v", o)
	}
	if !o.Stream.TLS.AllowInsecure || o.Stream.TLS.ServerName != "" {
		t.Errorf("tls %+v", o.Stream.TLS)
	}
	o, _ = parse("hysteria2://secret@[2001:db8::1]?mport=30000-30100")
	if o.Settings.Address != "2001:db8::1" || o.Settings.Port != 443 || o.Stream.Mask.Quic.Hop.Ports != "30000-30100" {
		t.Errorf("ipv6 %+v", o)
	}

	for _, bad := range []string{"hysteria2://1.2.3.4:443", "hysteria2://x@1.2.3.4:0", "hysteria2://x@1.2.3.4:443?obfs=salamander", "hysteria2://x@1.2.3.4?obfs=other&obfs-password=y"} {
		if _, _, err := ParseLink(bad); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
	if p, err := FromVLESS("hy2://secret@hy.example.org:443#one"); err != nil || p.Exits[0].Transports[0].ID != "hy2" {
		t.Errorf("single link: %v", err)
	}
}
