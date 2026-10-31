// Package xrayconf turns a profile and a mode into an Xray JSON config.
package xrayconf

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/Stilltiger45/TigerSB-client/pkg/profile"
)

// Mode is the routing mode the user picks.
type Mode string

const (
	// ModeAuto routes as ModeBasic and, with Options.Evasion, reaches the
	// servers through a DPI preset when they do not open plainly (package
	// steady picks it on every start and network change).
	ModeAuto Mode = "auto"
	// ModeBasic: Russian sites (.ru, .рф and Russian IPs), banks and
	// universities direct, everything else through the VPN.
	ModeBasic Mode = "basic"
	// ModeMain: blocked sites via the foreign exit, everything else via the RU
	// exit, banks and universities direct.
	ModeMain Mode = "main"
	// ModeAll: everything through the VPN, local networks and ShieldDomains
	// aside.
	ModeAll Mode = "all"
	// ModeOff: no VPN, everything direct.
	ModeOff Mode = "off"
)

// ParseMode reads a mode name.
func ParseMode(s string) (Mode, error) {
	switch m := Mode(s); m {
	case ModeAuto, ModeBasic, ModeMain, ModeAll, ModeOff:
		return m, nil
	}
	return "", fmt.Errorf("неизвестный режим %q (ожидается auto, basic, main, all или off)", s)
}

// Outbound tags. Transport tags are "<role>/<exit>/<transport>" so the
// observatory selects a role by prefix.
const (
	TagDirect = "direct"
	TagBlock  = "block"
	TagDNS    = "dns-internal"
	// TagDNSOut answers DNS queries that apps send into the TUN.
	TagDNSOut = "dns-out"
)

// PickTag is the outbound that carries a role's traffic when the role has
// several transports, or one with copies through the DPI presets: package
// steady adds it to Xray and keeps the fastest live transport behind it
// without flapping between them, through the preset in use.
func PickTag(role profile.Role) string { return "pick/" + string(role) }

// HasPick reports whether Build routes the role through PickTag: when it has
// several transports, or with evasion one that has preset copies.
func HasPick(p *profile.Profile, role profile.Role, evasion bool) bool {
	items := transports(p, role)
	if len(items) > 1 {
		return true
	}
	return evasion && len(items) == 1 && variantable(items[0].xray)
}

// DefaultMTU is the TUN MTU when the caller does not pick one.
const DefaultMTU = 1500

// DoHServer resolves blocked domains through the foreign exit.
const DoHServer = "https://1.1.1.1/dns-query"

// Inbound is a local proxy port the core listens on, or the system TUN.
type Inbound struct {
	Tag      string
	Protocol string // "socks", "http" or "tun"
	Listen   string
	Port     int
	MTU      int // tun only; 0 means DefaultMTU
	// User and Pass, when set, are the login a socks or http inbound asks
	// for: any program on the computer could use an open one, and learn
	// the server from it.
	User, Pass string
}

// Options are the inputs besides the profile.
type Options struct {
	Mode     Mode
	Inbounds []Inbound
	LogLevel string
	// LogFile, when set, gets Xray's log instead of stderr.
	LogFile string
	// Evasion adds, in ModeAuto, the DPI presets and the copies of the VPN
	// outbounds that dial through them (see Presets and Variants).
	Evasion bool
	// NoGeo leaves out the geosite: and geoip: lists, for a start before
	// geosite.dat and geoip.dat are downloaded: the rest of the routing works.
	NoGeo bool
}

// privateRanges never leave the device's own network.
var privateRanges = []string{
	"10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16",
	"172.16.0.0/12", "192.168.0.0/16", "224.0.0.0/4", "255.255.255.255/32",
	"::1/128", "fc00::/7", "fe80::/10",
}

type rule struct {
	Type        string   `json:"type"`
	InboundTag  []string `json:"inboundTag,omitempty"`
	Domain      []string `json:"domain,omitempty"`
	IP          []string `json:"ip,omitempty"`
	Port        string   `json:"port,omitempty"`
	Network     string   `json:"network,omitempty"`
	OutboundTag string   `json:"outboundTag,omitempty"`
	RuleTag     string   `json:"ruleTag,omitempty"`
}

type target struct {
	outbound string
}

func (t target) apply(r rule) rule {
	r.OutboundTag = t.outbound
	return r
}

// Build returns the Xray config as JSON.
func Build(p *profile.Profile, opt Options) ([]byte, error) {
	if opt.Mode == "" {
		opt.Mode = ModeMain
	}
	if opt.LogLevel == "" {
		opt.LogLevel = "warning"
	}

	outbounds := []any{
		map[string]any{"tag": TagDirect, "protocol": "freedom", "settings": map[string]any{"domainStrategy": "UseIPv4"}},
		map[string]any{"tag": TagBlock, "protocol": "blackhole"},
	}
	var observed []string
	var all []string
	targets := map[profile.Role]target{}
	evasion := opt.Mode == ModeAuto && opt.Evasion

	if opt.Mode != ModeOff {
		for _, role := range []profile.Role{profile.RoleForeign, profile.RoleRU} {
			tags, err := addTransports(p, role, &outbounds)
			if err != nil {
				return nil, err
			}
			all = append(all, tags...)
			if len(tags) > 1 {
				observed = append(observed, string(role)+"/")
			}
			switch {
			case HasPick(p, role, evasion):
				targets[role] = target{outbound: PickTag(role)}
			case len(tags) == 1:
				targets[role] = target{outbound: tags[0]}
			}
		}
	}

	direct := target{outbound: TagDirect}
	foreign, hasForeign := targets[profile.RoleForeign]
	ru, hasRU := targets[profile.RoleRU]
	if !hasRU {
		// Without an RU exit the "everything else" direction stays direct.
		ru = direct
	}
	// The VPN direction of the modes that do not tell exits apart: abroad,
	// or the RU exit when the profile has only that.
	vpn, hasVPN := foreign, hasForeign
	if !hasVPN {
		vpn, hasVPN = targets[profile.RoleRU]
	}
	if !hasVPN {
		vpn = direct
	}
	dnsVia, hasDNSVia := foreign, hasForeign
	if opt.Mode != ModeMain {
		dnsVia, hasDNSVia = vpn, hasVPN
	}

	directDomains, directIPs := directLists(p)
	switch opt.Mode {
	case ModeBasic, ModeAuto:
		directDomains = append(directDomains, BasicDirectDomains...)
		directIPs = append(directIPs, BasicDirectIPs...)
	case ModeAll:
		directDomains, directIPs = nil, nil
	}
	blocked := p.Routing.Blocked
	if opt.NoGeo {
		directDomains, directIPs = withoutGeo(directDomains), withoutGeo(directIPs)
		blocked.Domains, blocked.IPs = withoutGeo(blocked.Domains), withoutGeo(blocked.IPs)
	}

	var tunTags []string
	for _, ib := range opt.Inbounds {
		if ib.Protocol == "tun" {
			tunTags = append(tunTags, ib.Tag)
		}
	}

	var rules []rule
	if len(tunTags) > 0 {
		// Apps resolve names through the DNS address of the TUN; answer them
		// with Xray's own DNS so blocked domains are not resolved by the provider.
		outbounds = append(outbounds, map[string]any{"tag": TagDNSOut, "protocol": "dns"})
		rules = append(rules, rule{Type: "field", InboundTag: tunTags, Port: "53", OutboundTag: TagDNSOut, RuleTag: "tun-dns"})
	}
	if hasDNSVia {
		rules = append(rules, dnsVia.apply(rule{Type: "field", InboundTag: []string{TagDNS}, RuleTag: "dns"}))
	}
	rules = append(rules, direct.apply(rule{Type: "field", IP: privateRanges, RuleTag: "private"}))
	if len(tunTags) > 0 {
		// The TUN is IPv4-only in spirit: DNS answers carry no AAAA records and
		// IPv6 that still arrives is refused, so apps fall back to IPv4 at once
		// instead of leaking past the VPN or failing on servers without IPv6.
		rules = append(rules, rule{Type: "field", InboundTag: tunTags, IP: []string{"::/0"}, OutboundTag: TagBlock, RuleTag: "ipv6"})
	}
	if opt.Mode != ModeOff {
		rules = append(rules, direct.apply(rule{Type: "field", Domain: ShieldDomains, RuleTag: "shield"}))
	}
	if hasForeign && opt.Mode != ModeOff {
		rules = append(rules, foreign.apply(rule{Type: "field", Domain: ForeignOnlyDomains, RuleTag: "foreign-only"}))
	}
	if len(directDomains) > 0 {
		rules = append(rules, direct.apply(rule{Type: "field", Domain: directDomains, RuleTag: "direct-domains"}))
	}
	if len(directIPs) > 0 {
		rules = append(rules, direct.apply(rule{Type: "field", IP: directIPs, RuleTag: "direct-ips"}))
	}

	var rest target
	switch opt.Mode {
	case ModeMain:
		if len(blocked.Domains) > 0 {
			rules = append(rules, foreign.apply(rule{Type: "field", Domain: blocked.Domains, RuleTag: "blocked-domains"}))
		}
		if len(blocked.IPs) > 0 {
			rules = append(rules, foreign.apply(rule{Type: "field", IP: blocked.IPs, RuleTag: "blocked-ips"}))
		}
		rest = ru
		if !hasRU && len(blocked.Domains) == 0 && len(blocked.IPs) == 0 {
			// A bare vless:// key has no lists: without them the main mode
			// would never use the VPN, so send everything but the direct list through it.
			rest = foreign
		}
	case ModeAuto, ModeBasic, ModeAll:
		rest = vpn
	case ModeOff:
		rest = direct
	}
	rules = append(rules, rest.apply(rule{Type: "field", Network: "tcp,udp", RuleTag: "rest"}))
	if evasion && len(all) > 0 {
		extra, err := evasionOutbounds(p)
		if err != nil {
			return nil, err
		}
		outbounds = append(outbounds, extra...)
	}

	cfg := map[string]any{
		"log":       logConfig(opt),
		"inbounds":  inbounds(opt.Inbounds),
		"outbounds": outbounds,
		"routing": map[string]any{
			"domainStrategy": "IPIfNonMatch",
			"rules":          rules,
		},
		// Traffic counters of every outbound, read by package meter.
		"stats": map[string]any{},
		"policy": map[string]any{"system": map[string]any{
			"statsOutboundUplink":   true,
			"statsOutboundDownlink": true,
		}},
	}
	if len(observed) > 0 {
		probe := profile.DefaultProbeURL
		interval := "1m"
		if p.Probe != nil {
			probe = p.Probe.URL
			if p.Probe.Interval != "" {
				interval = p.Probe.Interval
			}
		}
		cfg["observatory"] = map[string]any{
			"subjectSelector":   observed,
			"probeURL":          probe,
			"probeInterval":     interval,
			"enableConcurrency": true,
		}
	}
	if hasDNSVia {
		cfg["dns"] = dnsConfig(opt.Mode, blocked.Domains, append(slices.Clone(ShieldDomains), directDomains...))
	}
	return json.MarshalIndent(cfg, "", "  ")
}

// Candidates returns the tags of the VPN outbounds Build makes for the
// profile, foreign exits first, each role best priority first. def are the
// foreign ones, which carry the VPN's traffic in every mode but the main
// one; without them all of them.
func Candidates(p *profile.Profile) (all, def []string) {
	for _, role := range []profile.Role{profile.RoleForeign, profile.RoleRU} {
		for _, it := range transports(p, role) {
			all = append(all, it.tag)
		}
		if role == profile.RoleForeign {
			def = slices.Clone(all)
		}
	}
	if len(def) == 0 {
		def = all
	}
	return all, def
}

// Transports returns the tags of the role's transports, best priority first.
func Transports(p *profile.Profile, role profile.Role) []string {
	var tags []string
	for _, it := range transports(p, role) {
		tags = append(tags, it.tag)
	}
	return tags
}

type transportItem struct {
	tag  string
	prio int
	xray json.RawMessage
}

// transports lists the transports of every exit with the role, best priority
// first.
func transports(p *profile.Profile, role profile.Role) []transportItem {
	var items []transportItem
	for _, e := range p.Exits {
		if e.Role != role {
			continue
		}
		for _, t := range e.Transports {
			items = append(items, transportItem{tag: string(role) + "/" + e.ID + "/" + t.ID, prio: t.Prio, xray: t.Xray})
		}
	}
	slices.SortStableFunc(items, func(a, b transportItem) int { return a.prio - b.prio })
	return items
}

// addTransports appends the outbounds of every exit with the role, best
// priority first, and returns their tags. Those over UDP send QUICNoise
// first.
func addTransports(p *profile.Profile, role profile.Role, outbounds *[]any) ([]string, error) {
	items := transports(p, role)
	tags := make([]string, 0, len(items))
	for _, it := range items {
		var ob map[string]json.RawMessage
		if err := json.Unmarshal(it.xray, &ob); err != nil {
			return nil, fmt.Errorf("транспорт %s: %w", it.tag, err)
		}
		tags = append(tags, it.tag)
		if carried(it.xray) {
			continue // added to Xray by its own package, see Carried
		}
		if OverUDP(it.xray) {
			if err := withQUICNoise(ob); err != nil {
				return nil, fmt.Errorf("транспорт %s: %w", it.tag, err)
			}
		}
		if err := withFewStreams(ob); err != nil {
			return nil, fmt.Errorf("транспорт %s: %w", it.tag, err)
		}
		tag, _ := json.Marshal(it.tag)
		ob["tag"] = tag
		*outbounds = append(*outbounds, ob)
	}
	return tags, nil
}

// Outbound is a transport's outbound with its tag in the config.
type Outbound struct {
	Tag  string
	Xray json.RawMessage
}

// carriers are the protocols Xray does not have. Each is carried by a
// package of this client: tuic, mieru, naive, and awg for WireGuard, which
// Xray has without AmneziaWG.
var carriers = map[string]bool{"tuic": true, "mieru": true, "naive": true, "wireguard": true}

// Carried lists the profile's transports of a protocol Xray does not have
// (tuic, mieru, naive or wireguard). Build leaves them out of the config but uses their
// tags in pick outbounds and rules like any other; the caller adds an
// outbound handler for each before starting Xray.
func Carried(p *profile.Profile, protocol string) []Outbound {
	var out []Outbound
	for _, role := range []profile.Role{profile.RoleForeign, profile.RoleRU} {
		for _, it := range transports(p, role) {
			if protocolOf(it.xray) == protocol {
				out = append(out, Outbound{Tag: it.tag, Xray: it.xray})
			}
		}
	}
	return out
}

func carried(xray json.RawMessage) bool { return carriers[protocolOf(xray)] }

func protocolOf(xray json.RawMessage) string {
	var ob struct {
		Protocol string `json:"protocol"`
	}
	json.Unmarshal(xray, &ob)
	return ob.Protocol
}

func directLists(p *profile.Profile) (domains, ips []string) {
	if !p.Routing.NoBuiltinDirect {
		domains = append(domains, BuiltinDirectDomains...)
	}
	domains = append(domains, p.Routing.Direct.Domains...)
	ips = append(ips, p.Routing.Direct.IPs...)
	return domains, ips
}

func dnsConfig(mode Mode, blocked, direct []string) map[string]any {
	servers := []any{}
	switch mode {
	case ModeMain:
		if len(blocked) > 0 {
			servers = append(servers, map[string]any{"address": DoHServer, "domains": blocked, "skipFallback": true})
		}
		servers = append(servers, "localhost")
	case ModeAll, ModeBasic, ModeAuto:
		if len(direct) > 0 {
			servers = append(servers, map[string]any{"address": "localhost", "domains": direct, "skipFallback": true})
		}
		servers = append(servers, DoHServer)
	case ModeOff:
		servers = append(servers, "localhost")
	}
	return map[string]any{"tag": TagDNS, "queryStrategy": "UseIPv4", "servers": servers}
}

func logConfig(opt Options) map[string]any {
	l := map[string]any{"loglevel": opt.LogLevel}
	if opt.LogFile != "" {
		l["error"] = opt.LogFile
		l["access"] = "none"
	}
	return l
}

func inbounds(in []Inbound) []any {
	out := make([]any, 0, len(in))
	for _, ib := range in {
		settings := map[string]any{}
		switch ib.Protocol {
		case "socks":
			settings = map[string]any{"udp": true, "auth": "noauth"}
			if ib.Pass != "" {
				settings["auth"] = "password"
				settings["accounts"] = []any{map[string]any{"user": ib.User, "pass": ib.Pass}}
			}
		case "http":
			if ib.Pass != "" {
				settings["accounts"] = []any{map[string]any{"user": ib.User, "pass": ib.Pass}}
			}
		case "tun":
			mtu := ib.MTU
			if mtu == 0 {
				mtu = DefaultMTU
			}
			settings = map[string]any{"name": "tigersb0", "MTU": mtu}
		}
		in := map[string]any{
			"tag":      ib.Tag,
			"protocol": ib.Protocol,
			"port":     ib.Port,
			"settings": settings,
			"sniffing": map[string]any{
				"enabled":      true,
				"destOverride": []string{"http", "tls", "quic"},
				"routeOnly":    true,
			},
		}
		if ib.Listen != "" {
			in["listen"] = ib.Listen
		}
		out = append(out, in)
	}
	return out
}

// withoutGeo returns list without its geosite: and geoip: entries.
func withoutGeo(list []string) []string {
	return slices.DeleteFunc(slices.Clone(list), func(s string) bool {
		return strings.HasPrefix(s, "geosite:") || strings.HasPrefix(s, "geoip:")
	})
}

// GeoData returns whether the mode's config refers to geosite: or geoip:
// lists, which need geosite.dat and geoip.dat next to the core, and the
// geoip: lists the mode adds to the profile's (package geodata fetches them).
func GeoData(p *profile.Profile, m Mode) (needed bool, extraIPs []string) {
	var lists []string
	switch m {
	case ModeBasic, ModeAuto:
		return true, BasicDirectIPs
	case ModeAll:
		return false, nil
	case ModeMain:
		lists = slices.Concat(p.Routing.Blocked.Domains, p.Routing.Blocked.IPs)
	}
	lists = slices.Concat(lists, p.Routing.Direct.Domains, p.Routing.Direct.IPs)
	for _, s := range lists {
		if strings.HasPrefix(s, "geosite:") || strings.HasPrefix(s, "geoip:") {
			return true, nil
		}
	}
	return false, nil
}
