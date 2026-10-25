package xrayconf_test

import (
	"testing"

	"github.com/Stilltiger45/TigerSB-client/internal/xrayconf"
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
