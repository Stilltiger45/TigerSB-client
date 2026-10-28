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

func TestTUICLinks(t *testing.T) {
	type ob struct {
		Protocol string `json:"protocol"`
		Settings struct {
			Address           string `json:"address"`
			Port              int    `json:"port"`
			UUID              string `json:"uuid"`
			Password          string `json:"password"`
			CongestionControl string `json:"congestionControl"`
			UDPRelayMode      string `json:"udpRelayMode"`
			ZeroRTT           bool   `json:"zeroRTTHandshake"`
		} `json:"settings"`
		Stream struct {
			Network string `json:"network"`
			TLS     struct {
				ServerName    string   `json:"serverName"`
				ALPN          []string `json:"alpn"`
				AllowInsecure bool     `json:"allowInsecure"`
			} `json:"tlsSettings"`
		} `json:"streamSettings"`
	}
	raw, name, err := ParseLink("tuic://B831381D-6324-4d53-ad4f-8cda48b30811:p%40ss@tuic.example.org:8443?congestion_control=bbr&udp_relay_mode=quic&alpn=h3,spdy/3.1&reduce_rtt=1&allow_insecure=1#%D0%A2%D0%A3%D0%98%D0%9A")
	if err != nil {
		t.Fatal(err)
	}
	var o ob
	if err := json.Unmarshal(raw, &o); err != nil {
		t.Fatal(err)
	}
	s := o.Settings
	if o.Protocol != "tuic" || s.Address != "tuic.example.org" || s.Port != 8443 || s.UUID != "b831381d-6324-4d53-ad4f-8cda48b30811" ||
		s.Password != "p@ss" || s.CongestionControl != "bbr" || s.UDPRelayMode != "quic" || !s.ZeroRTT || name != "ТУИК" {
		t.Errorf("settings %+v, name %q", s, name)
	}
	tls := o.Stream.TLS
	if o.Stream.Network != "tuic" || tls.ServerName != "tuic.example.org" || strings.Join(tls.ALPN, ",") != "h3,spdy/3.1" || !tls.AllowInsecure {
		t.Errorf("stream %+v", o.Stream)
	}

	// Defaults.
	raw, _, err = ParseLink("tuic://b831381d-6324-4d53-ad4f-8cda48b30811:pw@1.2.3.4:443")
	if err != nil {
		t.Fatal(err)
	}
	o = ob{}
	json.Unmarshal(raw, &o)
	if o.Settings.CongestionControl != "cubic" || o.Settings.UDPRelayMode != "native" || o.Stream.TLS.ServerName != "" || strings.Join(o.Stream.TLS.ALPN, ",") != "h3" {
		t.Errorf("defaults %+v", o)
	}

	for _, bad := range []string{
		"tuic://not-a-uuid:pw@1.2.3.4:443",
		"tuic://b831381d-6324-4d53-ad4f-8cda48b30811@1.2.3.4:443",
		"tuic://b831381d-6324-4d53-ad4f-8cda48b30811:pw@1.2.3.4:443?congestion_control=brutal",
		"tuic://b831381d-6324-4d53-ad4f-8cda48b30811:pw@1.2.3.4:443?udp_relay_mode=x",
	} {
		if _, _, err := ParseLink(bad); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}

	p, err := FromVLESS("tuic://b831381d-6324-4d53-ad4f-8cda48b30811:pw@1.2.3.4:443")
	if err != nil || p.ID != "tuic" {
		t.Fatalf("profile %v, %v", p, err)
	}
}

func TestMieruLinks(t *testing.T) {
	type ob struct {
		Protocol string `json:"protocol"`
		Settings struct {
			Address  string `json:"address"`
			Username string `json:"username"`
			Password string `json:"password"`
			Profile  string `json:"profile"`
			Ports    []struct {
				Port     string `json:"port"`
				Protocol string `json:"protocol"`
			} `json:"ports"`
			MTU           int    `json:"mtu"`
			Multiplexing  string `json:"multiplexing"`
			HandshakeMode string `json:"handshakeMode"`
			Config        string `json:"config"`
		} `json:"settings"`
	}
	// As "mieru export config simple" writes it.
	raw, name, err := ParseLink("mierus://baozi:manlianpenfen@1.2.3.4?handshake-mode=HANDSHAKE_NO_WAIT&mtu=1400&multiplexing=MULTIPLEXING_HIGH&port=6666&port=9998-9999&port=6489&protocol=TCP&protocol=TCP&protocol=udp&profile=default")
	if err != nil {
		t.Fatal(err)
	}
	var o ob
	if err := json.Unmarshal(raw, &o); err != nil {
		t.Fatal(err)
	}
	s := o.Settings
	if o.Protocol != "mieru" || s.Address != "1.2.3.4" || s.Username != "baozi" || s.Password != "manlianpenfen" || s.Profile != "default" ||
		s.MTU != 1400 || s.Multiplexing != "MULTIPLEXING_HIGH" || s.HandshakeMode != "HANDSHAKE_NO_WAIT" || name != "default" {
		t.Errorf("settings %+v, name %q", s, name)
	}
	if len(s.Ports) != 3 || s.Ports[1].Port != "9998-9999" || s.Ports[1].Protocol != "TCP" || s.Ports[2].Protocol != "UDP" {
		t.Errorf("ports %+v", s.Ports)
	}

	full := "mieru://" + base64.StdEncoding.EncodeToString([]byte{0x0a, 0x07}) + "#%D0%9C%D0%B8%D1%80"
	raw, name, err = ParseLink(full)
	if err != nil {
		t.Fatal(err)
	}
	o = ob{}
	json.Unmarshal(raw, &o)
	if o.Protocol != "mieru" || o.Settings.Config != "Cgc=" || name != "Мир" {
		t.Errorf("mieru:// %s, name %q", raw, name)
	}

	for _, bad := range []string{
		"mierus://baozi@1.2.3.4?port=1&protocol=TCP",
		"mierus://baozi:pw@1.2.3.4?port=1",
		"mierus://baozi:pw@1.2.3.4?port=1&protocol=QUIC",
		"mierus://baozi:pw@1.2.3.4?port=70000&protocol=TCP",
		"mierus://baozi:pw@1.2.3.4?port=9-1&protocol=TCP",
		"mierus://baozi:pw@1.2.3.4?port=1&protocol=TCP&mtu=9000",
		"mieru://not base64!",
		"mieru://",
	} {
		if _, _, err := ParseLink(bad); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}

func TestNaiveLinks(t *testing.T) {
	type ob struct {
		Protocol string `json:"protocol"`
		Settings struct {
			Address  string            `json:"address"`
			Port     int               `json:"port"`
			Username string            `json:"username"`
			Password string            `json:"password"`
			Padding  bool              `json:"padding"`
			Headers  map[string]string `json:"headers"`
		} `json:"settings"`
		Stream struct {
			Network  string `json:"network"`
			Security string `json:"security"`
			TLS      struct {
				ServerName    string   `json:"serverName"`
				Fingerprint   string   `json:"fingerprint"`
				ALPN          []string `json:"alpn"`
				AllowInsecure bool     `json:"allowInsecure"`
				Pin           string   `json:"pinnedPeerCertSha256"`
			} `json:"tlsSettings"`
		} `json:"streamSettings"`
	}
	raw, name, err := ParseLink("naive+https://user:p%40ss@1.2.3.4:8443?sni=naive.example.org&extra-headers=X-A%3A%201%0D%0AX-B%3A2&insecure=1#%D0%9D%D0%B0%D0%B8%D0%B2")
	if err != nil {
		t.Fatal(err)
	}
	var o ob
	if err := json.Unmarshal(raw, &o); err != nil {
		t.Fatal(err)
	}
	s := o.Settings
	if o.Protocol != "naive" || s.Address != "1.2.3.4" || s.Port != 8443 || s.Username != "user" || s.Password != "p@ss" || !s.Padding ||
		s.Headers["X-A"] != "1" || s.Headers["X-B"] != "2" || name != "Наив" {
		t.Errorf("settings %+v, name %q", s, name)
	}
	tls := o.Stream.TLS
	if o.Stream.Network != "raw" || o.Stream.Security != "tls" || tls.ServerName != "naive.example.org" || tls.Fingerprint != "chrome" ||
		strings.Join(tls.ALPN, ",") != "h2" || !tls.AllowInsecure {
		t.Errorf("stream %+v", o.Stream)
	}

	// Defaults, and padding turned off.
	raw, _, err = ParseLink("naive+https://user:pw@naive.example.org?padding=false&pinSHA256=" + strings.Repeat("AB", 32))
	if err != nil {
		t.Fatal(err)
	}
	o = ob{}
	json.Unmarshal(raw, &o)
	if o.Settings.Port != 443 || o.Settings.Padding || o.Stream.TLS.ServerName != "naive.example.org" ||
		o.Stream.TLS.Pin != strings.Repeat("ab", 32) || o.Stream.TLS.AllowInsecure {
		t.Errorf("defaults %+v", o)
	}

	for _, bad := range []string{"naive+https://user:pw@:443", "naive+https://user:pw@1.2.3.4:99999", "naive+quic://user:pw@1.2.3.4:443"} {
		if _, _, err := ParseLink(bad); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}

	p, err := FromVLESS("naive+https://user:pw@1.2.3.4")
	if err != nil || p.ID != "naive-https" {
		t.Fatalf("profile %v, %v", p, err)
	}
}
