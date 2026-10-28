package xrayconf

import (
	"encoding/json"
	"strings"

	"github.com/Stilltiger45/TigerSB-client/pkg/profile"
)

// Describe names a transport's protocol for people, such as
// "VLESS · Reality · gRPC" or "Hysteria 2". It returns "" for a config it
// cannot read.
func Describe(xray json.RawMessage) string {
	var ob struct {
		Protocol string `json:"protocol"`
		Settings struct {
			Version int `json:"version"`
		} `json:"settings"`
		Stream struct {
			Network  string `json:"network"`
			Security string `json:"security"`
		} `json:"streamSettings"`
	}
	if json.Unmarshal(xray, &ob) != nil || ob.Protocol == "" {
		return ""
	}
	switch ob.Protocol {
	case "hysteria":
		if ob.Settings.Version == 2 {
			return "Hysteria 2"
		}
		return "Hysteria"
	case "tuic":
		return "TUIC"
	case "mieru":
		return "Mieru"
	case "naive":
		return "NaïveProxy"
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
