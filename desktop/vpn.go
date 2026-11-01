package desktop

import (
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Stilltiger45/TigerSB-client/internal/bypass"
)

// The watchdog and the network follow the Android app's VPN service: see
// TigerVpnService.kt for why each is as it is.
var (
	// The watchdog checks the channel every watchEvery, and recheckAfter
	// after a failed check. After failsToReset failed checks in a row it
	// reopens the connections to the servers, after failsToRestart it
	// restarts the core, at most once per restartGap. While the computer's
	// network itself has no internet it only looks every offlineRecheck
	// for its return.
	watchEvery     = 20 * time.Second
	recheckAfter   = 3 * time.Second
	offlineRecheck = 2 * time.Second
	failsToReset   = 2
	failsToRestart = 4
	restartGap     = 2 * time.Minute

	// After a change of network the VPN moves to it once the network has
	// been stable for netSettle, and at most once per netSwitchGap.
	netSettle    = time.Second
	netSwitchGap = 5 * time.Second
)

// Phases of the VPN, as the app names them.
const (
	Disconnected = "disconnected"
	Connecting   = "connecting"
	Connected    = "connected"
	Failed       = "error"
)

// Status is what the VPN is doing now. Error explains a failure, or, when
// connected, what the user can do about connections that freeze.
type Status struct {
	Phase string `json:"phase"`
	Error string `json:"error,omitempty"`
}

// VPN keeps the VPN up: it connects, follows the network and watches the
// channel. Everything it changes happens on one worker goroutine, in order.
type VPN struct {
	eng  Engine
	plat Platform
	dir  string

	queue chan func()
	quit  chan struct{}

	// The worker's own.
	saved         savedState
	up            bool // the core and the TUN run
	waitNet       bool // the user wants the VPN on, but there is no network yet
	uplink        Uplink
	hasUplink     bool
	mtu           int
	epoch         int
	fails         int
	offline       bool
	checking      bool
	lastRestart   time.Time
	lastNetSwitch time.Time
	watchTimer    *time.Timer
	netTimer      *time.Timer
	stopWatch     func()

	stMu      sync.Mutex
	status    Status
	listeners map[int]func(Status)
	nextID    int
}

// savedState is what the VPN keeps across restarts of the service, in
// desktop.json.
type savedState struct {
	// Mode is the routing mode: "auto", "basic" or "all".
	Mode string `json:"mode"`
	// On is whether the user left the VPN on: after a reboot the app turns
	// it on again when it starts with the system.
	On bool `json:"on"`
}

const stateFile = "desktop.json"

// NewVPN makes the VPN; its state lives in dir. Close ends it.
func NewVPN(eng Engine, plat Platform, dir string) *VPN {
	v := &VPN{
		eng: eng, plat: plat, dir: dir,
		queue:     make(chan func(), 64),
		quit:      make(chan struct{}),
		status:    Status{Phase: Disconnected},
		listeners: map[int]func(Status){},
		saved:     savedState{Mode: "auto"},
	}
	if b, err := os.ReadFile(filepath.Join(dir, stateFile)); err == nil {
		json.Unmarshal(b, &v.saved)
	}
	if v.saved.Mode == "" {
		v.saved.Mode = "auto"
	}
	go v.work()
	return v
}

func (v *VPN) work() {
	for {
		select {
		case f := <-v.queue:
			f()
		case <-v.quit:
			return
		}
	}
}

// do runs f on the worker and waits for it.
func (v *VPN) do(f func()) {
	done := make(chan struct{})
	select {
	case v.queue <- func() { f(); close(done) }:
		<-done
	case <-v.quit:
	}
}

// post runs f on the worker later.
func (v *VPN) post(f func()) {
	go func() {
		select {
		case v.queue <- f:
		case <-v.quit:
		}
	}()
}

// Connect turns the VPN on, or reconnects it.
func (v *VPN) Connect() {
	v.do(func() {
		v.save(func(s *savedState) { s.On = true })
		v.connect()
	})
}

// Disconnect turns the VPN off.
func (v *VPN) Disconnect() {
	v.do(func() {
		v.save(func(s *savedState) { s.On = false })
		v.disconnect()
	})
}

// Reconnect restarts a VPN that is on, to pick up a change of the profile,
// the mode or the protocols.
func (v *VPN) Reconnect() {
	v.do(func() {
		if v.up || v.waitNet {
			v.connect()
		}
	})
}

// Mode returns the routing mode.
func (v *VPN) Mode() string {
	var m string
	v.do(func() { m = v.saved.Mode })
	return m
}

// SetMode saves the routing mode; a VPN that is on reconnects in it.
func (v *VPN) SetMode(mode string) {
	v.do(func() {
		v.save(func(s *savedState) { s.Mode = mode })
		if v.up || v.waitNet {
			v.connect()
		}
	})
}

// WasOn reports whether the user left the VPN on.
func (v *VPN) WasOn() bool {
	var on bool
	v.do(func() { on = v.saved.On })
	return on
}

// Close turns the VPN off for a stop of the service, keeping whether the
// user left it on, and ends the worker.
func (v *VPN) Close() {
	v.do(v.disconnect)
	close(v.quit)
}

// Status returns what the VPN is doing now.
func (v *VPN) Status() Status {
	v.stMu.Lock()
	defer v.stMu.Unlock()
	return v.status
}

// Subscribe calls f with every new status until cancel is called.
func (v *VPN) Subscribe(f func(Status)) (cancel func()) {
	v.stMu.Lock()
	id := v.nextID
	v.nextID++
	v.listeners[id] = f
	v.stMu.Unlock()
	return func() {
		v.stMu.Lock()
		delete(v.listeners, id)
		v.stMu.Unlock()
	}
}

func (v *VPN) setStatus(phase, err string) {
	v.stMu.Lock()
	v.status = Status{Phase: phase, Error: err}
	s := v.status
	ls := make([]func(Status), 0, len(v.listeners))
	for _, f := range v.listeners {
		ls = append(ls, f)
	}
	v.stMu.Unlock()
	for _, f := range ls {
		f(s)
	}
}

func (v *VPN) save(change func(*savedState)) {
	change(&v.saved)
	b, _ := json.Marshal(v.saved)
	os.MkdirAll(v.dir, 0o700)
	os.WriteFile(filepath.Join(v.dir, stateFile), b, 0o600)
}

func (v *VPN) note(event string) { v.eng.Note(event) }

// connect starts the core and the TUN over the network in use, or waits for
// a network when there is none.
func (v *VPN) connect() {
	v.epoch++
	stop(v.netTimer)
	v.setStatus(Connecting, "")
	v.stopCore()
	v.watchNetwork()
	u, ok, err := v.plat.Uplink()
	if err != nil || !ok {
		// Started with the system before the network: connect once it is up.
		v.hasUplink = false
		v.waitNet = true
		v.note("нет сети, подключусь, когда она появится")
		v.setStatus(Connecting, "Жду сеть")
		return
	}
	v.waitNet = false
	v.uplink, v.hasUplink = u, true
	bypass.Set(u.Index, u.dns())
	mtu := v.eng.PickMTU(u.Kind, u.MTU)
	// The smart mode tunes its DPI evasion per kind of network.
	v.eng.SetNetwork(u.Kind)
	if err := v.eng.StartTUN(TUNName, v.saved.Mode, mtu); err != nil {
		v.fail(err)
		return
	}
	v.up = true
	if err := v.plat.ConfigureTUN(TUNName, mtu); err != nil {
		v.fail(fmt.Errorf("сетевой адаптер: %w", err))
		return
	}
	v.mtu = mtu
	v.note(fmt.Sprintf("подключено: режим %s, MTU %d, %s", v.saved.Mode, mtu, cmp.Or(u.Kind, "сеть ?")))
	v.setStatus(Connected, "")
	v.fails, v.offline = 0, false
	v.scheduleWatch(watchEvery)
}

func (v *VPN) fail(err error) {
	v.note("ошибка подключения: " + err.Error())
	v.stopCore()
	v.unwatchNetwork()
	v.setStatus(Failed, err.Error())
}

func (v *VPN) disconnect() {
	v.epoch++
	v.waitNet = false
	v.stopCore()
	v.unwatchNetwork()
	bypass.Set(0, nil)
	v.setStatus(Disconnected, "")
}

func (v *VPN) stopCore() {
	stop(v.watchTimer)
	if v.up {
		v.plat.ReleaseTUN(TUNName)
	}
	if v.eng.Running() {
		if err := v.eng.Stop(); err != nil {
			v.note("остановка ядра: " + err.Error())
		}
	}
	v.up = false
	v.checking = false
}

func stop(t *time.Timer) {
	if t != nil {
		t.Stop()
	}
}

func (v *VPN) scheduleWatch(d time.Duration) {
	stop(v.watchTimer)
	epoch := v.epoch
	v.watchTimer = time.AfterFunc(d, func() {
		v.post(func() {
			if epoch != v.epoch || !v.up {
				return
			}
			v.scheduleWatch(watchEvery)
			v.watch()
		})
	})
}

// watch is the watchdog: a check asks t.me and YouTube through the server
// in use; a failed one is repeated soon. Failures in a row reopen the
// connections to the servers, then restart the core. While the computer's
// network has no internet nothing is restarted; the connections are
// reopened the moment it is back.
func (v *VPN) watch() {
	if !v.up || v.checking {
		return
	}
	if !v.eng.Running() {
		v.note("сторож: ядро не работает, переподключаю")
		v.connect()
		return
	}
	// The core started without the routing lists and has fetched them.
	if v.eng.TakeRestart() {
		v.connect()
		return
	}
	v.checking = true
	wasOffline, began := v.offline, v.epoch
	go func() {
		online := true
		if wasOffline {
			online = v.eng.Online()
		}
		healthy := online && v.eng.Healthy()
		if !healthy && !wasOffline {
			online = v.eng.Online()
		}
		v.post(func() {
			v.checking = false
			// A check from before a connect or a change of network says
			// nothing about now: check again soon.
			if began == v.epoch {
				v.judge(healthy, online)
			} else if v.up {
				v.scheduleWatch(recheckAfter)
			}
		})
	}()
}

func (v *VPN) judge(healthy, online bool) {
	if !v.up {
		return
	}
	if healthy {
		if v.offline || v.fails >= failsToReset {
			v.note(fmt.Sprintf("сторож: канал вернулся, переоткрыто соединений: %d", v.eng.ResetConnections()))
		} else if v.fails > 0 {
			v.note("сторож: сервер снова отвечает")
		}
		v.fails, v.offline = 0, false
		return
	}
	if !online {
		if !v.offline {
			v.note("сторож: в сети компьютера нет интернета, жду его")
		}
		v.offline = true
		v.scheduleWatch(offlineRecheck)
		return
	}
	// The internet is back, but not the channel: reopen at once.
	back := v.offline
	v.offline = false
	v.fails++
	v.note(fmt.Sprintf("сторож: сервер не ответил (%d)", v.fails))
	if v.fails >= failsToRestart && time.Since(v.lastRestart) > restartGap {
		v.restartUnlessFreezing()
		return
	}
	if back || v.fails == failsToReset {
		v.note(fmt.Sprintf("сторож: переоткрыто соединений: %d", v.eng.ResetConnections()))
	}
	// One failure may be a lost packet: check again soon.
	v.scheduleWatch(recheckAfter)
}

// restartUnlessFreezing restarts the core, unless the network freezes
// connections: then fresh ones work and only die after some data (DPI), and
// a restart would only break every connection the apps have. The user is
// told what helps.
func (v *VPN) restartUnlessFreezing() {
	v.checking = true
	began := v.epoch
	go func() {
		hint := v.eng.Freezing()
		v.post(func() {
			v.checking = false
			if !v.up || began != v.epoch {
				return
			}
			if hint == "" {
				v.lastRestart = time.Now()
				v.note("сторож: перезапускаю ядро")
				v.connect()
				return
			}
			v.note("сторож: свежие соединения работают, старые замерзают: ядро не перезапускаю. " + hint)
			v.fails = 0
			if v.Status().Phase == Connected {
				v.setStatus(Connected, hint)
			}
		})
	}()
}

// watchNetwork follows the computer's network while the VPN is on.
func (v *VPN) watchNetwork() {
	if v.stopWatch != nil {
		return
	}
	stopFn, err := v.plat.WatchNetwork(func() { v.post(v.networkMoved) })
	if err != nil {
		v.note("не слежу за сетью: " + err.Error())
		return
	}
	v.stopWatch = stopFn
}

func (v *VPN) unwatchNetwork() {
	stop(v.netTimer)
	if v.stopWatch != nil {
		v.stopWatch()
		v.stopWatch = nil
	}
	v.hasUplink = false
}

// networkMoved waits for the network to settle: a network that flaps is
// followed once.
func (v *VPN) networkMoved() {
	if v.stopWatch == nil {
		return
	}
	wait := max(netSettle, time.Until(v.lastNetSwitch.Add(netSwitchGap)))
	stop(v.netTimer)
	v.netTimer = time.AfterFunc(wait, func() { v.post(v.followNetwork) })
}

// followNetwork moves the VPN to the network the computer uses now without
// rebuilding the tunnel: the connections of the old network are closed, so
// the apps reconnect at once over the new one. Only a path narrower than
// the tunnel's MTU rebuilds it.
func (v *VPN) followNetwork() {
	if v.stopWatch == nil {
		return
	}
	u, ok, err := v.plat.Uplink()
	if err != nil || !ok {
		if v.hasUplink {
			v.note("сеть пропала, жду другую")
		}
		v.hasUplink = false
		return
	}
	if v.waitNet {
		v.connect()
		return
	}
	if !v.up {
		return
	}
	old, had := v.uplink, v.hasUplink
	v.uplink, v.hasUplink = u, true
	bypass.Set(u.Index, u.dns())
	if had && u.Index == old.Index {
		if !u.keeps(old) {
			v.note(fmt.Sprintf("адрес в сети сменился, переоткрыто соединений: %d", v.eng.ResetConnections()))
		}
		return
	}
	v.lastNetSwitch = time.Now()
	v.epoch++
	kind := cmp.Or(u.Kind, "?")
	want := v.eng.PickMTU(u.Kind, u.MTU)
	if want < v.mtu && (v.eng.PathKnown() || u.MTU > 0 && u.MTU < v.mtu) {
		v.note(fmt.Sprintf("сеть сменилась (%s), MTU %d -> %d, переподключаю", kind, v.mtu, want))
		v.connect()
		return
	}
	closed := v.eng.SwitchNetwork(u.Kind)
	v.note(fmt.Sprintf("сеть сменилась (%s): переоткрыто соединений %d, туннель тот же", kind, closed))
	v.fails, v.offline = 0, false
	v.scheduleWatch(recheckAfter)
}
