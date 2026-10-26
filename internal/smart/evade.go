package smart

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/xtls/xray-core/core"

	"github.com/Stilltiger45/TigerSB-client/internal/xrayconf"
	"github.com/Stilltiger45/TigerSB-client/pkg/profile"
)

// Evasion is the DPI evasion of ModeAuto, in place of zapret, which needs
// root on Android: a site that opens through no server gets a second round,
// through the servers reached via a preset of xrayconf.Presets (fragmented
// handshake, UDP noise) and directly via the same preset. Which preset works
// depends on the network, so Tune picks it again every time the core starts,
// which the app does when the VPN is turned on and when the phone moves
// between Wi-Fi and mobile data.
type Evasion struct {
	// Preset is the preset to use until Tune has picked one (from 1); 0
	// means the first.
	Preset int
	// Variants are the candidates that have a copy per preset
	// (xrayconf.Variants).
	Variants []string
	// Sites are checked directly through each preset by Tune; ProbeURL is
	// checked through the servers. Empty means DPISites and
	// profile.DefaultProbeURL.
	Sites    []string
	ProbeURL string
}

// DPISites are sites that DPI in Russia blocks by name, not by address, so
// that fragmentation opens them directly; Tune checks the presets on them.
var DPISites = []string{"discord.com", "rutracker.org", "www.youtube.com"}

// NewEvasion returns the evasion for the profile, starting from preset (0 for
// the first).
func NewEvasion(p *profile.Profile, preset int) *Evasion {
	ev := &Evasion{Preset: preset, Variants: xrayconf.Variants(p)}
	if p.Probe != nil {
		ev.ProbeURL = p.Probe.URL
	}
	return ev
}

func (ev *Evasion) start() int {
	if ev.Preset < 1 || ev.Preset > len(xrayconf.Presets) {
		return 1
	}
	return ev.Preset
}

// evasive returns what the second round checks a site through: the
// candidates' copies through the preset in use, then the preset directly.
func (h *Handler) evasive(cands []string) []string {
	k := int(h.preset.Load())
	if k == 0 {
		return nil
	}
	var out []string
	for _, t := range cands {
		if h.variants[t] {
			out = append(out, xrayconf.VariantTag(k, t))
		}
	}
	return append(out, xrayconf.DPITag(k))
}

// TuneResult is what Tune found.
type TuneResult struct {
	// Preset is the preset now in use, from 1.
	Preset int
	// Opened is how many of Checks the preset opened; Plain is how many
	// opened without any preset.
	Opened, Plain, Checks int
	// Changed tells whether the preset differs from the one used before.
	Changed bool
}

func (r TuneResult) String() string {
	return fmt.Sprintf("набор %d (%s): открылось %d из %d, без обхода %d",
		r.Preset, xrayconf.Presets[r.Preset-1].Name, r.Opened, r.Checks, r.Plain)
}

// maxTuneServers caps the servers Tune checks through each preset.
const maxTuneServers = 3

// Tune checks every preset on this network at once, through the first
// servers and directly on the DPI sites, and switches to the one that opens
// the most when it opens more than the preset in use. It returns an error when the instance runs
// no picker with evasion.
func Tune(inst *core.Instance, timeout time.Duration) (TuneResult, error) {
	h := handlerOf(inst)
	if h == nil || h.opt.Evasion == nil {
		return TuneResult{}, fmt.Errorf("умная маршрутизация без обхода DPI")
	}
	ev := h.opt.Evasion
	probe := ev.ProbeURL
	if probe == "" {
		probe = profile.DefaultProbeURL
	}
	u, err := url.Parse(probe)
	if err != nil {
		return TuneResult{}, err
	}
	sites := ev.Sites
	if len(sites) == 0 {
		sites = DPISites
	}
	var servers []string
	for _, t := range h.opt.Candidates {
		if h.variants[t] && len(servers) < maxTuneServers {
			servers = append(servers, t)
		}
	}

	// A check is an outbound and the site checked through it; set 0 holds
	// the checks without any preset.
	type check struct {
		set       int
		tag, host string
	}
	var checks []check
	for k := 0; k <= len(xrayconf.Presets); k++ {
		for _, s := range servers {
			tag := s
			if k > 0 {
				tag = xrayconf.VariantTag(k, s)
			}
			checks = append(checks, check{k, tag, u.Host})
		}
		for _, site := range sites {
			tag := h.opt.Direct
			if k > 0 {
				tag = xrayconf.DPITag(k)
			}
			checks = append(checks, check{k, tag, site})
		}
	}

	ctx := context.WithValue(context.Background(), core.XrayKey(1), inst)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	opened := make([]int, len(xrayconf.Presets)+1)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, c := range checks {
		wg.Go(func() {
			if h.probe(ctx, c.tag, u.Scheme, c.host) == failed {
				return
			}
			mu.Lock()
			opened[c.set]++
			mu.Unlock()
		})
	}
	wg.Wait()

	per := len(checks) / (len(xrayconf.Presets) + 1)
	old := int(h.preset.Load())
	best := old
	for k := 1; k <= len(xrayconf.Presets); k++ {
		// Only a preset that opens more takes over: presets that open as
		// much differ in speed by chance, and switching drops the picks.
		if opened[k] > opened[best] {
			best = k
		}
	}
	if best != old {
		h.preset.Store(int32(best))
		h.forgetEvasive()
	}
	return TuneResult{Preset: best, Opened: opened[best], Plain: opened[0], Checks: per, Changed: best != old}, nil
}

// forgetEvasive drops the sites picked through a preset, so they are checked
// again through the new one.
func (h *Handler) forgetEvasive() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for k, e := range h.cache {
		if strings.HasPrefix(e.tag, "dpi/") {
			delete(h.cache, k)
		}
	}
}
