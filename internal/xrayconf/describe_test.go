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
		`{"protocol":"shadowsocks"}`:                                               "Shadowsocks",
		`{"protocol":"hysteria","settings":{"version":2}}`:                         "Hysteria 2",
		`{"protocol":"tuic","streamSettings":{"network":"tuic"}}`:                  "TUIC",
		`{"protocol":"mieru","settings":{"config":"Cgc="}}`:                        "Mieru · TCP",
		`{"protocol":"naive","streamSettings":{"network":"raw","security":"tls"}}`: "NaïveProxy · TCP",
		`{"protocol":"naive","settings":{"quic":true}}`:                            "NaïveProxy · QUIC",
		`{"protocol":"vmess","streamSettings":{"network":"ws"}}`:                   "VMess · WebSocket",
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

func TestWithoutProtocols(t *testing.T) {
	tr := func(id, xray string) profile.Transport { return profile.Transport{ID: id, Xray: json.RawMessage(xray)} }
	hy := `{"protocol":"hysteria","settings":{"version":2,"address":"hy.example","port":443}}`
	grpc := `{"protocol":"vless","settings":{"vnext":[{"address":"a.example","port":443,"users":[{"id":"5783a3e7-e373-51cd-8642-c83782b807c5"}]}]},"streamSettings":{"network":"grpc","security":"reality"}}`
	tuic := `{"protocol":"tuic","streamSettings":{"network":"tuic"}}`
	p := &profile.Profile{Exits: []profile.Exit{
		{ID: "nl", Role: profile.RoleForeign, Transports: []profile.Transport{tr("g", grpc), tr("h", hy)}},
		{ID: "msk", Role: profile.RoleRU, Transports: []profile.Transport{tr("t", tuic), tr("h", hy)}},
	}}
	if got := xrayconf.ProtocolNames(p); len(got) != 3 || got[0] != "VLESS · Reality · gRPC" || got[1] != "Hysteria 2" || got[2] != "TUIC" {
		t.Errorf("names %q", got)
	}

	q, ok := xrayconf.WithoutProtocols(p, []string{"Hysteria 2", "TUIC"})
	if !ok || len(q.Exits) != 1 || q.Exits[0].ID != "nl" || len(q.Exits[0].Transports) != 1 || q.Exits[0].Transports[0].ID != "g" {
		t.Errorf("without Hysteria 2 and TUIC: %+v", q.Exits)
	}
	if len(p.Exits) != 2 || len(p.Exits[0].Transports) != 2 || len(p.Exits[1].Transports) != 2 {
		t.Errorf("the original profile changed: %+v", p.Exits)
	}
	if q, ok := xrayconf.WithoutProtocols(p, xrayconf.ProtocolNames(p)); ok || q != p {
		t.Error("everything turned off should leave the profile as it was")
	}
	if q, ok := xrayconf.WithoutProtocols(p, nil); !ok || q != p {
		t.Error("nothing turned off should leave the profile as it was")
	}
}
