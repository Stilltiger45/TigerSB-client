package mobile

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Stilltiger45/TigerSB-client/internal/xrayconf"
	"github.com/Stilltiger45/TigerSB-client/pkg/profile"
)

// TestFreezeAdvice checks what the watchdog tells on a network that freezes
// connections: the protocols over UDP the user turned off, or, without any,
// to ask for them.
func TestFreezeAdvice(t *testing.T) {
	grpc := json.RawMessage(`{"protocol":"vless","streamSettings":{"network":"grpc","security":"reality"}}`)
	hy2 := json.RawMessage(`{"protocol":"hysteria","settings":{"version":2}}`)
	tuic := json.RawMessage(`{"protocol":"tuic"}`)
	prof := func(xs ...json.RawMessage) *profile.Profile {
		e := profile.Exit{ID: "de", Role: profile.RoleForeign}
		for _, x := range xs {
			e.Transports = append(e.Transports, profile.Transport{Xray: x})
		}
		return &profile.Profile{Exits: []profile.Exit{e}}
	}
	whole := prof(grpc, hy2, tuic)
	only := prof(grpc)
	if a := freezeAdvice(whole, only); !strings.Contains(a, "Включите") || !strings.Contains(a, "Hysteria 2, TUIC") {
		t.Errorf("turned off: %q", a)
	}
	if a := freezeAdvice(only, only); !strings.Contains(a, "администратора") {
		t.Errorf("none in the profile: %q", a)
	}
	if a := freezeAdvice(whole, whole); a == "" || strings.Contains(a, "Включите") {
		t.Errorf("on: %q", a)
	}
	if !xrayconf.OverUDP(hy2) || xrayconf.OverUDP(grpc) {
		t.Error("OverUDP")
	}
}
