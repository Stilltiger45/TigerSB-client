package smart_test

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xtls/xray-core/core"

	"github.com/Stilltiger45/TigerSB-client/internal/smart"
	"github.com/Stilltiger45/TigerSB-client/internal/xrayconf"
)

// dpi stands in front of a site like a DPI box that reads the first segment
// of a connection and cuts the connection when it carries the site's name.
type dpi struct {
	addr            string
	blocked, passed atomic.Int32
}

func newDPI(t *testing.T, target string, name []byte) *dpi {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	d := &dpi{addr: l.Addr().String()}
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
	if bytes.Contains(first[:n], name) {
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

// blockedSetup runs the picker with one server that DPI cuts, a direct route
// that DPI cuts too, and the presets with copies of the server through them.
// Every route ends at the same site behind the DPI box.
func blockedSetup(t *testing.T, ev *smart.Evasion) (*http.Client, *core.Instance, *dpi) {
	open := newSite(t, "open", http.StatusOK)
	box := newDPI(t, open.addr(), []byte("example.com"))
	redirect := func(ob map[string]any) map[string]any {
		s, _ := ob["settings"].(map[string]any)
		if s == nil {
			s = map[string]any{}
		}
		s["redirect"] = box.addr
		ob["settings"] = s
		return ob
	}
	extra := []any{
		redirect(map[string]any{"tag": "direct", "protocol": "freedom"}),
		redirect(map[string]any{"tag": "foreign/a", "protocol": "freedom"}),
	}
	for k := 1; k <= len(xrayconf.Presets); k++ {
		extra = append(extra,
			redirect(xrayconf.DPIOutbound(k)),
			redirect(map[string]any{
				"tag": xrayconf.VariantTag(k, "foreign/a"), "protocol": "freedom",
				"streamSettings": map[string]any{"sockopt": map[string]any{"dialerProxy": xrayconf.DPITag(k)}},
			}))
	}
	// The site's certificate must verify: start trusts the routes' sites.
	c, inst := start(t, []route{{"cert-only", open}}, smart.Options{Candidates: []string{"foreign/a"}, Evasion: ev}, extra...)
	return c, inst, box
}

func TestEvasionOpensSiteDPIBlocks(t *testing.T) {
	c, _, box := blockedSetup(t, &smart.Evasion{Preset: 3, Variants: []string{"foreign/a"}})
	if got := get(t, c, "https://example.com/"); got != "open" {
		t.Fatalf("got %q", got)
	}
	if box.blocked.Load() == 0 || box.passed.Load() == 0 {
		t.Errorf("DPI blocked %d, passed %d: the plain route must be cut and the preset must pass", box.blocked.Load(), box.passed.Load())
	}
}

func TestWithoutEvasionDPIWins(t *testing.T) {
	c, _, _ := blockedSetup(t, nil)
	if _, err := c.Get("https://example.com/"); err == nil {
		t.Fatal("the site opened although DPI cuts every route")
	}
}

func TestTunePicksPresetThatPasses(t *testing.T) {
	ev := &smart.Evasion{Preset: 1, Variants: []string{"foreign/a"}, Sites: []string{"example.com"}, ProbeURL: "https://example.com/"}
	c, inst, _ := blockedSetup(t, ev)
	res, err := smart.Tune(inst, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if res.Plain != 0 || res.Opened != res.Checks || res.Checks != 2 {
		t.Errorf("tune %+v: nothing opens plainly, the picked preset must open the server and the site", res)
	}
	if got := get(t, c, "https://example.com/"); got != "open" {
		t.Fatalf("after tuning got %q", got)
	}
	t.Log(res)
}
