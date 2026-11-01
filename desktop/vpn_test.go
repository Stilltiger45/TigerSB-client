package desktop

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Stilltiger45/TigerSB-client/internal/bypass"
	"github.com/Stilltiger45/TigerSB-client/mobile"
)

type fakeEngine struct {
	mu       sync.Mutex
	running  bool
	healthy  bool
	online   bool
	freezing string
	startErr error
	starts   []string // modes
	resets   int
	switches []string
	notes    []string
}

func (e *fakeEngine) StartTUN(name, mode string, mtu int) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.startErr != nil {
		return e.startErr
	}
	e.running = true
	e.starts = append(e.starts, mode)
	return nil
}
func (e *fakeEngine) Stop() error       { e.mu.Lock(); e.running = false; e.mu.Unlock(); return nil }
func (e *fakeEngine) Running() bool     { e.mu.Lock(); defer e.mu.Unlock(); return e.running }
func (e *fakeEngine) Healthy() bool     { e.mu.Lock(); defer e.mu.Unlock(); return e.healthy }
func (e *fakeEngine) Online() bool      { e.mu.Lock(); defer e.mu.Unlock(); return e.online }
func (e *fakeEngine) Freezing() string  { e.mu.Lock(); defer e.mu.Unlock(); return e.freezing }
func (e *fakeEngine) TakeRestart() bool { return false }
func (e *fakeEngine) ResetConnections() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.resets++
	return 3
}
func (e *fakeEngine) SwitchNetwork(kind string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.switches = append(e.switches, kind)
	return 2
}
func (e *fakeEngine) PickMTU(string, int) int { return 1480 }
func (e *fakeEngine) PathKnown() bool         { return false }
func (e *fakeEngine) SetNetwork(string)       {}
func (e *fakeEngine) Note(s string)           { e.mu.Lock(); e.notes = append(e.notes, s); e.mu.Unlock() }

func (e *fakeEngine) noted(part string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.ContainsFunc(e.notes, func(n string) bool { return strings.Contains(n, part) })
}

type fakePlatform struct {
	mu         sync.Mutex
	uplink     Uplink
	ok         bool
	changed    func()
	configured int
	released   int
}

func (p *fakePlatform) Uplink() (Uplink, bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.uplink, p.ok, nil
}
func (p *fakePlatform) WatchNetwork(changed func()) (func(), error) {
	p.mu.Lock()
	p.changed = changed
	p.mu.Unlock()
	return func() {
		p.mu.Lock()
		p.changed = nil
		p.mu.Unlock()
	}, nil
}
func (p *fakePlatform) ConfigureTUN(string, int) error {
	p.mu.Lock()
	p.configured++
	p.mu.Unlock()
	return nil
}
func (p *fakePlatform) ReleaseTUN(string) { p.mu.Lock(); p.released++; p.mu.Unlock() }

// move switches the computer to another network and tells the VPN.
func (p *fakePlatform) move(u Uplink, ok bool) {
	p.mu.Lock()
	p.uplink, p.ok = u, ok
	f := p.changed
	p.mu.Unlock()
	if f != nil {
		f()
	}
}

var wifi = Uplink{Index: 7, Kind: "wifi", MTU: 1500, DNS: []netip.Addr{netip.MustParseAddr("192.168.1.1")},
	Addrs: []netip.Addr{netip.MustParseAddr("192.168.1.20")}}

func fast(t *testing.T) {
	old := []time.Duration{watchEvery, recheckAfter, offlineRecheck, netSettle, netSwitchGap, restartGap}
	watchEvery, recheckAfter, offlineRecheck = 30*time.Millisecond, 10*time.Millisecond, 10*time.Millisecond
	netSettle, netSwitchGap, restartGap = 10*time.Millisecond, 10*time.Millisecond, 0
	t.Cleanup(func() {
		watchEvery, recheckAfter, offlineRecheck, netSettle, netSwitchGap, restartGap = old[0], old[1], old[2], old[3], old[4], old[5]
		bypass.Set(0, nil)
	})
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatal("never: " + what)
}

func TestConnectFollowsNetworkAndRemembers(t *testing.T) {
	fast(t)
	dir := t.TempDir()
	eng := &fakeEngine{healthy: true, online: true}
	plat := &fakePlatform{uplink: wifi, ok: true}
	v := NewVPN(eng, plat, dir)
	v.SetMode("basic")
	v.Connect()
	if st := v.Status(); st.Phase != Connected {
		t.Fatalf("status %+v", st)
	}
	if bypass.Index() != wifi.Index {
		t.Errorf("core sockets bound to %d, want the uplink %d", bypass.Index(), wifi.Index)
	}
	if !slices.Equal(eng.starts, []string{"basic"}) || plat.configured != 1 {
		t.Errorf("starts %v, TUN configured %d times", eng.starts, plat.configured)
	}

	// Another network: the tunnel stays, the connections are reopened.
	eth := Uplink{Index: 9, Kind: "ethernet", MTU: 1500, Addrs: []netip.Addr{netip.MustParseAddr("10.0.0.5")}}
	plat.move(eth, true)
	waitFor(t, "switch to ethernet", func() bool {
		eng.mu.Lock()
		defer eng.mu.Unlock()
		return slices.Equal(eng.switches, []string{"ethernet"})
	})
	if bypass.Index() != 9 || len(eng.starts) != 1 {
		t.Errorf("bound to %d after the switch, %d starts", bypass.Index(), len(eng.starts))
	}

	// A new address on the same network reopens the connections.
	eth2 := eth
	eth2.Addrs = []netip.Addr{netip.MustParseAddr("10.0.0.6")}
	plat.move(eth2, true)
	waitFor(t, "reset after a new address", func() bool { return eng.noted("адрес в сети сменился") })

	if !v.WasOn() {
		t.Error("the VPN the user left on is not remembered")
	}
	v.Close()
	// A stop of the service keeps the user's choice for the next start.
	again := NewVPN(eng, plat, dir)
	defer again.Close()
	if !again.WasOn() || again.Mode() != "basic" {
		t.Errorf("after a restart: on %v, mode %s", again.WasOn(), again.Mode())
	}
	again.Disconnect()
	if again.WasOn() || again.Status().Phase != Disconnected || bypass.Index() != 0 {
		t.Errorf("after disconnect: on %v, status %+v, bound to %d", again.WasOn(), again.Status(), bypass.Index())
	}
}

func TestWaitsForNetwork(t *testing.T) {
	fast(t)
	eng := &fakeEngine{healthy: true, online: true}
	plat := &fakePlatform{}
	v := NewVPN(eng, plat, t.TempDir())
	defer v.Close()
	v.Connect()
	if st := v.Status(); st.Phase != Connecting || len(eng.starts) != 0 {
		t.Fatalf("without a network: %+v, %d starts", st, len(eng.starts))
	}
	plat.move(wifi, true)
	waitFor(t, "connect once the network is up", func() bool { return v.Status().Phase == Connected })
}

func TestWatchdogRestartsAfterFailures(t *testing.T) {
	fast(t)
	eng := &fakeEngine{healthy: true, online: true}
	plat := &fakePlatform{uplink: wifi, ok: true}
	v := NewVPN(eng, plat, t.TempDir())
	defer v.Close()
	v.Connect()
	eng.mu.Lock()
	eng.healthy = false
	eng.mu.Unlock()
	waitFor(t, "connections reopened", func() bool { return eng.noted("сторож: переоткрыто") })
	waitFor(t, "core restarted", func() bool {
		eng.mu.Lock()
		defer eng.mu.Unlock()
		return len(eng.starts) >= 2
	})

	// Connections that freeze: no restart, advice instead.
	eng.mu.Lock()
	eng.freezing = "попробуйте другой протокол"
	n := len(eng.starts)
	eng.mu.Unlock()
	waitFor(t, "advice shown", func() bool { return v.Status().Error == "попробуйте другой протокол" })
	eng.mu.Lock()
	defer eng.mu.Unlock()
	if len(eng.starts) != n {
		t.Error("restarted although connections only freeze")
	}
}

func TestFailedStartShowsError(t *testing.T) {
	fast(t)
	eng := &fakeEngine{startErr: errTest}
	plat := &fakePlatform{uplink: wifi, ok: true}
	v := NewVPN(eng, plat, t.TempDir())
	defer v.Close()
	v.Connect()
	if st := v.Status(); st.Phase != Failed || st.Error != errTest.Error() {
		t.Fatalf("status %+v", st)
	}
}

var errTest = &testError{}

type testError struct{}

func (*testError) Error() string { return "ключ ещё не добавлен" }

// The app must send the token first; then it gets the status and answers.
func TestServer(t *testing.T) {
	fast(t)
	mobile.Init(t.TempDir(), "test")
	eng := &fakeEngine{healthy: true, online: true}
	plat := &fakePlatform{uplink: wifi, ok: true}
	v := NewVPN(eng, plat, t.TempDir())
	defer v.Close()
	srv := NewServer(v)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go srv.Serve(ctx, l)

	// A wrong token: the connection is closed.
	bad, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	bad.Write([]byte(`{"token":"nope"}` + "\n"))
	bad.SetReadDeadline(time.Now().Add(time.Second))
	if n, _ := bad.Read(make([]byte, 64)); n != 0 {
		t.Error("answered a wrong token")
	}
	bad.Close()

	c, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	r := bufio.NewReader(c)
	send := func(s string) { c.Write([]byte(s + "\n")) }
	read := func() map[string]any {
		t.Helper()
		c.SetReadDeadline(time.Now().Add(3 * time.Second))
		line, err := r.ReadBytes('\n')
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		json.Unmarshal(line, &m)
		return m
	}
	send(`{"token":"` + srv.Token() + `"}`)
	if m := read(); m["ev"] != "status" || m["phase"] != Disconnected {
		t.Fatalf("first message %v", m)
	}
	send(`{"id":1,"m":"setMode","a":{"mode":"all"}}`)
	if m := read(); m["id"] != 1.0 || m["e"] != nil {
		t.Fatalf("setMode: %v", m)
	}
	send(`{"id":2,"m":"mode"}`)
	if m := read(); m["r"] != "all" {
		t.Fatalf("mode: %v", m)
	}
	send(`{"id":3,"m":"current"}`)
	if m := read(); m["r"] != "" || m["e"] != nil {
		t.Fatalf("current without a key: %v", m)
	}
	send(`{"id":4,"m":"connect"}`)
	phases := map[string]bool{}
	for len(phases) < 2 || !phases[Connected] {
		m := read()
		if p, ok := m["phase"].(string); ok {
			phases[p] = true
		}
	}
	send(`{"id":5,"m":"nonsense"}`)
	if m := read(); m["e"] == nil {
		t.Fatalf("unknown request answered: %v", m)
	}
}
