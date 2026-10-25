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
		"hysteria2://x@1.2.3.4:443#HY",
		"trojan://secret@tr.example.org:443#DE",
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := p.Exits[0].Transports
	if skipped != 1 || len(ts) != 2 || ts[0].ID != "s1" || ts[1].Name != "DE" || ts[0].Prio != ts[1].Prio {
		t.Errorf("skipped %d, transports %+v", skipped, ts)
	}
	if _, _, err := FromLinks("x", []string{"hysteria2://x@1.2.3.4:443"}); err == nil || !strings.Contains(err.Error(), "нет серверов") {
		t.Errorf("got %v", err)
	}
}
