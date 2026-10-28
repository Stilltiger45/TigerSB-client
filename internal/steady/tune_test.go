package steady

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xtls/xray-core/core"

	"github.com/Stilltiger45/TigerSB-client/internal/engine"
	"github.com/Stilltiger45/TigerSB-client/internal/xrayconf"
)

// dpi stands in front of a site like a DPI box that reads the first segment
// of a connection and cuts the connection when it carries the site's name.
type dpi struct {
	addr            string
	on, all         atomic.Bool // cut the site's name; cut everything
	blocked, passed atomic.Int32
}

func newDPI(t *testing.T, target string, name []byte) *dpi {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	d := &dpi{addr: l.Addr().String()}
	d.on.Store(true)
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go d.serve(c, target, name)
		}
	}()
	return d
}

func (d *dpi) serve(c net.Conn, target string, name []byte) {
	defer c.Close()
	first := make([]byte, 4096)
	c.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, err := c.Read(first)
	if err != nil {
		return
	}
	c.SetReadDeadline(time.Time{})
	if d.all.Load() || d.on.Load() && bytes.Contains(first[:n], name) {
		d.blocked.Add(1)
		c.(*net.TCPConn).SetLinger(0) // reset, as DPI does
		return
	}
	d.passed.Add(1)
	up, err := net.Dial("tcp", target)
	if err != nil {
		return
	}
	defer up.Close()
	up.Write(first[:n])
	go io.Copy(up, c)
	io.Copy(c, up)
}

// evasionSetup runs a picker with one server behind the DPI box, and its
// copies through every preset, as xrayconf.Build makes them in the smart
// mode. The server stands in as a freedom outbound sent to the box.
func evasionSetup(t *testing.T, preset int) (*core.Instance, *dpi) {
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(site.Close)
	box := newDPI(t, site.Listener.Addr().String(), []byte("example.com"))
	redirect := func(ob map[string]any) map[string]any {
		s, _ := ob["settings"].(map[string]any)
		if s == nil {
			s = map[string]any{}
		}
		s["redirect"] = box.addr
		ob["settings"] = s
		return ob
	}
	const server = "ru/x/a"
	outbounds := []any{redirect(map[string]any{"tag": server, "protocol": "freedom"})}
	for k := 1; k <= len(xrayconf.Presets); k++ {
		outbounds = append(outbounds,
			redirect(xrayconf.DPIOutbound(k)),
			redirect(map[string]any{
				"tag": xrayconf.VariantTag(k, server), "protocol": "freedom",
				"streamSettings": map[string]any{"sockopt": map[string]any{"dialerProxy": xrayconf.DPITag(k)}},
			}))
	}
	cfg, _ := json.Marshal(map[string]any{"log": map[string]any{"loglevel": "none"}, "outbounds": outbounds})
	inst, err := engine.Start(cfg, func(inst *core.Instance) error {
		return Attach(inst, Options{
			Tag: "pick/ru", Tags: []string{server}, Variants: []string{server},
			Preset: preset, ProbeURL: "http://example.com/",
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { inst.Close() })
	return inst, box
}

// The box cuts the plain way and the presets that split only a TLS
// ClientHello (the check is plain HTTP); the one that splits the start of
// any connection passes, and the picker moves to it.
func TestTunePicksPresetThatPasses(t *testing.T) {
	inst, box := evasionSetup(t, 0)
	res, err := Tune(inst, 6*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if res.Preset != 3 || res.Plain != 0 || res.Opened != 1 || res.Checks != 1 || !res.Changed {
		t.Fatalf("tune %+v: want preset 3", res)
	}
	if got := Current(inst, "pick/ru"); got != xrayconf.VariantTag(3, "ru/x/a") {
		t.Errorf("picker sends through %s", got)
	}
	if box.blocked.Load() == 0 || box.passed.Load() == 0 {
		t.Errorf("DPI blocked %d, passed %d", box.blocked.Load(), box.passed.Load())
	}
	t.Log(res)

	// Without DPI the plain way opens as well, and no preset is used.
	box.on.Store(false)
	res, err = Tune(inst, 6*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if res.Preset != 0 || res.Plain != 1 || !res.Changed {
		t.Fatalf("tune without DPI %+v: want none", res)
	}
	if got := Current(inst, "pick/ru"); got != "ru/x/a" {
		t.Errorf("picker sends through %s", got)
	}
	t.Log(res)
}

// SetPreset starts from the preset saved for a network; when nothing opens
// at all, Tune keeps the preset in use.
func TestSetPresetAndNothingOpens(t *testing.T) {
	inst, box := evasionSetup(t, 2)
	if got := Current(inst, "pick/ru"); got != xrayconf.VariantTag(2, "ru/x/a") {
		t.Errorf("start with preset 2: %s", got)
	}
	SetPreset(inst, 1)
	if got := Current(inst, "pick/ru"); got != xrayconf.VariantTag(1, "ru/x/a") {
		t.Errorf("after SetPreset(1): %s", got)
	}
	SetPreset(inst, 9) // out of range: ignored
	box.all.Store(true)
	res, err := Tune(inst, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if res.Preset != 1 || res.Opened != 0 || res.Changed {
		t.Errorf("tune with nothing open %+v: want preset 1 kept", res)
	}
}

// Without copies through the presets there is nothing to tune.
func TestTuneWithoutVariants(t *testing.T) {
	cfg, _ := json.Marshal(map[string]any{"log": map[string]any{"loglevel": "none"}, "outbounds": []any{
		map[string]any{"tag": "ru/x/a", "protocol": "freedom"},
		map[string]any{"tag": "ru/x/b", "protocol": "freedom"},
	}})
	inst, err := engine.Start(cfg, func(inst *core.Instance) error {
		return Attach(inst, Options{Tag: "pick/ru", Tags: []string{"ru/x/a", "ru/x/b"}, Preset: 2})
	})
	if err != nil {
		t.Fatal(err)
	}
	defer inst.Close()
	if _, err := Tune(inst, time.Second); err == nil {
		t.Error("tuned a picker without copies")
	}
	if got := Current(inst, "pick/ru"); got != "ru/x/a" {
		t.Errorf("a preset without copies changed the outbound: %s", got)
	}
}
