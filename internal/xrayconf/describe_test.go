package xrayconf_test

import (
	"encoding/json"
	"testing"

	"github.com/Stilltiger45/TigerSB-client/internal/xrayconf"
	"github.com/Stilltiger45/TigerSB-client/pkg/profile"
)

func TestDescribe(t *testing.T) {
	for in, want := range map[string]string{
		`{"protocol":"vless","streamSettings":{"network":"grpc","security":"reality"}}`: "VLESS · Reality · gRPC",
		`{"protocol":"vless","streamSettings":{"network":"xhttp","security":"tls"}}`:    "VLESS · TLS · XHTTP",
		`{"protocol":"trojan","streamSettings":{"network":"tcp","security":"tls"}}`:     "Trojan · TLS",
		`{"protocol":"shadowsocks"}`:                              "Shadowsocks",
		`{"protocol":"hysteria","settings":{"version":2}}`:        "Hysteria 2",
		`{"protocol":"tuic","streamSettings":{"network":"tuic"}}`: "TUIC",
		`{"protocol":"vmess","streamSettings":{"network":"ws"}}`:  "VMess · WebSocket",
		`not json`: "",
	} {
		if got := xrayconf.Describe([]byte(in)); got != want {
			t.Errorf("%s: got %q, want %q", in, got, want)
		}
	}
}

func TestServerHost(t *testing.T) {
	p := fixture(t)
	want := ""
	for _, tag := range xrayconf.Transports(p, profile.RoleForeign)[:1] {
		for _, e := range p.Exits {
			for _, tr := range e.Transports {
				if "foreign/"+e.ID+"/"+tr.ID == tag {
					var ob struct {
						Settings struct {
							Vnext []struct{ Address string } `json:"vnext"`
						} `json:"settings"`
					}
					json.Unmarshal(tr.Xray, &ob)
					want = ob.Settings.Vnext[0].Address
				}
			}
		}
	}
	if got := xrayconf.ServerHost(p); got == "" || got != want {
		t.Errorf("server host %q, want the best foreign transport's %q", got, want)
	}
	hy := &profile.Profile{Exits: []profile.Exit{{ID: "x", Role: profile.RoleForeign, Transports: []profile.Transport{{ID: "h",
		Xray: json.RawMessage(`{"protocol":"hysteria","settings":{"version":2,"address":"hy.example","port":443}}`)}}}}}
	if got := xrayconf.ServerHost(hy); got != "hy.example" {
		t.Errorf("hysteria host %q", got)
	}
}
