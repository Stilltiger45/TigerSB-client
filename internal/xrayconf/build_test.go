package xrayconf_test

import (
	"bytes"
	"encoding/json"
	"os"
	"slices"
	"testing"

	"github.com/Stilltiger45/TigerSB-client/internal/engine"
	"github.com/Stilltiger45/TigerSB-client/internal/xrayconf"
	"github.com/Stilltiger45/TigerSB-client/pkg/profile"
)

func fixture(t *testing.T) *profile.Profile {
	t.Helper()
	b, err := os.ReadFile("../../pkg/profile/testdata/profile.json")
	if err != nil {
		t.Fatal(err)
	}
	var p profile.Profile
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	return &p
}

type built struct {
	Outbounds []struct {
		Tag      string `json:"tag"`
		Protocol string `json:"protocol"`
	} `json:"outbounds"`
	Routing struct {
		Rules []struct {
			RuleTag     string   `json:"ruleTag"`
			OutboundTag string   `json:"outboundTag"`
			Domain      []string `json:"domain"`
		} `json:"rules"`
		Balancers []json.RawMessage `json:"balancers"`
	} `json:"routing"`
	Observatory *struct {
		SubjectSelector []string `json:"subjectSelector"`
	} `json:"observatory"`
	DNS *struct {
		Servers []json.RawMessage `json:"servers"`
	} `json:"dns"`
}

// build makes the config, checks that real Xray accepts it and decodes it.
func build(t *testing.T, p *profile.Profile, mode xrayconf.Mode) (built, map[string]string) {
	t.Helper()
	return buildOpt(t, p, xrayconf.Options{
		Mode:     mode,
		Inbounds: []xrayconf.Inbound{{Tag: "socks-in", Protocol: "socks", Listen: "127.0.0.1", Port: 10808}},
	})
}

func buildOpt(t *testing.T, p *profile.Profile, opt xrayconf.Options) (built, map[string]string) {
	t.Helper()
	mode := opt.Mode
	cfg, err := xrayconf.Build(p, opt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Load(cfg); err != nil {
		t.Fatalf("mode %s: %v\n%s", mode, err, cfg)
	}
	var b built
	if err := json.Unmarshal(cfg, &b); err != nil {
		t.Fatal(err)
	}
	// ruleTag -> where it sends traffic
	dest := map[string]string{}
	for _, r := range b.Routing.Rules {
		dest[r.RuleTag] = r.OutboundTag
	}
	return b, dest
}

func TestMainModeThreeDirections(t *testing.T) {
	b, dest := build(t, fixture(t), xrayconf.ModeMain)

	want := map[string]string{
		"dns":             "pick/foreign",
		"private":         "direct",
		"direct-domains":  "direct",
		"direct-ips":      "direct",
		"blocked-domains": "pick/foreign",
		"blocked-ips":     "pick/foreign",
		"rest":            "ru/msk-1/grpc",
	}
	for tag, to := range want {
		if dest[tag] != to {
			t.Errorf("rule %s goes to %q, want %q", tag, dest[tag], to)
		}
	}
	// Direct rules must come before blocked ones: a bank on a blocked list still goes direct.
	order := []string{}
	for _, r := range b.Routing.Rules {
		order = append(order, r.RuleTag)
	}
	if slices.Index(order, "direct-domains") > slices.Index(order, "blocked-domains") || order[len(order)-1] != "rest" {
		t.Errorf("rule order %v", order)
	}
	for _, r := range b.Routing.Rules {
		if r.RuleTag == "direct-domains" && !slices.Contains(r.Domain, "domain:example-bank.ru") || r.RuleTag == "direct-domains" && !slices.Contains(r.Domain, "domain:msu.ru") {
			t.Errorf("direct domains miss the profile or built-in list: %v", r.Domain)
		}
	}

	// Package steady keeps a transport behind pick/foreign, not an Xray balancer.
	if len(b.Routing.Balancers) != 0 {
		t.Errorf("balancers: %s", b.Routing.Balancers)
	}
	if got := xrayconf.Transports(fixture(t), profile.RoleForeign); !slices.Equal(got, []string{"foreign/nl-1/reality", "foreign/nl-1/xhttp-cdn"}) {
		t.Errorf("foreign transports %v, best priority first", got)
	}
	if b.Observatory == nil || !slices.Equal(b.Observatory.SubjectSelector, []string{"foreign/"}) {
		t.Errorf("observatory %+v", b.Observatory)
	}
	if b.DNS == nil || len(b.DNS.Servers) != 2 {
		t.Errorf("dns %+v", b.DNS)
	}
}

func TestAllMode(t *testing.T) {
	b, dest := build(t, fixture(t), xrayconf.ModeAll)
	if dest["rest"] != "pick/foreign" || dest["private"] != "direct" {
		t.Errorf("rules %v", dest)
	}
	if _, ok := dest["direct-domains"]; ok {
		t.Error("all mode sends banks through the VPN too")
	}
	if _, ok := dest["blocked-domains"]; ok {
		t.Error("all mode does not need blocked rules")
	}
	for _, o := range b.Outbounds {
		if o.Tag == "ru/msk-1/grpc" {
			return
		}
	}
	t.Error("ru outbound missing")
}

func TestOffMode(t *testing.T) {
	b, dest := build(t, fixture(t), xrayconf.ModeOff)
	if dest["rest"] != "direct" {
		t.Errorf("rules %v", dest)
	}
	for _, o := range b.Outbounds {
		if o.Protocol == "vless" {
			t.Errorf("off mode must not open VPN outbounds, got %s", o.Tag)
		}
	}
	if b.DNS != nil || b.Observatory != nil {
		t.Error("off mode needs no DNS over VPN or observatory")
	}
}

func TestNoRUExitGoesDirect(t *testing.T) {
	p := fixture(t)
	p.Exits = p.Exits[:1]
	_, dest := build(t, p, xrayconf.ModeMain)
	if dest["rest"] != "direct" {
		t.Errorf("rest goes to %q without an RU exit", dest["rest"])
	}
}

func TestSingleTransportNoPicker(t *testing.T) {
	p := fixture(t)
	p.Exits[0].Transports = p.Exits[0].Transports[1:]
	b, dest := build(t, p, xrayconf.ModeMain)
	if b.Observatory != nil {
		t.Error("one transport per exit needs no observatory")
	}
	if dest["blocked-domains"] != "foreign/nl-1/reality" {
		t.Errorf("blocked goes to %q", dest["blocked-domains"])
	}
}

func TestNoBuiltinDirect(t *testing.T) {
	p := fixture(t)
	p.Routing.NoBuiltinDirect = true
	b, _ := build(t, p, xrayconf.ModeMain)
	for _, r := range b.Routing.Rules {
		if r.RuleTag == "direct-domains" && !slices.Equal(r.Domain, []string{"domain:example-bank.ru"}) {
			t.Errorf("direct domains %v", r.Domain)
		}
	}
}

func TestVLESSProfileBuilds(t *testing.T) {
	link := "vless://5783a3e7-e373-51cd-8642-c83782b807c5@cdn.example.org:443?type=xhttp&security=tls&sni=cdn.example.org&path=%2Fx&mode=packet-up&alpn=h2,http/1.1&extra=%7B%22xPaddingBytes%22%3A%22100-1000%22%7D#cdn"
	p, err := profile.FromVLESS(link)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []xrayconf.Mode{xrayconf.ModeAuto, xrayconf.ModeBasic, xrayconf.ModeMain, xrayconf.ModeAll, xrayconf.ModeOff} {
		build(t, p, m)
	}
	for _, link := range []string{
		"vless://5783a3e7-e373-51cd-8642-c83782b807c5@1.2.3.4:443?type=grpc&security=tls&sni=a.example.org&serviceName=api&mode=multi",
		"vless://5783a3e7-e373-51cd-8642-c83782b807c5@1.2.3.4:443?type=ws&security=tls&host=a.example.org&path=%2Fws",
		"vless://5783a3e7-e373-51cd-8642-c83782b807c5@1.2.3.4:80?type=httpupgrade&path=%2Fu",
	} {
		p, err := profile.FromVLESS(link)
		if err != nil {
			t.Fatal(err)
		}
		build(t, p, xrayconf.ModeMain)
	}
}

func TestParseMode(t *testing.T) {
	if m, err := xrayconf.ParseMode("auto"); err != nil || m != xrayconf.ModeAuto {
		t.Errorf("auto: %v", err)
	}
	if _, err := xrayconf.ParseMode("smart"); err == nil {
		t.Error("unknown mode accepted")
	}
}

func TestBareVLESSMainModeUsesVPN(t *testing.T) {
	p, err := profile.FromVLESS("vless://5783a3e7-e373-51cd-8642-c83782b807c5@1.2.3.4:443?security=none")
	if err != nil {
		t.Fatal(err)
	}
	_, dest := build(t, p, xrayconf.ModeMain)
	if dest["rest"] != "foreign/link/vless" || dest["direct-domains"] != "direct" {
		t.Errorf("rules %v", dest)
	}
}

func TestTUNHijacksDNSAndRefusesIPv6(t *testing.T) {
	b, dest := buildOpt(t, fixture(t), xrayconf.Options{
		Mode:     xrayconf.ModeMain,
		Inbounds: []xrayconf.Inbound{{Tag: "tun-in", Protocol: "tun", MTU: 1400}},
	})
	if dest["tun-dns"] != xrayconf.TagDNSOut || dest["ipv6"] != xrayconf.TagBlock || dest["rest"] != "ru/msk-1/grpc" {
		t.Errorf("rules %v", dest)
	}
	// DNS to the TUN address must be answered before the private-range rule sends it direct.
	if b.Routing.Rules[0].RuleTag != "tun-dns" {
		t.Errorf("first rule %q", b.Routing.Rules[0].RuleTag)
	}
	found := false
	for _, o := range b.Outbounds {
		found = found || o.Tag == xrayconf.TagDNSOut && o.Protocol == "dns"
	}
	if !found {
		t.Error("dns outbound missing")
	}
}

func TestAutoMode(t *testing.T) {
	p := fixture(t)
	b, dest := build(t, p, xrayconf.ModeAuto)
	want := map[string]string{
		"dns":            "pick/foreign",
		"private":        "direct",
		"direct-domains": "direct",
		"direct-ips":     "direct",
		"rest":           xrayconf.TagSmart,
	}
	for tag, to := range want {
		if dest[tag] != to {
			t.Errorf("rule %s goes to %q, want %q", tag, dest[tag], to)
		}
	}
	if _, ok := dest["blocked-domains"]; ok {
		t.Error("auto mode checks sites instead of using the blocked lists")
	}
	// Every server is measured, so the picker can check the fastest first.
	if b.Observatory == nil || !slices.Equal(b.Observatory.SubjectSelector, []string{"foreign/", "ru/"}) {
		t.Errorf("observatory %+v", b.Observatory)
	}
	// Encrypted DNS first, the provider's when no server answers.
	if b.DNS == nil || len(b.DNS.Servers) != 3 || string(b.DNS.Servers[2]) != `"localhost"` {
		t.Errorf("dns %+v", b.DNS)
	}

	all, def := xrayconf.Candidates(p)
	if !slices.Equal(all, []string{"foreign/nl-1/reality", "foreign/nl-1/xhttp-cdn", "ru/msk-1/grpc"}) {
		t.Errorf("candidates %v", all)
	}
	if !slices.Equal(def, []string{"foreign/nl-1/reality", "foreign/nl-1/xhttp-cdn"}) {
		t.Errorf("default %v", def)
	}
}

func TestAutoModeOnlyRU(t *testing.T) {
	p := fixture(t)
	p.Exits = p.Exits[1:]
	b, dest := build(t, p, xrayconf.ModeAuto)
	if dest["dns"] != "ru/msk-1/grpc" || dest["rest"] != xrayconf.TagSmart || b.DNS == nil {
		t.Errorf("rules %v, dns %v", dest, b.DNS)
	}
	if b.Observatory != nil {
		t.Error("one server needs no observatory")
	}
	all, def := xrayconf.Candidates(p)
	if !slices.Equal(all, def) || len(all) != 1 {
		t.Errorf("candidates %v, default %v", all, def)
	}
}

func TestBasicMode(t *testing.T) {
	b, dest := build(t, fixture(t), xrayconf.ModeBasic)
	if dest["rest"] != "pick/foreign" || dest["direct-domains"] != "direct" || dest["direct-ips"] != "direct" {
		t.Errorf("rules %v", dest)
	}
	if _, ok := dest["blocked-domains"]; ok {
		t.Error("basic mode does not need blocked rules")
	}
	for _, r := range b.Routing.Rules {
		if r.RuleTag == "direct-domains" && (!slices.Contains(r.Domain, "domain:ru") || !slices.Contains(r.Domain, "domain:xn--p1ai") || !slices.Contains(r.Domain, "domain:msu.ru")) {
			t.Errorf("direct domains %v", r.Domain)
		}
	}
	var cfg struct {
		Routing struct {
			Rules []struct {
				RuleTag string   `json:"ruleTag"`
				IP      []string `json:"ip"`
			} `json:"rules"`
		} `json:"routing"`
		Stats  map[string]any `json:"stats"`
		Policy struct {
			System map[string]bool `json:"system"`
		} `json:"policy"`
	}
	raw, err := xrayconf.Build(fixture(t), xrayconf.Options{Mode: xrayconf.ModeBasic})
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	for _, r := range cfg.Routing.Rules {
		if r.RuleTag == "direct-ips" && !slices.Contains(r.IP, "geoip:ru") {
			t.Errorf("direct ips %v", r.IP)
		}
	}
	if cfg.Stats == nil || !cfg.Policy.System["statsOutboundUplink"] || !cfg.Policy.System["statsOutboundDownlink"] {
		t.Error("traffic counters are off")
	}
}

func TestBasicModeOnlyRU(t *testing.T) {
	p := fixture(t)
	p.Exits = p.Exits[1:]
	_, dest := build(t, p, xrayconf.ModeBasic)
	if dest["rest"] != "ru/msk-1/grpc" || dest["dns"] != "ru/msk-1/grpc" {
		t.Errorf("rules %v", dest)
	}
}

// AI services refuse Russian users: they go abroad in every mode, never
// through the per-site check, the RU exit or direct.
func TestAIServicesGoAbroad(t *testing.T) {
	for _, mode := range []xrayconf.Mode{xrayconf.ModeAuto, xrayconf.ModeMain, xrayconf.ModeBasic, xrayconf.ModeAll} {
		b, dest := build(t, fixture(t), mode)
		if dest["foreign-only"] != "pick/foreign" {
			t.Errorf("%s: AI services go to %q", mode, dest["foreign-only"])
		}
		for _, r := range b.Routing.Rules {
			if r.RuleTag == "foreign-only" && (!slices.Contains(r.Domain, "domain:chatgpt.com") || !slices.Contains(r.Domain, "domain:claude.ai")) {
				t.Errorf("%s: list %v", mode, r.Domain)
			}
			if r.RuleTag == "foreign-only" {
				break
			}
			if r.RuleTag == "direct-domains" {
				t.Errorf("%s: direct rules come before the AI services", mode)
			}
		}
	}
	p := fixture(t)
	p.Exits = p.Exits[1:]
	if _, dest := build(t, p, xrayconf.ModeAuto); dest["foreign-only"] != "" {
		t.Error("without a foreign exit there is nowhere to send them")
	}
	if _, dest := build(t, fixture(t), xrayconf.ModeOff); dest["foreign-only"] != "" {
		t.Error("off mode sends everything direct")
	}
}

func TestShieldAlwaysDirect(t *testing.T) {
	for _, m := range []xrayconf.Mode{xrayconf.ModeAuto, xrayconf.ModeBasic, xrayconf.ModeMain, xrayconf.ModeAll} {
		p := fixture(t)
		p.Routing.NoBuiltinDirect = true
		b, dest := build(t, p, m)
		if dest["shield"] != "direct" {
			t.Errorf("%s: shield goes to %q", m, dest["shield"])
		}
		var order []string
		for _, r := range b.Routing.Rules {
			order = append(order, r.RuleTag)
			if r.RuleTag == "shield" && (!slices.Contains(r.Domain, "domain:max.ru") || !slices.Contains(r.Domain, "domain:vk.com") ||
				!slices.Contains(r.Domain, "domain:yandex.ru") || !slices.Contains(r.Domain, "domain:gosuslugi.ru")) {
				t.Errorf("%s: shield domains %v", m, r.Domain)
			}
		}
		// Before anything that sends traffic to a server.
		if i := slices.Index(order, "shield"); i < 0 || i > slices.Index(order, "foreign-only") {
			t.Errorf("%s: rule order %v", m, order)
		}
	}
	_, dest := build(t, fixture(t), xrayconf.ModeOff)
	if _, ok := dest["shield"]; ok {
		t.Error("off mode needs no shield rule")
	}
}

func TestEvasionOutbounds(t *testing.T) {
	p := fixture(t)
	opt := xrayconf.Options{
		Mode:     xrayconf.ModeAuto,
		Inbounds: []xrayconf.Inbound{{Tag: "socks-in", Protocol: "socks", Listen: "127.0.0.1", Port: 10808}},
		Evasion:  true,
	}
	b, _ := buildOpt(t, p, opt)
	tags := map[string]bool{}
	for _, o := range b.Outbounds {
		tags[o.Tag] = true
	}
	variants := xrayconf.Variants(p)
	if len(variants) == 0 {
		t.Fatal("no transport gets a copy per preset")
	}
	for k := 1; k <= len(xrayconf.Presets); k++ {
		if !tags[xrayconf.DPITag(k)] {
			t.Errorf("preset %d outbound missing", k)
		}
		for _, v := range variants {
			vt := xrayconf.VariantTag(k, v)
			if !tags[vt] || xrayconf.BaseTag(vt) != v {
				t.Errorf("copy %s missing or does not name its transport", vt)
			}
		}
	}

	cfg, err := xrayconf.Build(p, opt)
	if err != nil {
		t.Fatal(err)
	}
	var raw struct {
		Outbounds []struct {
			Tag    string `json:"tag"`
			Stream struct {
				Sockopt struct {
					DialerProxy string `json:"dialerProxy"`
				} `json:"sockopt"`
			} `json:"streamSettings"`
		} `json:"outbounds"`
	}
	json.Unmarshal(cfg, &raw)
	for _, o := range raw.Outbounds {
		if want := xrayconf.DPITag(2); o.Tag == xrayconf.VariantTag(2, variants[0]) && o.Stream.Sockopt.DialerProxy != want {
			t.Errorf("%s dials through %q, want %q", o.Tag, o.Stream.Sockopt.DialerProxy, want)
		}
	}

	if _, dest := buildOpt(t, p, opt); dest["dns"] != xrayconf.TagSmart {
		t.Errorf("encrypted DNS goes to %q, want the picker", dest["dns"])
	}

	// Only the smart mode uses the presets.
	opt.Mode = xrayconf.ModeAll
	b, _ = buildOpt(t, p, opt)
	for _, o := range b.Outbounds {
		if xrayconf.BaseTag(o.Tag) != o.Tag || o.Tag == xrayconf.DPITag(1) {
			t.Errorf("all mode got %s", o.Tag)
		}
	}
}

// TestNoGeo builds a config that starts before geosite.dat and geoip.dat are
// downloaded: their lists are left out, the rest of the routing stays.
func TestNoGeo(t *testing.T) {
	t.Setenv("XRAY_LOCATION_ASSET", t.TempDir())
	for _, m := range []xrayconf.Mode{xrayconf.ModeAuto, xrayconf.ModeBasic, xrayconf.ModeMain} {
		cfg, err := xrayconf.Build(fixture(t), xrayconf.Options{Mode: m, NoGeo: true})
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(cfg, []byte("geoip:")) || bytes.Contains(cfg, []byte("geosite:")) {
			t.Errorf("mode %s still refers to the lists:\n%s", m, cfg)
		}
		if _, err := engine.Load(cfg); err != nil {
			t.Errorf("mode %s without the lists: %v", m, err)
		}
	}
	_, dest := buildOpt(t, fixture(t), xrayconf.Options{Mode: xrayconf.ModeBasic, NoGeo: true})
	if dest["direct-domains"] != "direct" || dest["rest"] != "pick/foreign" {
		t.Errorf("basic mode without the lists: %v", dest)
	}
}

// TestQUICNoise checks that Hysteria 2 sends noise first, ahead of its own
// obfuscation, in every mode, gets no copies through the presets and is
// known to go over UDP like TUIC.
func TestQUICNoise(t *testing.T) {
	p, _, err := profile.FromLinks("mix", []string{
		"vless://5783a3e7-e373-51cd-8642-c83782b807c5@203.0.113.1:443?security=none#tcp",
		"hysteria2://pw@203.0.113.1:8443/?sni=hy.example.org&obfs=salamander&obfs-password=ob#hy",
		"tuic://5783a3e7-e373-51cd-8642-c83782b807c5:pw@203.0.113.1:8444?sni=tuic.example.org&congestion_control=bbr#tuic",
	})
	if err != nil {
		t.Fatal(err)
	}
	udp := xrayconf.UDPTransports(p)
	if len(udp) != 2 {
		t.Fatalf("over UDP: %v, want Hysteria 2 and TUIC", udp)
	}
	for _, v := range xrayconf.Variants(p) {
		if slices.Contains(udp, v) {
			t.Errorf("%s over UDP got copies through the presets", v)
		}
	}

	masks := func(mode xrayconf.Mode) []string {
		opt := xrayconf.Options{
			Mode:     mode,
			Inbounds: []xrayconf.Inbound{{Tag: "socks-in", Protocol: "socks", Listen: "127.0.0.1", Port: 10808}},
			Evasion:  mode == xrayconf.ModeAuto,
		}
		buildOpt(t, p, opt) // real Xray takes it
		cfg, err := xrayconf.Build(p, opt)
		if err != nil {
			t.Fatal(err)
		}
		var raw struct {
			Outbounds []struct {
				Protocol string `json:"protocol"`
				Stream   struct {
					FinalMask struct {
						UDP []struct {
							Type string `json:"type"`
						} `json:"udp"`
					} `json:"finalmask"`
				} `json:"streamSettings"`
			} `json:"outbounds"`
		}
		json.Unmarshal(cfg, &raw)
		var types []string
		for _, o := range raw.Outbounds {
			if o.Protocol == "hysteria" {
				for _, m := range o.Stream.FinalMask.UDP {
					types = append(types, m.Type)
				}
			}
		}
		return types
	}
	if got := masks(xrayconf.ModeAuto); !slices.Equal(got, []string{"noise", "salamander"}) {
		t.Errorf("smart mode masks %v, want noise then salamander", got)
	}
	if got := masks(xrayconf.ModeAll); !slices.Equal(got, []string{"noise", "salamander"}) {
		t.Errorf("all mode masks %v, want noise then salamander", got)
	}
}

// TestCarried checks that the protocols Xray does not have are left out of
// its config but listed for their packages, and that only mieru over UDP
// counts as UDP.
func TestCarried(t *testing.T) {
	p, _, err := profile.FromLinks("mix", []string{
		"vless://5783a3e7-e373-51cd-8642-c83782b807c5@203.0.113.1:443?security=none#tcp",
		"tuic://5783a3e7-e373-51cd-8642-c83782b807c5:pw@203.0.113.1:8444?sni=tuic.example.org#tuic",
		"mierus://u:pw@203.0.113.1?port=6666&protocol=TCP#mieru-tcp",
		"mierus://u:pw@203.0.113.1?port=6667&protocol=UDP#mieru-udp",
		"naive+https://u:pw@naive.example.org#naive",
	})
	if err != nil {
		t.Fatal(err)
	}
	for protocol, n := range map[string]int{"tuic": 1, "mieru": 2, "naive": 1} {
		if got := xrayconf.Carried(p, protocol); len(got) != n {
			t.Errorf("%s: %d carried, want %d", protocol, len(got), n)
		}
	}
	b, _ := buildOpt(t, p, xrayconf.Options{Mode: xrayconf.ModeAll, Evasion: true})
	for _, o := range b.Outbounds {
		if o.Protocol == "tuic" || o.Protocol == "mieru" || o.Protocol == "naive" {
			t.Errorf("%s outbound %s left in the Xray config", o.Protocol, o.Tag)
		}
	}
	// The transports are s1 to s5 in the order of the links.
	if udp := xrayconf.UDPTransports(p); !slices.Equal(udp, []string{"foreign/sub/s2", "foreign/sub/s4"}) {
		t.Errorf("over UDP: %v, want TUIC and mieru over UDP", udp)
	}
	if v := xrayconf.Variants(p); !slices.Equal(v, []string{"foreign/sub/s1"}) {
		t.Errorf("copies through the presets for %v, want only VLESS", v)
	}
}
