// Package steady keeps one transport of an exit in use: the fastest live one,
// chosen without flapping. Xray's leastPing balancer jumps to whichever
// transport answered fastest in the last single probe and keeps a dead one
// until the next probe, up to a minute later; here delays are smoothed, a
// transport is left only for a clearly faster one, and the one in use is
// checked more often so a dead server is left within seconds.
//
// The picker is an Xray outbound handler with the tag xrayconf.PickTag(role).
package steady

import (
	"context"
	stderrors "errors"
	"slices"
	"sync"
	"time"

	"github.com/xtls/xray-core/app/observatory"
	"github.com/xtls/xray-core/common"
	"github.com/xtls/xray-core/common/errors"
	"github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/common/session"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/features/extension"
	"github.com/xtls/xray-core/features/outbound"
	"github.com/xtls/xray-core/transport"

	"github.com/Stilltiger45/TigerSB-client/internal/meter"
	"github.com/Stilltiger45/TigerSB-client/internal/xrayconf"
	"github.com/Stilltiger45/TigerSB-client/pkg/profile"
)

// Options tune a picker. Zero fields take the defaults.
type Options struct {
	// Tag is the picker's outbound tag.
	Tag string
	// Tags are the transports, best priority first. The first carries
	// traffic until the observatory has measured them.
	Tags []string
	// ProbeURL is what the check of the transport in use requests; empty
	// means profile.DefaultProbeURL.
	ProbeURL string
	// Check is how often the transport in use is checked (20 s); Down is
	// how long a transport that failed two checks in a row is avoided (2 min).
	Check, Down time.Duration
}

// Hooks returns the engine.Start hooks that add a picker for every role with
// several transports.
func Hooks(p *profile.Profile) []func(*core.Instance) error {
	probe := ""
	if p.Probe != nil {
		probe = p.Probe.URL
	}
	var hooks []func(*core.Instance) error
	for _, role := range []profile.Role{profile.RoleForeign, profile.RoleRU} {
		if tags := xrayconf.Transports(p, role); len(tags) > 1 {
			opt := Options{Tag: xrayconf.PickTag(role), Tags: tags, ProbeURL: probe}
			hooks = append(hooks, func(inst *core.Instance) error { return Attach(inst, opt) })
		}
	}
	return hooks
}

// Attach adds a picker to a created, not yet started Xray instance.
func Attach(inst *core.Instance, opt Options) error {
	if opt.Check == 0 {
		opt.Check = 20 * time.Second
	}
	if opt.Down == 0 {
		opt.Down = 2 * time.Minute
	}
	ohm, _ := inst.GetFeature(outbound.ManagerType()).(outbound.Manager)
	if ohm == nil {
		return stderrors.New("в ядре Xray нет менеджера исходящих")
	}
	obs, _ := inst.GetFeature(extension.ObservatoryType()).(extension.Observatory)
	h := &Handler{
		opt:  opt,
		inst: inst,
		ohm:  ohm,
		obs:  obs,
		seen: map[string]int64{},
		rtt:  map[string]float64{},
		down: map[string]time.Time{},
		stop: make(chan struct{}),
	}
	return ohm.AddHandler(context.Background(), h)
}

// Current returns the transport the picker with the tag uses now, or "" when
// the instance has no such picker.
func Current(inst *core.Instance, tag string) string {
	ohm, _ := inst.GetFeature(outbound.ManagerType()).(outbound.Manager)
	if ohm == nil {
		return ""
	}
	if h, ok := ohm.GetHandler(tag).(*Handler); ok {
		return h.Current()
	}
	return ""
}

// Handler is the picker. It implements Xray's outbound.Handler.
type Handler struct {
	opt  Options
	inst *core.Instance
	ohm  outbound.Manager
	obs  extension.Observatory

	mu       sync.Mutex
	cur      string
	measured bool                 // cur was chosen from measurements
	seen     map[string]int64     // the probe time last folded into rtt
	alive    map[string]bool      // the last probe's verdict
	rtt      map[string]float64   // smoothed delay, ms
	down     map[string]time.Time // failed our own checks until then
	fails    int                  // checks of cur failed in a row

	stop     chan struct{}
	stopOnce sync.Once
}

func (h *Handler) Tag() string                          { return h.opt.Tag }
func (h *Handler) SenderSettings() *serial.TypedMessage { return nil }
func (h *Handler) ProxySettings() *serial.TypedMessage  { return nil }

func (h *Handler) Start() error {
	go h.watch()
	return nil
}

func (h *Handler) Close() error {
	h.stopOnce.Do(func() { close(h.stop) })
	return nil
}

// Dispatch hands the connection to the transport in use.
func (h *Handler) Dispatch(ctx context.Context, link *transport.Link) {
	tag := h.Current()
	handler := h.ohm.GetHandler(tag)
	if handler == nil {
		errors.LogWarning(ctx, "steady: no outbound ", tag)
		common.Close(link.Writer)
		common.Interrupt(link.Reader)
		return
	}
	obs := session.OutboundsFromContext(ctx)
	obs[len(obs)-1].Tag = tag
	handler.Dispatch(ctx, link)
}

// Current returns the transport in use, switching first if the measurements
// call for it.
func (h *Handler) Current() string {
	st := h.observe()
	h.mu.Lock()
	defer h.mu.Unlock()
	h.fold(st)
	next, measured := choose(h.cur, h.measured, h.opt.Tags, h.views(time.Now()))
	if next != h.cur && h.cur != "" {
		errors.LogInfo(context.Background(), "steady: ", h.opt.Tag, " ", h.cur, " -> ", next)
	}
	if next != h.cur {
		h.fails = 0
	}
	h.cur, h.measured = next, measured
	return h.cur
}

func (h *Handler) observe() []*observatory.OutboundStatus {
	if h.obs == nil {
		return nil
	}
	m, err := h.obs.GetObservation(context.Background())
	if err != nil {
		return nil
	}
	res, _ := m.(*observatory.ObservationResult)
	if res == nil {
		return nil
	}
	return res.Status
}

// smoothing is the weight of a new delay in the running average.
const smoothing = 0.4

// fold adds the observatory's new probes to the smoothed delays.
func (h *Handler) fold(st []*observatory.OutboundStatus) {
	if h.alive == nil {
		h.alive = map[string]bool{}
	}
	for _, s := range st {
		if !slices.Contains(h.opt.Tags, s.OutboundTag) || h.seen[s.OutboundTag] == s.LastTryTime {
			continue
		}
		h.seen[s.OutboundTag] = s.LastTryTime
		h.alive[s.OutboundTag] = s.Alive
		if !s.Alive {
			continue
		}
		if old, ok := h.rtt[s.OutboundTag]; ok {
			h.rtt[s.OutboundTag] = old + smoothing*(float64(s.Delay)-old)
		} else {
			h.rtt[s.OutboundTag] = float64(s.Delay)
		}
	}
}

func (h *Handler) views(now time.Time) map[string]view {
	v := make(map[string]view, len(h.opt.Tags))
	for _, t := range h.opt.Tags {
		alive, known := h.alive[t]
		if until, ok := h.down[t]; ok && now.Before(until) {
			alive, known = false, true
		}
		v[t] = view{known: known, alive: alive, rtt: h.rtt[t]}
	}
	return v
}

// watch checks the transport in use every Check; after two failures in a row
// it is avoided for Down and the next best takes over.
func (h *Handler) watch() {
	t := time.NewTicker(h.opt.Check)
	defer t.Stop()
	for {
		select {
		case <-h.stop:
			return
		case <-t.C:
		}
		cur := h.Current()
		ok := meter.Ping(h.inst, cur, h.opt.ProbeURL, 6*time.Second) >= 0
		h.mu.Lock()
		if cur == h.cur {
			if ok {
				h.fails = 0
			} else if h.fails++; h.fails >= 2 {
				errors.LogWarning(context.Background(), "steady: ", cur, " does not answer, leaving it")
				h.down[cur] = time.Now().Add(h.opt.Down)
				h.fails = 0
			}
		}
		h.mu.Unlock()
	}
}

// view is what the picker knows about a transport.
type view struct {
	known bool    // probed at least once
	alive bool    // the last probe answered
	rtt   float64 // smoothed delay, ms
}

// A transport takes over from a live one only when it is this much faster,
// both in share and in milliseconds.
const (
	switchShare = 0.7
	switchGap   = 40.0
)

// choose returns the transport to use given the one in use (cur) and whether
// every transport had been measured when cur was chosen. Before any
// measurement it keeps the best priority transport; until all are measured it
// follows the fastest live one; from then on it leaves cur only when cur is
// dead or another live transport is clearly faster. With nothing alive it
// keeps cur.
func choose(cur string, measured bool, tags []string, v map[string]view) (string, bool) {
	if len(tags) == 0 {
		return "", false
	}
	best := ""
	for _, t := range tags {
		if s := v[t]; s.alive && (best == "" || s.rtt < v[best].rtt) {
			best = t
		}
	}
	if cur == "" {
		cur = tags[0]
	}
	if best == "" {
		return cur, measured
	}
	if !measured {
		// Until every transport has answered or failed once, follow the
		// fastest so far: the first probes all start together.
		all := true
		for _, t := range tags {
			all = all && v[t].known
		}
		return best, all
	}
	c := v[cur]
	switch {
	case !c.alive:
		return best, true
	case v[best].rtt < c.rtt*switchShare && c.rtt-v[best].rtt > switchGap:
		return best, true
	}
	return cur, true
}
