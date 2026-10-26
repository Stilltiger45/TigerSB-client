package meter_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Stilltiger45/TigerSB-client/internal/engine"
	"github.com/Stilltiger45/TigerSB-client/internal/meter"
)

func TestTrafficAndPing(t *testing.T) {
	body := strings.Repeat("x", 10000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
	defer srv.Close()
	addr := strings.TrimPrefix(srv.URL, "http://")

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()

	// Two "servers": freedom outbounds that send everything to the test site.
	cfg, _ := json.Marshal(map[string]any{
		"log": map[string]any{"loglevel": "none"},
		"inbounds": []any{map[string]any{
			"tag": "socks-in", "protocol": "socks", "listen": "127.0.0.1", "port": port,
		}},
		"outbounds": []any{
			map[string]any{"tag": "foreign/a", "protocol": "freedom", "settings": map[string]any{"redirect": addr}},
			map[string]any{"tag": "foreign/b", "protocol": "freedom", "settings": map[string]any{"redirect": addr}},
		},
		"stats":  map[string]any{},
		"policy": map[string]any{"system": map[string]any{"statsOutboundUplink": true, "statsOutboundDownlink": true}},
	})
	inst, err := engine.Start(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer inst.Close()

	proxy, _ := url.Parse(fmt.Sprintf("socks5://127.0.0.1:%d", port))
	c := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxy)}, Timeout: 5 * time.Second}
	resp, err := c.Get("http://example.com/")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	c.CloseIdleConnections()

	tags := []string{"foreign/a", "foreign/b"}
	// Counters update when the connection ends.
	var up, down int64
	for range 50 {
		if up, down = meter.Traffic(inst, tags); up > 0 && down >= int64(len(body)) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if up == 0 || down < int64(len(body)) {
		t.Errorf("traffic up %d, down %d", up, down)
	}

	if d := meter.Ping(inst, meter.Fastest(inst, tags), "http://example.com/", 5*time.Second); d < 0 {
		t.Error("ping failed")
	}
	if d := meter.Ping(inst, "", "", time.Second); d != -1 {
		t.Errorf("ping without servers: %v", d)
	}
}

// TestCheckKeepAlive checks two sites with two requests each, the second on
// the connection of the first, and a dead outbound that fails in time.
func TestCheckKeepAlive(t *testing.T) {
	var conns, reqs atomic.Int32
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqs.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	srv.Config.ConnState = func(_ net.Conn, s http.ConnState) {
		if s == http.StateNew {
			conns.Add(1)
		}
	}
	srv.Start()
	defer srv.Close()
	cfg, _ := json.Marshal(map[string]any{
		"log": map[string]any{"loglevel": "none"},
		"outbounds": []any{
			map[string]any{"tag": "foreign/a", "protocol": "freedom", "settings": map[string]any{"redirect": strings.TrimPrefix(srv.URL, "http://")}},
			map[string]any{"tag": "foreign/dead", "protocol": "blackhole"},
		},
	})
	inst, err := engine.Start(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer inst.Close()
	urls := []string{"http://t.example/", "http://yt.example/generate_204"}
	if d := meter.Check(inst, "foreign/a", urls, 5*time.Second); d < 0 {
		t.Fatal("live outbound failed the check")
	}
	if reqs.Load() != 4 || conns.Load() != 2 {
		t.Errorf("%d requests over %d connections, want 4 over 2", reqs.Load(), conns.Load())
	}
	start := time.Now()
	if d := meter.Check(inst, "foreign/dead", urls, time.Second); d >= 0 {
		t.Error("dead outbound passed the check")
	}
	if time.Since(start) > 3*time.Second {
		t.Error("the check of a dead outbound overran its timeout")
	}
}
