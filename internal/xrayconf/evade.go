package xrayconf

import (
	"encoding/json"
	"fmt"
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

// NoisePacket is one packet of junk: Size is the range of its length in
// bytes, Delay the range of the pause after it in milliseconds.
type NoisePacket struct {
	Size, Delay [2]int
}

// QUICNoise is the junk sent before the first packet of every Hysteria 2
// and TUIC connection. They run QUIC over UDP, which DPI in Russia filters:
// DPI that tells a flow by its first packet sees random bytes instead of a
// QUIC handshake and lets the flow be, and the server drops the junk. DPI
// that reads every packet still sees QUIC: only obfuscation on the server
// (Hysteria 2's salamander) hides it from that.
//
// It goes out in every mode, not only the smart one: Xray keeps one Hysteria
// 2 client per server address for the life of the process and builds it
// with the settings of the core that used the server first, so noise that
// depended on the mode would stay off after a switch to the smart mode.
var QUICNoise = []NoisePacket{
	{Size: [2]int{40, 120}, Delay: [2]int{5, 15}},
	{Size: [2]int{40, 120}, Delay: [2]int{5, 15}},
}

// quicNoiseMask is QUICNoise as Xray's finalmask for UDP.
func quicNoiseMask() map[string]any {
	var items []any
	for _, n := range QUICNoise {
		items = append(items, map[string]any{
			"rand":  fmt.Sprintf("%d-%d", n.Size[0], n.Size[1]),
			"delay": fmt.Sprintf("%d-%d", n.Delay[0], n.Delay[1]),
		})
	}
	return map[string]any{"type": "noise", "settings": map[string]any{"noise": items}}
}

// withQUICNoise returns the Hysteria 2 outbound with QUICNoise sent before
// its first packet. The noise goes first among the UDP masks, so it leaves
// the phone as is, ahead of the salamander-obfuscated packets.
func withQUICNoise(ob map[string]json.RawMessage) error {
	var stream map[string]any
	if raw, ok := ob["streamSettings"]; ok {
		if err := json.Unmarshal(raw, &stream); err != nil {
			return err
		}
	}
	if stream == nil {
		stream = map[string]any{}
	}
	mask, _ := stream["finalmask"].(map[string]any)
	if mask == nil {
		mask = map[string]any{}
	}
	udp, _ := mask["udp"].([]any)
	mask["udp"] = append([]any{quicNoiseMask()}, udp...)
	stream["finalmask"] = mask
	raw, err := json.Marshal(stream)
	ob["streamSettings"] = raw
	return err
}

// OverUDP reports whether the transport runs over UDP: Hysteria 2 and TUIC
// (QUIC), mKCP, mieru whose first port is UDP. DPI cuts these more readily
// than Xray's TCP transports.
func OverUDP(xray json.RawMessage) bool {
	var ob struct {
		Protocol string `json:"protocol"`
		Settings struct {
			Ports []struct {
				Protocol string `json:"protocol"`
			} `json:"ports"`
		} `json:"settings"`
		Stream struct {
			Network string `json:"network"`
		} `json:"streamSettings"`
	}
	if json.Unmarshal(xray, &ob) != nil {
		return false
	}
	switch ob.Protocol {
	case "hysteria", "hysteria2", "tuic":
		return true
	case "mieru":
		return len(ob.Settings.Ports) > 0 && ob.Settings.Ports[0].Protocol == "UDP"
	}
	switch ob.Stream.Network {
	case "hysteria", "kcp", "mkcp":
		return true
	}
	return false
}

// UDPTransports returns the tags of the profile's transports over UDP.
func UDPTransports(p *profile.Profile) []string {
	var out []string
	for _, role := range []profile.Role{profile.RoleForeign, profile.RoleRU} {
		for _, it := range transports(p, role) {
			if OverUDP(it.xray) {
				out = append(out, it.tag)
			}
		}
	}
	return out
}

// Variants returns the VPN outbounds that get a copy per preset: all but
// those over UDP, which fragmentation does not help and which carry
// QUICNoise themselves, and those the panel already chains through another
// outbound.
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
	// The carried protocols are added by their own packages, which take no presets.
	return !OverUDP(xray) && !carried(xray) && json.Unmarshal(xray, &ob) == nil && ob.Stream.Sockopt.DialerProxy == ""
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
