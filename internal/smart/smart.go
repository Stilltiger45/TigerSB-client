// Package smart picks a server for each site. The first time a site is opened
// it is checked through every VPN server at once and the fastest that works
// carries the site from then on; when none of them works the site goes direct.
//
// The picker is an Xray outbound handler with the tag Tag: routing rules send
// traffic to it like to any other outbound, and it hands each connection on to
// the outbound it picked.
package smart

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	stderrors "errors"
	gonet "net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xtls/xray-core/app/observatory"
	"github.com/xtls/xray-core/common"
	"github.com/xtls/xray-core/common/errors"
	"github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/common/session"
	"github.com/xtls/xray-core/common/utils"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/features/extension"
	"github.com/xtls/xray-core/features/outbound"
	"github.com/xtls/xray-core/features/routing"
	"github.com/xtls/xray-core/transport"
	"github.com/xtls/xray-core/transport/internet/tagged"

	"github.com/Stilltiger45/TigerSB-client/internal/xrayconf"
	"github.com/Stilltiger45/TigerSB-client/pkg/profile"
)

// Tag is the outbound tag routing rules use to send traffic to the picker.
const Tag = xrayconf.TagSmart

// Options tune the picker. Zero fields take the defaults below.
type Options struct {
	// Candidates are the VPN outbounds a site is checked through, best
	// priority first.
	Candidates []string
	// Default are the outbounds for traffic without a domain, which cannot be
	// checked; the fastest live one carries it. Empty means Candidates.
	Default []string
	// Direct is the outbound for sites no candidate opens.
	Direct string

	// MaxProbes caps how many candidates a site is checked through.
	MaxProbes int
	// Timeout bounds one check.
	Timeout time.Duration
	// Grace is how long to wait for a clean answer after a candidate opened
	// the site but got a refusal (403 or 451), which is what sites that turn
	// away foreign visitors answer.
	Grace time.Duration
	// TTL is how long a pick is kept; FailTTL is how long "direct, nothing
	// worked" is kept, shorter so a passing outage is not remembered.
	TTL, FailTTL time.Duration

	// RootCAs verifies the sites' certificates; nil means the system roots.
	RootCAs *x509.CertPool

	// Evasion, when set, gives sites that open through no server a second
	// round through the DPI presets (see evade.go); nil turns that off.
	Evasion *Evasion
}

func (o *Options) defaults() {
	if o.Direct == "" {
		o.Direct = "direct"
	}
	if len(o.Default) == 0 {
		o.Default = o.Candidates
	}
	if o.MaxProbes == 0 {
		o.MaxProbes = 8
	}
	if o.Timeout == 0 {
		o.Timeout = 4 * time.Second
	}
	if o.Grace == 0 {
		o.Grace = time.Second
	}
	if o.TTL == 0 {
		o.TTL = 30 * time.Minute
	}
	if o.FailTTL == 0 {
		o.FailTTL = 2 * time.Minute
	}
}

// maxCache bounds the number of remembered sites; expired ones are dropped
// when it is reached.
const maxCache = 4096

// Setup returns a hook for engine.Start that adds the picker to Xray.
func Setup(opt Options) func(*core.Instance) error {
	return func(inst *core.Instance) error { return Attach(inst, opt) }
}

// Hooks returns the engine.Start hooks the mode needs: the picker for
// xrayconf.ModeAuto, none for the other modes. ev is the DPI evasion the
// config was built with (xrayconf.Options.Evasion), nil for none.
func Hooks(p *profile.Profile, m xrayconf.Mode, ev *Evasion) []func(*core.Instance) error {
	if m != xrayconf.ModeAuto {
		return nil
	}
	all, def := xrayconf.Candidates(p)
	return []func(*core.Instance) error{Setup(Options{Candidates: all, Default: def, Direct: xrayconf.TagDirect, Evasion: ev})}
}

// Attach adds the picker to a created, not yet started Xray instance.
func Attach(inst *core.Instance, opt Options) error {
	opt.defaults()
	ohm, _ := inst.GetFeature(outbound.ManagerType()).(outbound.Manager)
	dispatcher, _ := inst.GetFeature(routing.DispatcherType()).(routing.Dispatcher)
	if ohm == nil || dispatcher == nil {
		return stderrors.New("в ядре Xray нет менеджера исходящих или диспетчера")
	}
	obs, _ := inst.GetFeature(extension.ObservatoryType()).(extension.Observatory)
	h := &Handler{
		opt:        opt,
		ohm:        ohm,
		dispatcher: dispatcher,
		obs:        obs,
		cache:      map[string]entry{},
		inflight:   map[string]*call{},
	}
	if ev := opt.Evasion; ev != nil {
		h.preset.Store(int32(ev.start()))
		h.variants = map[string]bool{}
		for _, t := range ev.Variants {
			h.variants[t] = true
		}
	}
	return ohm.AddHandler(context.Background(), h)
}

// Handler is the picker. It implements Xray's outbound.Handler.
type Handler struct {
	opt        Options
	ohm        outbound.Manager
	dispatcher routing.Dispatcher
	obs        extension.Observatory

	mu       sync.Mutex
	cache    map[string]entry
	inflight map[string]*call

	// The DPI preset in use (0 without evasion) and the candidates that have
	// a copy per preset.
	preset   atomic.Int32
	variants map[string]bool
}

type entry struct {
	tag   string
	until time.Time
}

type call struct {
	done chan struct{}
	tag  string
}

func (h *Handler) Tag() string                          { return Tag }
func (h *Handler) Start() error                         { return nil }
func (h *Handler) Close() error                         { return nil }
func (h *Handler) SenderSettings() *serial.TypedMessage { return nil }
func (h *Handler) ProxySettings() *serial.TypedMessage  { return nil }

// Dispatch hands the connection to the outbound picked for its site.
func (h *Handler) Dispatch(ctx context.Context, link *transport.Link) {
	obs := session.OutboundsFromContext(ctx)
	ob := obs[len(obs)-1]
	host := hostOf(ob)

	tag := h.opt.Direct
	if host == "" {
		tag = h.fallback()
	} else {
		scheme := "https"
		if ob.Target.Port == 80 {
			scheme = "http"
		}
		tag = h.pick(core.ToBackgroundDetachedContext(ctx), scheme, host)
	}

	handler := h.ohm.GetHandler(tag)
	if handler == nil {
		tag, handler = h.opt.Direct, h.ohm.GetHandler(h.opt.Direct)
	}
	if handler == nil {
		errors.LogWarning(ctx, "smart: no outbound ", tag)
		common.Close(link.Writer)
		common.Interrupt(link.Reader)
		return
	}
	if tag != h.opt.Direct && host != "" && ob.Target.Network == net.Network_TCP {
		// Let the server resolve the name, as it did during the check: the
		// site then answers from its node nearest to the server.
		ob.Target = net.TCPDestination(net.DomainAddress(host), ob.Target.Port)
	}
	ob.Tag = tag
	errors.LogInfo(ctx, "smart: ", host, " -> ", tag)
	handler.Dispatch(ctx, link)
}

// hostOf returns the site's name: the one sniffed from the traffic, or the one
// the app asked the proxy for.
func hostOf(ob *session.Outbound) string {
	for _, d := range []net.Destination{ob.RouteTarget, ob.Target} {
		if d.IsValid() && d.Address.Family().IsDomain() {
			return strings.TrimSuffix(strings.ToLower(d.Address.Domain()), ".")
		}
	}
	return ""
}

// pick returns the remembered outbound for the site, or checks the site and
// remembers the result. Connections to a site being checked wait for that check.
func (h *Handler) pick(ctx context.Context, scheme, host string) string {
	key := scheme + "://" + host
	now := time.Now()
	h.mu.Lock()
	if e, ok := h.cache[key]; ok && now.Before(e.until) && !h.dead(e.tag) {
		h.mu.Unlock()
		return e.tag
	}
	if c, ok := h.inflight[key]; ok {
		h.mu.Unlock()
		<-c.done
		return c.tag
	}
	c := &call{done: make(chan struct{})}
	h.inflight[key] = c
	h.mu.Unlock()

	tag, ok := h.probeAll(ctx, scheme, host)
	ttl := h.opt.TTL
	if !ok {
		ttl = h.opt.FailTTL
	}

	h.mu.Lock()
	if len(h.cache) >= maxCache {
		for k, e := range h.cache {
			if now.After(e.until) {
				delete(h.cache, k)
			}
		}
		if len(h.cache) >= maxCache {
			clear(h.cache)
		}
	}
	h.cache[key] = entry{tag: tag, until: time.Now().Add(ttl)}
	delete(h.inflight, key)
	c.tag = tag
	h.mu.Unlock()
	close(c.done)
	return tag
}

type verdict int

const (
	failed  verdict = iota
	refused         // the site answered 403 or 451
	opened
)

type result struct {
	tag string
	v   verdict
}

// probeAll checks the site through the candidates at once. The first clean
// answer wins, being the fastest. A refusal only wins when no candidate and
// not the direct route answers cleanly. ok is false when nothing worked and
// the site goes direct unchecked.
func (h *Handler) probeAll(ctx context.Context, scheme, host string) (tag string, ok bool) {
	cands := h.ordered()
	if len(cands) == 0 {
		return h.opt.Direct, false
	}
	var refusedBy string
	// Servers that all failed their last check are not worth the wait when
	// the preset can still reach them.
	if h.opt.Evasion == nil || !h.allDead(cands) {
		tag, refusedBy = h.race(ctx, cands, scheme, host)
		if tag != "" {
			return tag, true
		}
	}
	if refusedBy == "" {
		// No server answered: maybe DPI stops the servers or the site itself,
		// so try once more through the preset that works on this network.
		if tag, refusedBy = h.race(ctx, h.evasive(cands), scheme, host); tag != "" {
			return tag, true
		}
		if refusedBy != "" {
			return refusedBy, true
		}
		return h.opt.Direct, false
	}
	// Servers abroad got turned away: maybe the site only lets in visitors
	// from home, so try it without the VPN.
	if h.probe(ctx, h.opt.Direct, scheme, host) == opened {
		return h.opt.Direct, true
	}
	return refusedBy, true
}

// race checks the site through the outbounds at once. tag is the first that
// opened it; when none did, refusedBy is the first that got a refusal, if any
// (waiting Grace after it for a clean answer).
func (h *Handler) race(ctx context.Context, tags []string, scheme, host string) (tag, refusedBy string) {
	results := make(chan result, len(tags))
	for _, t := range tags {
		go func() { results <- result{t, h.probe(ctx, t, scheme, host)} }()
	}
	var grace <-chan time.Time
	for range tags {
		select {
		case r := <-results:
			switch r.v {
			case opened:
				return r.tag, ""
			case refused:
				if refusedBy == "" {
					refusedBy = r.tag
					grace = time.After(h.opt.Grace)
				}
			}
		case <-grace:
			return "", refusedBy
		}
	}
	return "", refusedBy
}

// probe requests the site's front page through one outbound.
func (h *Handler) probe(ctx context.Context, tag, scheme, host string) verdict {
	ctx, cancel := context.WithTimeout(ctx, h.opt.Timeout)
	defer cancel()
	tr := &http.Transport{
		Proxy: func(*http.Request) (*url.URL, error) { return nil, nil },
		DialContext: func(ctx context.Context, network, addr string) (gonet.Conn, error) {
			dest, err := net.ParseDestination(network + ":" + addr)
			if err != nil {
				return nil, err
			}
			return tagged.Dialer(ctx, h.dispatcher, dest, tag)
		},
		TLSClientConfig:   &tls.Config{RootCAs: h.opt.RootCAs},
		DisableKeepAlives: true,
	}
	defer tr.CloseIdleConnections()
	client := &http.Client{
		Transport:     tr,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, scheme+"://"+host+"/", nil)
	if err != nil {
		return failed
	}
	utils.TryDefaultHeadersWith(req.Header, "nav")
	resp, err := client.Do(req)
	if err != nil {
		errors.LogDebug(ctx, "smart: ", host, " via ", tag, ": ", err)
		return failed
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnavailableForLegalReasons {
		return refused
	}
	return opened
}

// ordered returns the candidates to check, live ones first by speed, then
// unmeasured ones in priority order, then dead ones, at most MaxProbes.
func (h *Handler) ordered() []string {
	st := h.status()
	rank := func(t string) (int, int64) {
		s, ok := st[t]
		switch {
		case !ok:
			return 1, 0
		case s.Alive:
			return 0, s.Delay
		default:
			return 2, 0
		}
	}
	out := slices.Clone(h.opt.Candidates)
	slices.SortStableFunc(out, func(a, b string) int {
		ra, da := rank(a)
		rb, db := rank(b)
		if ra != rb {
			return ra - rb
		}
		return int(da - db)
	})
	if len(out) > h.opt.MaxProbes {
		out = out[:h.opt.MaxProbes]
	}
	return out
}

// fallback is the outbound for traffic without a domain: the fastest live
// default outbound, or the first one before anything is measured.
func (h *Handler) fallback() string {
	if len(h.opt.Default) == 0 {
		return h.opt.Direct
	}
	st := h.status()
	best, delay := h.opt.Default[0], int64(-1)
	for _, t := range h.opt.Default {
		if s, ok := st[t]; ok && s.Alive && (delay < 0 || s.Delay < delay) {
			best, delay = t, s.Delay
		}
	}
	if delay < 0 && h.allDead(h.opt.Default) {
		// Every server failed its last check: reach one through the preset.
		if k := int(h.preset.Load()); k > 0 {
			for _, t := range h.opt.Default {
				if h.variants[t] {
					return xrayconf.VariantTag(k, t)
				}
			}
		}
	}
	return best
}

// InUse returns the outbound that carries traffic without a domain now, which
// is also the server most sites go through: what the app shows and checks.
func InUse(inst *core.Instance) string {
	if h := handlerOf(inst); h != nil {
		return h.fallback()
	}
	return ""
}

func handlerOf(inst *core.Instance) *Handler {
	ohm, _ := inst.GetFeature(outbound.ManagerType()).(outbound.Manager)
	if ohm == nil {
		return nil
	}
	h, _ := ohm.GetHandler(Tag).(*Handler)
	return h
}

// allDead reports whether the observatory has checked every one of tags and
// seen each fail lately.
func (h *Handler) allDead(tags []string) bool {
	st := h.status()
	for _, t := range tags {
		if s, ok := st[t]; !ok || s.Alive {
			return false
		}
	}
	return len(tags) > 0
}

// dead reports whether the observatory saw the outbound fail lately.
func (h *Handler) dead(tag string) bool {
	if tag == h.opt.Direct {
		return false
	}
	s, ok := h.status()[tag]
	return ok && !s.Alive
}

func (h *Handler) status() map[string]*observatory.OutboundStatus {
	if h.obs == nil {
		return nil
	}
	m, err := h.obs.GetObservation(context.Background())
	if err != nil {
		return nil
	}
	res, ok := m.(*observatory.ObservationResult)
	if !ok {
		return nil
	}
	st := make(map[string]*observatory.OutboundStatus, len(res.Status))
	for _, s := range res.Status {
		st[s.OutboundTag] = s
	}
	return st
}
