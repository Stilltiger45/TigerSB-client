package smart_test

import (
	"crypto/tls"
	"crypto/x509"
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

	"github.com/xtls/xray-core/core"

	"github.com/Stilltiger45/TigerSB-client/internal/engine"
	"github.com/Stilltiger45/TigerSB-client/internal/smart"
)

// site is a test HTTPS site; its certificate is valid for example.com.
type site struct {
	*httptest.Server
	hits atomic.Int32
}

func newSite(t *testing.T, name string, status int) *site {
	s := &site{}
	s.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.hits.Add(1)
		w.WriteHeader(status)
		fmt.Fprint(w, name)
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *site) addr() string { return strings.TrimPrefix(s.URL, "https://") }

// route is an outbound of the test core: a freedom outbound that sends every
// connection to one site, or a blackhole when site is nil (a dead server).
type route struct {
	tag  string
	site *site
}

func freePort(t *testing.T) int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// start runs Xray with a SOCKS inbound whose traffic all goes to the picker,
// and returns an HTTP client that uses it. extra are more outbounds.
func start(t *testing.T, routes []route, opt smart.Options, extra ...any) (*http.Client, *core.Instance) {
	t.Helper()
	port := freePort(t)
	var outbounds []any
	pool := x509.NewCertPool()
	for _, r := range routes {
		if r.site == nil {
			outbounds = append(outbounds, map[string]any{"tag": r.tag, "protocol": "blackhole"})
			continue
		}
		pool.AddCert(r.site.Certificate())
		outbounds = append(outbounds, map[string]any{
			"tag": r.tag, "protocol": "freedom",
			"settings": map[string]any{"redirect": r.site.addr()},
		})
	}
	outbounds = append(outbounds, extra...)
	cfg, _ := json.Marshal(map[string]any{
		"log": map[string]any{"loglevel": "none"},
		"inbounds": []any{map[string]any{
			"tag": "socks-in", "protocol": "socks", "listen": "127.0.0.1", "port": port,
			// TCP only: a UDP listener would need the port free for UDP too.
			"settings": map[string]any{"udp": false},
			"sniffing": map[string]any{"enabled": true, "destOverride": []string{"tls", "http"}, "routeOnly": true},
		}},
		"outbounds": outbounds,
		"routing": map[string]any{"rules": []any{
			map[string]any{"type": "field", "inboundTag": []string{"socks-in"}, "outboundTag": smart.Tag},
		}},
	})
	opt.RootCAs = pool
	if opt.Timeout == 0 {
		opt.Timeout = 2 * time.Second
	}
	inst, err := engine.Start(cfg, smart.Setup(opt))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { inst.Close() })

	proxy, _ := url.Parse(fmt.Sprintf("socks5://127.0.0.1:%d", port))
	return &http.Client{
		Transport: &http.Transport{
			Proxy:             http.ProxyURL(proxy),
			TLSClientConfig:   &tls.Config{RootCAs: pool},
			DisableKeepAlives: true,
		},
		Timeout: 10 * time.Second,
	}, inst
}

func get(t *testing.T, c *http.Client, u string) string {
	t.Helper()
	resp, err := c.Get(u)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}

// settled returns the counter once it has not moved for 200 ms (at most 3 s).
func settled(n *atomic.Int32) int32 {
	v := n.Load()
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); {
		time.Sleep(200 * time.Millisecond)
		now := n.Load()
		if now == v {
			return v
		}
		v = now
	}
	return v
}

func TestPicksServerThatOpensSite(t *testing.T) {
	refusing := newSite(t, "refusing", http.StatusForbidden)
	open := newSite(t, "open", http.StatusOK)
	home := newSite(t, "home", http.StatusOK)
	c, _ := start(t, []route{
		{"direct", home},
		{"foreign/dead", nil},
		{"foreign/refusing", refusing},
		{"ru/open", open},
	}, smart.Options{Candidates: []string{"foreign/dead", "foreign/refusing", "ru/open"}})

	if got := get(t, c, "https://example.com/"); got != "open" {
		t.Fatalf("site went to %q", got)
	}
	// The pick is remembered: the next visit is not checked again. The check
	// asks all servers at once and the refusing one may answer after the
	// pick, so wait for its count to settle first.
	before := settled(&refusing.hits)
	if got := get(t, c, "https://example.com/page"); got != "open" {
		t.Fatalf("second visit went to %q", got)
	}
	if refusing.hits.Load() != before {
		t.Error("the site was checked again")
	}
	if home.hits.Load() != 0 {
		t.Error("the direct route was checked although a server opened the site")
	}
}

func TestNothingWorksGoesDirect(t *testing.T) {
	home := newSite(t, "home", http.StatusOK)
	c, _ := start(t, []route{
		{"direct", home},
		{"foreign/a", nil},
		{"foreign/b", nil},
	}, smart.Options{Candidates: []string{"foreign/a", "foreign/b"}})
	if got := get(t, c, "https://example.com/"); got != "home" {
		t.Fatalf("site went to %q", got)
	}
}

func TestRefusedAbroadOpenAtHomeGoesDirect(t *testing.T) {
	refusing := newSite(t, "refusing", http.StatusForbidden)
	home := newSite(t, "home", http.StatusOK)
	c, _ := start(t, []route{
		{"direct", home},
		{"foreign/refusing", refusing},
		{"foreign/dead", nil},
	}, smart.Options{Candidates: []string{"foreign/refusing", "foreign/dead"}, Grace: 200 * time.Millisecond})
	if got := get(t, c, "https://example.com/"); got != "home" {
		t.Fatalf("site went to %q", got)
	}
}

func TestRefusedEverywhereKeepsServer(t *testing.T) {
	refusing := newSite(t, "refusing", http.StatusForbidden)
	homeRefusing := newSite(t, "home", http.StatusForbidden)
	c, _ := start(t, []route{
		{"direct", homeRefusing},
		{"foreign/refusing", refusing},
	}, smart.Options{Candidates: []string{"foreign/refusing"}})
	if got := get(t, c, "https://example.com/"); got != "refusing" {
		t.Fatalf("site went to %q", got)
	}
}

func TestNoDomainUsesDefault(t *testing.T) {
	a := newSite(t, "a", http.StatusOK)
	b := newSite(t, "b", http.StatusOK)
	home := newSite(t, "home", http.StatusOK)
	c, _ := start(t, []route{
		{"direct", home},
		{"foreign/a", a},
		{"ru/b", b},
	}, smart.Options{Candidates: []string{"ru/b", "foreign/a"}, Default: []string{"foreign/a"}})
	// An IP address carries no name to check (and Go sends no SNI for it).
	if got := get(t, c, "https://127.0.0.1/"); got != "a" {
		t.Fatalf("went to %q", got)
	}
	if a.hits.Load() != 1 || b.hits.Load() != 0 {
		t.Error("traffic without a domain must not be checked")
	}
}
