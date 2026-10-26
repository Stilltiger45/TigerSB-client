package steady

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
