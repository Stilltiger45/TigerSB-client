package steady

import (
	"fmt"
	"sync"
	"time"

	"github.com/xtls/xray-core/core"

	"github.com/Stilltiger45/TigerSB-client/internal/meter"
	"github.com/Stilltiger45/TigerSB-client/internal/xrayconf"
)

// The smart mode's DPI evasion, in place of zapret, which needs root on
// Android: the Xray transports over TCP can reach their servers through a
// preset of xrayconf.Presets (a fragmented start of the connection, noise).
// Which one works, if any is needed, depends on the network, so Tune checks
// them every time the core starts, which the app does when the VPN is turned
// on, and when the phone moves between Wi-Fi and mobile data.

// TuneResult is what Tune found.
type TuneResult struct {
	// Preset is the preset now in use, from 1, or 0 for none.
	Preset int
	// Opened is how many of Checks opened through it; Plain is how many
	// opened without any preset.
	Opened, Plain, Checks int
	// Changed tells whether the preset differs from the one used before.
	Changed bool
}

func (r TuneResult) String() string {
	if r.Preset == 0 {
		return fmt.Sprintf("не нужен: серверы открываются напрямую, %d из %d", r.Opened, r.Checks)
	}
	return fmt.Sprintf("набор %d (%s): открылось %d из %d, без обхода %d",
		r.Preset, xrayconf.Presets[r.Preset-1].Name, r.Opened, r.Checks, r.Plain)
}

// maxTuneServers caps the servers Tune checks through each preset.
const maxTuneServers = 3

// Tune checks the first servers plainly and through every preset at once
// and has the pickers use the preset that opens the most, the mildest when
// several open as many, and none when the plain way opens as many: a preset
// costs a little speed. When nothing opens at all the preset in use stays.
// It returns an error when the instance has no picker with preset copies.
func Tune(inst *core.Instance, timeout time.Duration) (TuneResult, error) {
	var hs []*Handler
	var servers []string
	probe := map[string]string{}
	for _, h := range pickers(inst) {
		if len(h.opt.Variants) == 0 {
			continue
		}
		hs = append(hs, h)
		for _, t := range h.opt.Variants {
			if len(servers) < maxTuneServers {
				servers = append(servers, t)
				probe[t] = h.opt.ProbeURL
			}
		}
	}
	if len(hs) == 0 {
		return TuneResult{}, fmt.Errorf("обход DPI не нужен: нет серверов Xray поверх TCP")
	}

	opened := make([]int, len(xrayconf.Presets)+1)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for k := range opened {
		for _, s := range servers {
			tag := s
			if k > 0 {
				tag = xrayconf.VariantTag(k, s)
			}
			wg.Go(func() {
				if meter.Check(inst, tag, meter.CheckURLs(s, probe[s]), timeout) < 0 {
					return
				}
				mu.Lock()
				opened[k]++
				mu.Unlock()
			})
		}
	}
	wg.Wait()

	old := int(hs[0].preset.Load())
	best := 0
	for k := 1; k < len(opened); k++ {
		if opened[k] > opened[best] {
			best = k
		}
	}
	if opened[best] == 0 {
		best = old
	}
	if best != old {
		setPreset(hs, best)
	}
	return TuneResult{Preset: best, Opened: opened[best], Plain: opened[0], Checks: len(servers), Changed: best != old}, nil
}

// SetPreset has the pickers of the instance use preset k (0 for none), as
// the one that last worked on the network the phone moved to, until Tune
// checks again. Out of range values are ignored.
func SetPreset(inst *core.Instance, k int) {
	if k < 0 || k > len(xrayconf.Presets) {
		return
	}
	var hs []*Handler
	for _, h := range pickers(inst) {
		if len(h.opt.Variants) > 0 && int(h.preset.Load()) != k {
			hs = append(hs, h)
		}
	}
	setPreset(hs, k)
}

// setPreset switches the pickers to preset k and measures their transports
// again, now through it.
func setPreset(hs []*Handler, k int) {
	for _, h := range hs {
		h.preset.Store(int32(k))
		h.renew()
	}
}
