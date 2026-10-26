package xrayconf

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/Stilltiger45/TigerSB-client/pkg/profile"
)

// Preset is one way to get past DPI without root, in place of zapret: TCP
// fragmentation of the first bytes a connection sends (the TLS ClientHello,
// which carries the site's name) and UDP noise sent before the first datagram
// (QUIC, Hysteria 2). Xray's freedom outbound does both.
type Preset struct {
	Name     string
	Fragment map[string]any
	Noises   []map[string]any
}

// Presets are what ModeAuto can use when a site or a server does not open
// plainly, mildest first. Preset k (from 1) is Presets[k-1]; package smart
// picks the one that works best on the network the phone is on. Each splits
// only the start of a connection into a capped number of pieces (maxSplit):
// that is enough for DPI and cheap for the phone, while splitting whole
// packets into tiny segments floods the network.
var Presets = []Preset{
	{
		Name:     "ClientHello по 100–200 байт, шум 10–20 байт",
		Fragment: map[string]any{"packets": "tlshello", "length": "100-200", "interval": "10-20", "maxSplit": "4-8"},
		Noises:   []map[string]any{{"type": "rand", "packet": "10-20", "delay": "10-16"}},
	},
	{
		Name:     "ClientHello по 10–30 байт, шум 2 × 50–100 байт",
		Fragment: map[string]any{"packets": "tlshello", "length": "10-30", "interval": "5-10", "maxSplit": "10-20"},
		Noises: []map[string]any{
			{"type": "rand", "packet": "50-100", "delay": "5-10"},
			{"type": "rand", "packet": "50-100", "delay": "5-10"},
		},
	},
	{
		// Splits the start of any protocol, not only TLS: plain VLESS and
		// Shadowsocks headers too. Only the first packet, and only its first
		// pieces: the rest goes in one segment.
		Name:     "начало соединения по 1–5 байт, шум 1–5 байт",
		Fragment: map[string]any{"packets": "1-1", "length": "1-5", "interval": "1-3", "maxSplit": "4-8"},
		Noises:   []map[string]any{{"type": "rand", "packet": "1-5", "delay": "5-10"}},
	},
}

// DPITag is the tag of the freedom outbound of preset k: sites that open
// neither through a server nor plainly go direct through it.
func DPITag(k int) string { return "dpi/" + strconv.Itoa(k) }

// VariantTag is the tag of a copy of the VPN outbound tag that reaches its
// server through preset k.
func VariantTag(k int, tag string) string { return DPITag(k) + ":" + tag }

// BaseTag strips the preset from a variant tag.
func BaseTag(tag string) string {
	if !strings.HasPrefix(tag, "dpi/") {
		return tag
	}
	if _, base, ok := strings.Cut(tag, ":"); ok {
		return base
	}
	return tag
}

// DPIOutbound is the freedom outbound of preset k.
func DPIOutbound(k int) map[string]any {
	pr := Presets[k-1]
	return map[string]any{
		"tag":      DPITag(k),
		"protocol": "freedom",
		"settings": map[string]any{"domainStrategy": "UseIPv4", "fragment": pr.Fragment, "noises": pr.Noises},
	}
}

// Variants returns the VPN outbounds that get a copy per preset: all but
// TUIC, which dials outside Xray, and those the panel already chains through
// another outbound.
func Variants(p *profile.Profile) []string {
	var out []string
	for _, role := range []profile.Role{profile.RoleForeign, profile.RoleRU} {
		for _, it := range transports(p, role) {
			if variantable(it.xray) {
				out = append(out, it.tag)
			}
		}
	}
	return out
}

func variantable(xray json.RawMessage) bool {
	var ob struct {
		Stream struct {
			Sockopt struct {
				DialerProxy string `json:"dialerProxy"`
			} `json:"sockopt"`
		} `json:"streamSettings"`
	}
	return !isTUIC(xray) && json.Unmarshal(xray, &ob) == nil && ob.Stream.Sockopt.DialerProxy == ""
}

// evasionOutbounds returns the preset outbounds and, for each preset, a copy
// of every variantable VPN outbound that dials its server through it.
func evasionOutbounds(p *profile.Profile) ([]any, error) {
	var out []any
	for k := range len(Presets) {
		out = append(out, DPIOutbound(k+1))
	}
	for _, role := range []profile.Role{profile.RoleForeign, profile.RoleRU} {
		for _, it := range transports(p, role) {
			if !variantable(it.xray) {
				continue
			}
			for k := range len(Presets) {
				var ob map[string]any
				if err := json.Unmarshal(it.xray, &ob); err != nil {
					return nil, err
				}
				stream, _ := ob["streamSettings"].(map[string]any)
				if stream == nil {
					stream = map[string]any{}
				}
				sockopt, _ := stream["sockopt"].(map[string]any)
				if sockopt == nil {
					sockopt = map[string]any{}
				}
				sockopt["dialerProxy"] = DPITag(k + 1)
				stream["sockopt"] = sockopt
				ob["streamSettings"] = stream
				ob["tag"] = VariantTag(k+1, it.tag)
				out = append(out, ob)
			}
		}
	}
	return out, nil
}
