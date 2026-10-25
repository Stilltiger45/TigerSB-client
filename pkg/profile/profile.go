// Package profile defines the TigerSB client profile (format v1): the set of
// exits, their transports and the routing lists the panel hands to a client.
//
// The package has no dependencies outside the standard library so the panel
// can import it to build and sign profiles.
package profile

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"regexp"
	"strings"
	"time"
)

// Version is the profile format this package reads and writes.
const Version = 1

// Role says which side of the border an exit releases traffic on.
type Role string

const (
	// RoleForeign is an exit abroad (the panel's main server). Blocked sites go here.
	RoleForeign Role = "foreign"
	// RoleRU is an exit in Russia (the panel's routing point). Everything else goes here.
	RoleRU Role = "ru"
)

// Profile is everything a client needs to connect.
type Profile struct {
	V       int       `json:"v"`
	ID      string    `json:"id"`
	Name    string    `json:"name,omitempty"`
	Updated time.Time `json:"updated"`
	Update  *Update   `json:"update,omitempty"`
	Exits   []Exit    `json:"exits"`
	Routing Routing   `json:"routing"`
	Probe   *Probe    `json:"probe,omitempty"`
	Geodata *Geodata  `json:"geodata,omitempty"`
}

// Geodata says where to download geosite.dat and geoip.dat when the routing
// lists use "geosite:" or "geoip:". Empty fields fall back to the client's
// default source. The panel can serve files trimmed to the lists it uses.
type Geodata struct {
	GeoSite string `json:"geosite,omitempty"`
	GeoIP   string `json:"geoip,omitempty"`
}

// Update says where and how often the client refreshes the profile.
type Update struct {
	URL     string   `json:"url"`
	Every   string   `json:"every,omitempty"`
	Mirrors []string `json:"mirrors,omitempty"`
}

// Exit is one server (or one group of servers) that releases traffic.
type Exit struct {
	ID         string      `json:"id"`
	Role       Role        `json:"role"`
	Transports []Transport `json:"transports"`
}

// Transport is one way of reaching an exit. Xray holds an Xray outbound
// object without its tag: protocol, settings, streamSettings and mux.
type Transport struct {
	ID   string          `json:"id"`
	Prio int             `json:"prio"`
	Xray json.RawMessage `json:"xray"`
}

// Routing holds the lists for the three directions. Everything that is in
// neither list goes to the RU exit.
type Routing struct {
	// Blocked goes through the foreign exit.
	Blocked Matchers `json:"blocked"`
	// Direct bypasses the VPN: banks, university sites and other sites that
	// break with any VPN.
	Direct Matchers `json:"direct"`
	// NoBuiltinDirect turns off the client's built-in list of direct sites.
	NoBuiltinDirect bool `json:"no_builtin_direct,omitempty"`
}

// Matchers are Xray-style rule entries.
//
// Domains: "domain:", "full:", "keyword:", "regexp:", "geosite:" or a bare
// domain (read as "domain:"). IPs: an address, a CIDR or "geoip:". Apps:
// Android package names, honoured only for Direct.
type Matchers struct {
	Domains []string `json:"domains,omitempty"`
	IPs     []string `json:"ips,omitempty"`
	Apps    []string `json:"apps,omitempty"`
}

// Probe is the URL the client fetches through a transport to check it works.
type Probe struct {
	URL      string `json:"url"`
	Interval string `json:"interval,omitempty"`
}

// DefaultProbeURL answers 204 and is reachable from most networks.
const DefaultProbeURL = "https://www.gstatic.com/generate_204"

var (
	idRe  = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)
	appRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(\.[A-Za-z][A-Za-z0-9_]*)+$`)
)

// allowedProtocols are the outbound protocols a profile may use.
var allowedProtocols = map[string]bool{
	"vless":       true,
	"vmess":       true,
	"trojan":      true,
	"shadowsocks": true,
}

// allowedXrayKeys are the outbound fields a transport may set. The client
// sets the tag itself and does not let a profile chain or redirect outbounds.
var allowedXrayKeys = map[string]bool{
	"protocol":       true,
	"settings":       true,
	"streamSettings": true,
	"mux":            true,
}

// Validate checks the profile and normalises its routing lists in place.
func (p *Profile) Validate() error {
	if p.V != Version {
		return fmt.Errorf("версия профиля %d не поддерживается (ожидается %d)", p.V, Version)
	}
	if !idRe.MatchString(p.ID) {
		return fmt.Errorf("неверный id профиля %q", p.ID)
	}
	if len(p.Exits) == 0 {
		return fmt.Errorf("в профиле нет ни одного выхода")
	}
	exitIDs := map[string]bool{}
	for i := range p.Exits {
		e := &p.Exits[i]
		if !idRe.MatchString(e.ID) {
			return fmt.Errorf("неверный id выхода %q", e.ID)
		}
		if exitIDs[e.ID] {
			return fmt.Errorf("выход %q указан дважды", e.ID)
		}
		exitIDs[e.ID] = true
		if e.Role != RoleForeign && e.Role != RoleRU {
			return fmt.Errorf("выход %q: неизвестная роль %q (ожидается foreign или ru)", e.ID, e.Role)
		}
		if len(e.Transports) == 0 {
			return fmt.Errorf("выход %q: нет ни одного транспорта", e.ID)
		}
		trIDs := map[string]bool{}
		for _, t := range e.Transports {
			if !idRe.MatchString(t.ID) {
				return fmt.Errorf("выход %q: неверный id транспорта %q", e.ID, t.ID)
			}
			if trIDs[t.ID] {
				return fmt.Errorf("выход %q: транспорт %q указан дважды", e.ID, t.ID)
			}
			trIDs[t.ID] = true
			if err := validateXray(t.Xray); err != nil {
				return fmt.Errorf("выход %q, транспорт %q: %w", e.ID, t.ID, err)
			}
		}
	}
	if !p.HasRole(RoleForeign) {
		return fmt.Errorf("в профиле нет выхода за границей (role foreign)")
	}
	var err error
	if p.Routing.Blocked, err = normalizeMatchers(p.Routing.Blocked, false); err != nil {
		return fmt.Errorf("список blocked: %w", err)
	}
	if p.Routing.Direct, err = normalizeMatchers(p.Routing.Direct, true); err != nil {
		return fmt.Errorf("список direct: %w", err)
	}
	if p.HasRole(RoleRU) && len(p.Routing.Blocked.Domains) == 0 && len(p.Routing.Blocked.IPs) == 0 {
		return fmt.Errorf("есть выход в РФ, но список blocked пуст: заблокированные сайты не откроются")
	}
	if p.Update != nil && !strings.HasPrefix(p.Update.URL, "https://") {
		return fmt.Errorf("адрес обновления должен начинаться с https://")
	}
	if g := p.Geodata; g != nil {
		for _, u := range []string{g.GeoSite, g.GeoIP} {
			if u != "" && !strings.HasPrefix(u, "https://") {
				return fmt.Errorf("адрес geodata должен начинаться с https://")
			}
		}
	}
	return nil
}

// HasRole reports whether the profile has an exit with the given role.
func (p *Profile) HasRole(r Role) bool {
	for _, e := range p.Exits {
		if e.Role == r {
			return true
		}
	}
	return false
}

func validateXray(raw json.RawMessage) error {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return fmt.Errorf("поле xray должно быть объектом: %w", err)
	}
	for k := range obj {
		if !allowedXrayKeys[k] {
			return fmt.Errorf("поле xray.%s не разрешено", k)
		}
	}
	var proto string
	if err := json.Unmarshal(obj["protocol"], &proto); err != nil || !allowedProtocols[proto] {
		return fmt.Errorf("протокол %q не поддерживается", proto)
	}
	return nil
}

func normalizeMatchers(m Matchers, appsAllowed bool) (Matchers, error) {
	out := Matchers{}
	for _, d := range m.Domains {
		n, err := normalizeDomain(d)
		if err != nil {
			return out, err
		}
		out.Domains = append(out.Domains, n)
	}
	for _, ip := range m.IPs {
		n, err := normalizeIP(ip)
		if err != nil {
			return out, err
		}
		out.IPs = append(out.IPs, n)
	}
	if len(m.Apps) > 0 && !appsAllowed {
		return out, fmt.Errorf("приложения можно указывать только в списке direct")
	}
	for _, a := range m.Apps {
		if !appRe.MatchString(a) {
			return out, fmt.Errorf("неверное имя пакета приложения %q", a)
		}
		out.Apps = append(out.Apps, a)
	}
	return out, nil
}

func normalizeDomain(d string) (string, error) {
	d = strings.TrimSpace(d)
	prefix, value, found := strings.Cut(d, ":")
	if !found {
		prefix, value = "domain", d
	}
	if value == "" {
		return "", fmt.Errorf("пустое правило %q", d)
	}
	switch prefix {
	case "domain", "full":
		if strings.ContainsAny(value, " /:*") {
			return "", fmt.Errorf("неверный домен %q", d)
		}
		value = strings.ToLower(strings.TrimSuffix(value, "."))
	case "keyword", "geosite":
	case "regexp":
		if _, err := regexp.Compile(value); err != nil {
			return "", fmt.Errorf("неверное регулярное выражение %q: %w", d, err)
		}
	default:
		return "", fmt.Errorf("неизвестный тип правила %q", d)
	}
	return prefix + ":" + value, nil
}

func normalizeIP(s string) (string, error) {
	s = strings.TrimSpace(s)
	if v, ok := strings.CutPrefix(s, "geoip:"); ok {
		if v == "" {
			return "", fmt.Errorf("пустое правило %q", s)
		}
		return s, nil
	}
	if p, err := netip.ParsePrefix(s); err == nil {
		return p.Masked().String(), nil
	}
	if a, err := netip.ParseAddr(s); err == nil {
		return a.String(), nil
	}
	return "", fmt.Errorf("неверный адрес или подсеть %q", s)
}
