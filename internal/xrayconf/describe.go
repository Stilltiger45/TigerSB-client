package xrayconf

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/Stilltiger45/TigerSB-client/pkg/profile"
)

// Describe names a transport's protocol for people, such as
// "VLESS · Reality · gRPC" or "Hysteria 2". It returns "" for a config it
// cannot read.
func Describe(xray json.RawMessage) string {
	var ob struct {
		Protocol string          `json:"protocol"`
		Settings json.RawMessage `json:"settings"`
		Stream   struct {
			Network  string `json:"network"`
			Security string `json:"security"`
		} `json:"streamSettings"`
	}
	if json.Unmarshal(xray, &ob) != nil || ob.Protocol == "" {
		return ""
	}
	var settings struct {
		Version int  `json:"version"`
		QUIC    bool `json:"quic"`
	}
	json.Unmarshal(ob.Settings, &settings)
	switch ob.Protocol {
	case "hysteria":
		if settings.Version == 2 {
			return "Hysteria 2"
		}
		return "Hysteria"
	case "tuic":
		return "TUIC"
	// Mieru and NaïveProxy run over TCP or over UDP (QUIC), and the name
	// says which: one of them may work on a network where the other does not.
	case "mieru":
		if mieruOverUDP(ob.Settings) {
			return "Mieru · UDP"
		}
		return "Mieru · TCP"
	case "naive":
		if settings.QUIC {
			return "NaïveProxy · QUIC"
		}
		return "NaïveProxy · TCP"
	}
	parts := []string{protocolNames.name(ob.Protocol)}
	switch ob.Stream.Security {
	case "reality":
		parts = append(parts, "Reality")
	case "tls":
		parts = append(parts, "TLS")
	}
	switch n := ob.Stream.Network; n {
	case "", "tcp", "raw":
	default:
		parts = append(parts, networkNames.name(n))
	}
	return strings.Join(parts, " · ")
}

type names map[string]string

func (m names) name(s string) string {
	if n, ok := m[s]; ok {
		return n
	}
	return strings.ToUpper(s)
}

var protocolNames = names{
	"vless": "VLESS", "vmess": "VMess", "trojan": "Trojan", "shadowsocks": "Shadowsocks",
	"wireguard": "WireGuard", "socks": "SOCKS", "http": "HTTP",
}

var networkNames = names{
	"grpc": "gRPC", "xhttp": "XHTTP", "ws": "WebSocket", "httpupgrade": "HTTPUpgrade",
	"kcp": "mKCP", "h2": "HTTP/2", "http": "HTTP/2", "splithttp": "XHTTP",
}

// Protocols maps the tag of every VPN outbound Build makes for the profile to
// its Describe name.
func Protocols(p *profile.Profile) map[string]string {
	m := map[string]string{}
	for _, role := range []profile.Role{profile.RoleForeign, profile.RoleRU} {
		for _, it := range transports(p, role) {
			m[it.tag] = Describe(it.xray)
		}
	}
	return m
}

// ProtocolNames lists the protocols of the profile's transports as Describe
// names them, each once, the best abroad first: what the user can turn off
// (WithoutProtocols).
func ProtocolNames(p *profile.Profile) []string {
	var out []string
	for _, role := range []profile.Role{profile.RoleForeign, profile.RoleRU} {
		for _, it := range transports(p, role) {
			if name := Describe(it.xray); name != "" && !slices.Contains(out, name) {
				out = append(out, name)
			}
		}
	}
	return out
}

// WithoutProtocols returns the profile without the transports whose protocol
// (as Describe names it) is in off, and without the exits left with none. A
// profile left with no exit at all comes back as it was, with ok false: a
// VPN through protocols the user found poor beats none.
func WithoutProtocols(p *profile.Profile, off []string) (out *profile.Profile, ok bool) {
	if len(off) == 0 {
		return p, true
	}
	q := *p
	q.Exits = nil
	for _, e := range p.Exits {
		e.Transports = slices.DeleteFunc(slices.Clone(e.Transports), func(t profile.Transport) bool {
			return slices.Contains(off, Describe(t.Xray))
		})
		if len(e.Transports) > 0 {
			q.Exits = append(q.Exits, e)
		}
	}
	if len(q.Exits) == 0 {
		return p, false
	}
	return &q, true
}

// ServerHost returns the address of the profile's best server, abroad if
// there is one: what the app measures the network path to. "" when no
// transport names one.
func ServerHost(p *profile.Profile) string {
	for _, role := range []profile.Role{profile.RoleForeign, profile.RoleRU} {
		for _, it := range transports(p, role) {
			var ob struct {
				Settings struct {
					Address string `json:"address"`
					Vnext   []struct {
						Address string `json:"address"`
					} `json:"vnext"`
					Servers []struct {
						Address string `json:"address"`
					} `json:"servers"`
				} `json:"settings"`
			}
			if json.Unmarshal(it.xray, &ob) != nil {
				continue
			}
			switch s := ob.Settings; {
			case len(s.Vnext) > 0 && s.Vnext[0].Address != "":
				return s.Vnext[0].Address
			case len(s.Servers) > 0 && s.Servers[0].Address != "":
				return s.Servers[0].Address
			case s.Address != "":
				return s.Address
			}
		}
	}
	return ""
}
