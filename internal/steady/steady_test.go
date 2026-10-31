package steady

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/features/outbound"

	"github.com/Stilltiger45/TigerSB-client/internal/engine"
)

func TestChoose(t *testing.T) {
	tags := []string{"a", "b", "c"}
	live := func(rtt float64) view { return view{known: true, alive: true, rtt: rtt} }
	dead := view{known: true}
	cases := []struct {
		name     string
		cur      string
		measured bool
		v        map[string]view
		want     string
		wantAll  bool
	}{
		{"nothing measured keeps the best priority", "", false, map[string]view{}, "a", false},
		{"follows the fastest until all are measured", "a", false, map[string]view{"a": live(300), "b": live(100)}, "b", false},
		{"all measured", "b", false, map[string]view{"a": live(300), "b": live(100), "c": dead}, "b", true},
		{"a bit faster is not enough", "a", true, map[string]view{"a": live(120), "b": live(100), "c": live(110)}, "a", true},
		{"clearly faster takes over", "a", true, map[string]view{"a": live(300), "b": live(100), "c": live(250)}, "b", true},
		{"30 ms faster is noise even at a third less", "a", true, map[string]view{"a": live(60), "b": live(30), "c": dead}, "a", true},
		{"dead one is left at once", "a", true, map[string]view{"a": dead, "b": live(400), "c": live(200)}, "c", true},
		{"nothing alive keeps the one in use", "b", true, map[string]view{"a": dead, "b": dead, "c": dead}, "b", true},
	}
	for _, c := range cases {
		got, all := choose(c.cur, c.measured, tags, c.v)
		if got != c.want || all != c.wantAll {
			t.Errorf("%s: got %s %v, want %s %v", c.name, got, all, c.want, c.wantAll)
		}
	}
}

// TestLeavesDeadTransport runs Xray with a dead best-priority transport and a
// live one, and checks that the picker moves to the live one once the
// observatory has measured them.
func TestLeavesDeadTransport(t *testing.T) {
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer site.Close()
	cfg, _ := json.Marshal(map[string]any{
		"log": map[string]any{"loglevel": "none"},
		"outbounds": []any{
			map[string]any{"tag": "ru/x/dead", "protocol": "blackhole"},
			map[string]any{"tag": "ru/x/live", "protocol": "freedom"},
		},
		"observatory": map[string]any{
			"subjectSelector": []string{"ru/"},
			"probeURL":        site.URL,
			"probeInterval":   "1s",
		},
	})
	inst, err := engine.Start(cfg, func(inst *core.Instance) error {
		return Attach(inst, Options{Tag: "pick/ru", Tags: []string{"ru/x/dead", "ru/x/live"}, ProbeURL: site.URL})
	})
	if err != nil {
		t.Fatal(err)
	}
	defer inst.Close()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		if Current(inst, "pick/ru") == "ru/x/live" {
			return
		}
	}
	t.Errorf("still on %s", Current(inst, "pick/ru"))
}

// TestPrimeLeavesDeadTransport has no observatory at all: the check of every
// transport right after the start alone moves the picker off the dead one.
func TestPrimeLeavesDeadTransport(t *testing.T) {
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer site.Close()
	cfg, _ := json.Marshal(map[string]any{
		"log": map[string]any{"loglevel": "none"},
		"outbounds": []any{
			map[string]any{"tag": "ru/x/dead", "protocol": "blackhole"},
			map[string]any{"tag": "ru/x/live", "protocol": "freedom"},
		},
	})
	inst, err := engine.Start(cfg, func(inst *core.Instance) error {
		return Attach(inst, Options{Tag: "pick/ru", Tags: []string{"ru/x/dead", "ru/x/live"}, ProbeURL: site.URL})
	})
	if err != nil {
		t.Fatal(err)
	}
	defer inst.Close()
	for deadline := time.Now().Add(8 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		if Current(inst, "pick/ru") == "ru/x/live" {
			return
		}
	}
	t.Errorf("still on %s", Current(inst, "pick/ru"))
}

// TestRenewForgetsOldNetwork checks that a transport avoided on the old
// network is checked again and taken back after a change of network.
func TestRenewForgetsOldNetwork(t *testing.T) {
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer site.Close()
	cfg, _ := json.Marshal(map[string]any{
		"log": map[string]any{"loglevel": "none"},
		"outbounds": []any{
			map[string]any{"tag": "ru/x/live", "protocol": "freedom"},
			map[string]any{"tag": "ru/x/dead", "protocol": "blackhole"},
		},
	})
	inst, err := engine.Start(cfg, func(inst *core.Instance) error {
		return Attach(inst, Options{Tag: "pick/ru", Tags: []string{"ru/x/live", "ru/x/dead"}, ProbeURL: site.URL})
	})
	if err != nil {
		t.Fatal(err)
	}
	defer inst.Close()
	ohm, _ := inst.GetFeature(outbound.ManagerType()).(outbound.Manager)
	h := ohm.GetHandler("pick/ru").(*Handler)
	for deadline := time.Now().Add(8 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		h.mu.Lock()
		primed := len(h.alive) == 2
		h.mu.Unlock()
		if primed {
			break
		}
	}
	// On the old network the live one failed its checks.
	h.mu.Lock()
	h.down["ru/x/live"] = time.Now().Add(time.Hour)
	h.mu.Unlock()
	usable := func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		return h.views(time.Now())["ru/x/live"].alive
	}
	if usable() {
		t.Fatal("the avoided transport counts as usable")
	}
	Renew(inst)
	for deadline := time.Now().Add(8 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		if usable() {
			return
		}
	}
	t.Error("the transport avoided on the old network is still avoided")
}

// freezer stands in front of a site like Russian DPI on a connection abroad:
// it passes what the site sends up to limit bytes and then nothing, keeping
// the connection open. A negative limit passes everything.
type freezer struct {
	addr  string
	limit atomic.Int64
}

func newFreezer(t *testing.T, target string) *freezer {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	f := &freezer{addr: l.Addr().String()}
	f.limit.Store(-1)
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				up, err := net.Dial("tcp", target)
				if err != nil {
					return
				}
				defer up.Close()
				done := make(chan struct{})
				go func() { io.Copy(up, c); close(done) }()
				if n := f.limit.Load(); n >= 0 {
					io.CopyN(c, up, n)
					<-done
					return
				}
				io.Copy(c, up)
			}()
		}
	}()
	return f
}

// frozenSetup runs a picker over two transports to a site with a big page:
// "ru/x/frozen" goes through a freezer, "ru/x/live" straight. A dokodemo
// inbound on the returned port carries connections to the site through the
// picker.
func frozenSetup(t *testing.T) (*core.Instance, *Handler, *freezer, int, string) {
	big := bytes.Repeat([]byte("x"), 256<<10)
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/big" {
			w.Write(big)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(site.Close)
	f := newFreezer(t, site.Listener.Addr().String())
	in, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := in.Addr().(*net.TCPAddr).Port
	in.Close()
	host, sitePort, _ := net.SplitHostPort(site.Listener.Addr().String())
	cfg, _ := json.Marshal(map[string]any{
		"log": map[string]any{"loglevel": "none"},
		"inbounds": []any{map[string]any{
			"tag": "in", "listen": "127.0.0.1", "port": port, "protocol": "dokodemo-door",
			"settings": map[string]any{"address": host, "port": atoi(sitePort), "network": "tcp"},
		}},
		"outbounds": []any{
			map[string]any{"tag": "ru/x/frozen", "protocol": "freedom", "settings": map[string]any{"redirect": f.addr}},
			map[string]any{"tag": "ru/x/live", "protocol": "freedom"},
		},
		"routing": map[string]any{"rules": []any{
			map[string]any{"type": "field", "inboundTag": []string{"in"}, "outboundTag": "pick/ru"},
		}},
	})
	inst, err := engine.Start(cfg, func(inst *core.Instance) error {
		return Attach(inst, Options{
			Tag: "pick/ru", Tags: []string{"ru/x/frozen", "ru/x/live"}, ProbeURL: site.URL,
			BulkURLs: []string{site.URL + "/big"}, Stall: time.Second, BulkTimeout: 2 * time.Second,
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { inst.Close() })
	ohm, _ := inst.GetFeature(outbound.ManagerType()).(outbound.Manager)
	return inst, ohm.GetHandler("pick/ru").(*Handler), f, port, site.URL
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatal(what)
}

// TestPrimeFindsFrozenTransport: the frozen transport answers the small
// checks, so only the download right after the start tells it apart.
func TestPrimeFindsFrozenTransport(t *testing.T) {
	inst, h, f, _, _ := frozenSetup(t)
	_ = inst
	f.limit.Store(16 << 10)
	h.renew() // the start, now with DPI in place
	waitFor(t, "the frozen transport is still in use", func() bool { return Current(inst, "pick/ru") == "ru/x/live" })
	h.mu.Lock()
	froze := time.Now().Before(h.froze["ru/x/frozen"])
	h.mu.Unlock()
	if !froze {
		t.Error("the frozen transport is not marked")
	}
}

// TestStallFindsFrozenTransport: the transport in use freezes in the middle
// of the session; a connection that gets nothing back has the picker check
// it and leave it.
func TestStallFindsFrozenTransport(t *testing.T) {
	inst, h, f, port, _ := frozenSetup(t)
	waitFor(t, "not primed", func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		return len(h.alive) == 2 && !h.bulking && !h.bulked.IsZero()
	})
	h.mu.Lock()
	h.cur, h.measured = "ru/x/frozen", true
	h.rtt["ru/x/frozen"], h.rtt["ru/x/live"] = 1, 1
	h.mu.Unlock()
	if Current(inst, "pick/ru") != "ru/x/frozen" {
		t.Fatal("not on the frozen transport")
	}
	time.Sleep(2 * time.Second) // past the pause after the last inspection
	f.limit.Store(0)            // its connections get nothing back now
	c, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.Write([]byte("GET / HTTP/1.1\r\nHost: x\r\n\r\n"))
	waitFor(t, "the frozen transport is still in use", func() bool { return Current(inst, "pick/ru") == "ru/x/live" })
}

func atoi(s string) int { n, _ := strconv.Atoi(s); return n }
