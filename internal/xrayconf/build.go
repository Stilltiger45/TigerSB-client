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
	// ModeMain: blocked sites via the foreign exit, everything else via the RU
	// exit, banks and universities direct.
	ModeMain Mode = "main"
	// ModeAll: everything via the foreign exit except the direct list.
	ModeAll Mode = "all"
	// ModeOff: no VPN, everything direct.
	ModeOff Mode = "off"
)

// ParseMode reads a mode name.
func ParseMode(s string) (Mode, error) {
	switch m := Mode(s); m {
	case ModeMain, ModeAll, ModeOff:
		return m, nil
	}
	return "", fmt.Errorf("неизвестный режим %q (ожидается main, all или off)", s)
}

// Outbound and balancer tags. Transport tags are "<role>/<exit>/<transport>"
// so a balancer selects a role by prefix.
const (
	TagDirect = "direct"
	TagBlock  = "block"
	TagDNS    = "dns-internal"
	// TagDNSOut answers DNS queries that apps send into the TUN.
	TagDNSOut = "dns-out"
)

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
}

// Options are the inputs besides the profile.
type Options struct {
	Mode     Mode
	Inbounds []Inbound
	LogLevel string
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
	BalancerTag string   `json:"balancerTag,omitempty"`
	RuleTag     string   `json:"ruleTag,omitempty"`
}

type target struct {
	outbound string
	balancer string
}

func (t target) apply(r rule) rule {
	r.OutboundTag, r.BalancerTag = t.outbound, t.balancer
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
	var balancers []any
	var observed []string
	targets := map[profile.Role]target{}

	if opt.Mode != ModeOff {
		for _, role := range []profile.Role{profile.RoleForeign, profile.RoleRU} {
			tags, err := addTransports(p, role, &outbounds)
			if err != nil {
				return nil, err
			}
			switch len(tags) {
			case 0:
			case 1:
				targets[role] = target{outbound: tags[0]}
			default:
				// The first tag has the best priority: it carries traffic until
				// the observatory has measured the others.
				balancers = append(balancers, map[string]any{
					"tag":         string(role),
					"selector":    []string{string(role) + "/"},
					"strategy":    map[string]any{"type": "leastPing"},
					"fallbackTag": tags[0],
				})
				observed = append(observed, string(role)+"/")
				targets[role] = target{balancer: string(role)}
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

	directDomains, directIPs := directLists(p)
	blocked := p.Routing.Blocked

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
	if hasForeign {
		rules = append(rules, foreign.apply(rule{Type: "field", InboundTag: []string{TagDNS}, RuleTag: "dns"}))
	}
	rules = append(rules, direct.apply(rule{Type: "field", IP: privateRanges, RuleTag: "private"}))
	if len(tunTags) > 0 {
		// The TUN is IPv4-only in spirit: DNS answers carry no AAAA records and
		// IPv6 that still arrives is refused, so apps fall back to IPv4 at once
		// instead of leaking past the VPN or failing on servers without IPv6.
		rules = append(rules, rule{Type: "field", InboundTag: tunTags, IP: []string{"::/0"}, OutboundTag: TagBlock, RuleTag: "ipv6"})
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
	case ModeAll:
		rest = foreign
	case ModeOff:
		rest = direct
	}
	rules = append(rules, rest.apply(rule{Type: "field", Network: "tcp,udp", RuleTag: "rest"}))

	cfg := map[string]any{
		"log":       map[string]any{"loglevel": opt.LogLevel},
		"inbounds":  inbounds(opt.Inbounds),
		"outbounds": outbounds,
		"routing": map[string]any{
			"domainStrategy": "IPIfNonMatch",
			"rules":          rules,
		},
	}
	if len(balancers) > 0 {
		cfg["routing"].(map[string]any)["balancers"] = balancers
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
	if hasForeign {
		cfg["dns"] = dnsConfig(opt.Mode, blocked.Domains, directDomains)
	}
	return json.MarshalIndent(cfg, "", "  ")
}

// addTransports appends the outbounds of every exit with the role, best
// priority first, and returns their tags.
func addTransports(p *profile.Profile, role profile.Role, outbounds *[]any) ([]string, error) {
	type item struct {
		tag  string
		prio int
		xray json.RawMessage
	}
	var items []item
	for _, e := range p.Exits {
		if e.Role != role {
			continue
		}
		for _, t := range e.Transports {
			items = append(items, item{tag: string(role) + "/" + e.ID + "/" + t.ID, prio: t.Prio, xray: t.Xray})
		}
	}
	slices.SortStableFunc(items, func(a, b item) int { return a.prio - b.prio })
	tags := make([]string, 0, len(items))
	for _, it := range items {
		var ob map[string]json.RawMessage
		if err := json.Unmarshal(it.xray, &ob); err != nil {
			return nil, fmt.Errorf("транспорт %s: %w", it.tag, err)
		}
		tag, _ := json.Marshal(it.tag)
		ob["tag"] = tag
		*outbounds = append(*outbounds, ob)
		tags = append(tags, it.tag)
	}
	return tags, nil
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
	case ModeAll:
		if len(direct) > 0 {
			servers = append(servers, map[string]any{"address": "localhost", "domains": direct, "skipFallback": true})
		}
		servers = append(servers, DoHServer)
	case ModeOff:
		servers = append(servers, "localhost")
	}
	return map[string]any{"tag": TagDNS, "queryStrategy": "UseIPv4", "servers": servers}
}

func inbounds(in []Inbound) []any {
	out := make([]any, 0, len(in))
	for _, ib := range in {
		settings := map[string]any{}
		switch ib.Protocol {
		case "socks":
			settings = map[string]any{"udp": true, "auth": "noauth"}
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

// HasGeoData reports whether the config refers to geosite: or geoip: lists,
// which need geosite.dat and geoip.dat next to the core.
func HasGeoData(p *profile.Profile) bool {
	all := slices.Concat(p.Routing.Blocked.Domains, p.Routing.Blocked.IPs, p.Routing.Direct.Domains, p.Routing.Direct.IPs)
	for _, s := range all {
		if strings.HasPrefix(s, "geosite:") || strings.HasPrefix(s, "geoip:") {
			return true
		}
	}
	return false
}
