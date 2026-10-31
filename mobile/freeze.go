package mobile

import (
	"slices"
	"strings"
	"time"

	"github.com/Stilltiger45/TigerSB-client/internal/engine"
	"github.com/Stilltiger45/TigerSB-client/internal/meter"
	"github.com/Stilltiger45/TigerSB-client/internal/xrayconf"
	"github.com/Stilltiger45/TigerSB-client/pkg/profile"
)

// Freezing is the watchdog's last look before it restarts the core. When the
// checks fail on connections that have been open a while but pass on fresh
// ones, the network freezes connections after some data, as DPI does, and a
// restart would only break every connection the apps have. Freezing returns
// what to tell the user then, and "" when the server does not answer on
// fresh connections either, so that a restart may help. It closes the
// connections the core has open and takes up to 5 seconds.
func Freezing() string {
	mu.Lock()
	i, def, url, hint := inst, pingTags, probeURL, freezeHint
	mu.Unlock()
	if i == nil || len(def) == 0 {
		return ""
	}
	engine.CloseConnections()
	tag := inUse(i, def)
	if meter.Check(i, tag, meter.CheckURLs(xrayconf.BaseTag(tag), url), 5*time.Second) < 0 {
		return ""
	}
	return hint
}

// freezeAdvice says what helps on a network that freezes connections, for
// the profile whole of which running is the part with the protocols the
// user left on: the protocols over UDP get past it.
func freezeAdvice(whole, running *profile.Profile) string {
	if len(udpAbroad(running)) > 0 {
		return "Провайдер замораживает соединения: переоткрываю их"
	}
	if off := udpAbroad(whole); len(off) > 0 {
		return "Провайдер замораживает TCP-соединения. Включите в «Протоколах»: " + strings.Join(off, ", ")
	}
	return "Провайдер замораживает TCP-соединения. Попросите администратора сервера включить Hysteria 2 или TUIC"
}

// udpAbroad lists the protocols over UDP among the profile's transports
// abroad, each once.
func udpAbroad(p *profile.Profile) []string {
	var out []string
	for _, e := range p.Exits {
		if e.Role != profile.RoleForeign {
			continue
		}
		for _, t := range e.Transports {
			if n := xrayconf.Describe(t.Xray); xrayconf.OverUDP(t.Xray) && !slices.Contains(out, n) {
				out = append(out, n)
			}
		}
	}
	return out
}
