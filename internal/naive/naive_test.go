package naive_test

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	mrand "math/rand/v2"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xtls/xray-core/core"
	"golang.org/x/net/http2"

	"github.com/Stilltiger45/TigerSB-client/internal/engine"
	"github.com/Stilltiger45/TigerSB-client/internal/meter"
	"github.com/Stilltiger45/TigerSB-client/internal/naive"
	"github.com/Stilltiger45/TigerSB-client/internal/xrayconf"
	"github.com/Stilltiger45/TigerSB-client/pkg/profile"
)

// cert makes a self-signed certificate for naive.test and returns it with
// the hex SHA-256 that pins it.
func cert(t *testing.T) (tls.Certificate, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "naive.test"},
		DNSNames:     []string{"naive.test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(der)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, hex.EncodeToString(sum[:])
}

// server is a NaïveProxy server like Caddy's forwardproxy of klzgrad: CONNECT
// over HTTP/2 for the user "naive" with the password "secret", padding the
// first frames each way when the request asks for it. A server that does not
// pad (plain) never answers with the Padding header.
type server struct {
	port  int
	pin   string
	plain bool

	mu      sync.Mutex
	headers []http.Header
}

func newServer(t *testing.T, plain bool) *server {
	t.Helper()
	crt, pin := cert(t)
	l, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{crt}, NextProtos: []string{"h2"}})
	if err != nil {
		t.Fatal(err)
	}
	s := &server{port: l.Addr().(*net.TCPAddr).Port, pin: pin, plain: plain}
	hs := &http.Server{Handler: s, Protocols: new(http.Protocols)}
	hs.Protocols.SetHTTP2(true)
	go hs.Serve(l)
	t.Cleanup(func() { hs.Close() })
	return s
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.headers = append(s.headers, r.Header.Clone())
	s.mu.Unlock()
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("naive:secret"))
	if r.Method != http.MethodConnect || r.ProtoMajor != 2 || r.Header.Get("Proxy-Authorization") != want {
		w.WriteHeader(http.StatusProxyAuthRequired)
		return
	}
	pad := !s.plain && r.Header.Get("Padding") != ""
	if pad {
		w.Header().Set("Padding", strings.Repeat("~", 30+mrand.IntN(32)))
	}
	w.WriteHeader(http.StatusOK)
	rc := http.NewResponseController(w)
	rc.Flush()
	up, err := net.Dial("tcp", r.Host)
	if err != nil {
		return
	}
	defer up.Close()
	go func() {
		unpad(up, r.Body, pad)
		up.(*net.TCPConn).CloseWrite()
	}()
	addPadding(w, rc, up, pad)
}

// unpad copies the client's stream to dst, taking the padding off its first
// eight frames.
func unpad(dst io.Writer, src io.Reader, pad bool) {
	for frames := 0; pad && frames < 8; frames++ {
		var head [3]byte
		if _, err := io.ReadFull(src, head[:]); err != nil {
			return
		}
		b := make([]byte, int(head[0])<<8|int(head[1])+int(head[2]))
		if _, err := io.ReadFull(src, b); err != nil {
			return
		}
		if _, err := dst.Write(b[:len(b)-int(head[2])]); err != nil {
			return
		}
	}
	io.Copy(dst, src)
}

// addPadding copies src to the client, padding the first eight frames.
func addPadding(w io.Writer, rc *http.ResponseController, src io.Reader, pad bool) {
	b := make([]byte, 65536)
	for frames := 0; ; frames++ {
		var n int
		var err error
		if pad && frames < 8 {
			p := mrand.IntN(256)
			n, err = src.Read(b[3 : 65536-p])
			if n > 0 {
				b[0], b[1], b[2] = byte(n>>8), byte(n), byte(p)
				clear(b[3+n : 3+n+p])
				n += 3 + p
			}
		} else {
			n, err = src.Read(b)
		}
		if n > 0 {
			if _, err := w.Write(b[:n]); err != nil {
				return
			}
			rc.Flush()
		}
		if err != nil {
			return
		}
	}
}

func (s *server) seen() []http.Header {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.headers
}

// start runs the client's own config for the profile, with a SOCKS port.
func start(t *testing.T, p *profile.Profile) (*core.Instance, int) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	socks := l.Addr().(*net.TCPAddr).Port
	l.Close()
	cfg, err := xrayconf.Build(p, xrayconf.Options{
		Mode:     xrayconf.ModeAll,
		LogLevel: "none",
		Inbounds: []xrayconf.Inbound{{Tag: "socks-in", Protocol: "socks", Listen: "127.0.0.1", Port: socks}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(cfg), `"naive"`) {
		t.Fatalf("naive outbound left in the Xray config: %s", cfg)
	}
	inst, err := engine.Start(cfg, naive.Hooks(p)...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { inst.Close() })
	return inst, socks
}

func client(socks int) *http.Client {
	proxy, _ := url.Parse(fmt.Sprintf("socks5://127.0.0.1:%d", socks))
	return &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxy)}, Timeout: 10 * time.Second}
}

// site answers GET with size random bytes and POST with the SHA-256 of
// the body, so that a mangled frame either way shows.
func site(t *testing.T, size int) (*httptest.Server, []byte) {
	t.Helper()
	data := make([]byte, size)
	rand.Read(data)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			h := sha256.New()
			io.Copy(h, r.Body)
			fmt.Fprintf(w, "%x", h.Sum(nil))
			return
		}
		w.Write(data)
	}))
	t.Cleanup(s.Close)
	return s, data
}

// viaName is the site's URL by the name localhost: by its address
// 127.0.0.1 it would not go through the proxy, which the client keeps for
// sites outside the device.
func viaName(s *httptest.Server) string {
	return strings.Replace(s.URL, "127.0.0.1", "localhost", 1)
}

// roundTrip downloads and uploads a megabyte through the SOCKS port, which
// takes the eight padded frames each way and many plain ones after them.
func roundTrip(t *testing.T, socks int) {
	t.Helper()
	s, data := site(t, 1<<20)
	c := client(socks)
	resp, err := c.Get(viaName(s))
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("download: %d bytes of %d, equal %v, %v", len(got), len(data), bytes.Equal(got, data), err)
	}
	resp, err = c.Post(viaName(s), "application/octet-stream", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	sum, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if want := fmt.Sprintf("%x", sha256.Sum256(data)); string(sum) != want {
		t.Fatalf("upload arrived mangled: %s, want %s", sum, want)
	}
}

func TestNaive(t *testing.T) {
	for _, tc := range []struct {
		name        string
		plain       bool
		linkPadding string
		wantPadding bool
	}{
		{name: "padding", wantPadding: true},
		{name: "server without padding", plain: true, wantPadding: true},
		{name: "padding off in link", linkPadding: "&padding=false"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := newServer(t, tc.plain)
			link := fmt.Sprintf("naive+https://naive:secret@127.0.0.1:%d?sni=naive.test&pinSHA256=%s&extra-headers=X-Test%%3A%%20yes%s#Naive", srv.port, srv.pin, tc.linkPadding)
			p, err := profile.FromVLESS(link)
			if err != nil {
				t.Fatal(err)
			}
			if d := xrayconf.Describe(p.Exits[0].Transports[0].Xray); d != "NaïveProxy" {
				t.Errorf("described as %q", d)
			}
			inst, socks := start(t, p)
			roundTrip(t, socks)
			roundTrip(t, socks)

			for _, h := range srv.seen() {
				if (h.Get("Padding") != "") != tc.wantPadding {
					t.Errorf("Padding header %q, want it %v", h.Get("Padding"), tc.wantPadding)
				}
				if h.Get("X-Test") != "yes" {
					t.Errorf("extra header not sent: %v", h)
				}
				if ua, ok := h["User-Agent"]; ok {
					t.Errorf("User-Agent sent: %q", ua)
				}
			}
			all, _ := xrayconf.Candidates(p)
			if up, down := meter.Traffic(inst, all); up < 1<<20 || down < 1<<20 {
				t.Errorf("traffic not counted: up %d, down %d", up, down)
			}
		})
	}
}

// TestNaiveOneConnection checks that the streams share one connection to the
// server, as Chrome's would.
func TestNaiveOneConnection(t *testing.T) {
	srv := newServer(t, false)
	p, err := profile.FromVLESS(fmt.Sprintf("naive+https://naive:secret@127.0.0.1:%d?sni=naive.test&pinSHA256=%s", srv.port, srv.pin))
	if err != nil {
		t.Fatal(err)
	}
	_, socks := start(t, p)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "ok") }))
	defer s.Close()
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// A new client each time: each request is a new CONNECT.
			resp, err := client(socks).Get(viaName(s))
			if err != nil {
				t.Error(err)
				return
			}
			resp.Body.Close()
		}()
	}
	wg.Wait()
	if n := len(srv.seen()); n != 10 {
		t.Errorf("%d CONNECT requests, want 10", n)
	}
	if n := len(established(t, srv.port)); n != 1 {
		t.Errorf("%d connections to the server, want 1", n)
	}
}

// established lists the open TCP connections to the local port.
func established(t *testing.T, port int) []string {
	t.Helper()
	b, err := os.ReadFile("/proc/net/tcp")
	if err != nil {
		t.Skip("no /proc/net/tcp")
	}
	var out []string
	for _, line := range strings.Split(string(b), "\n")[1:] {
		f := strings.Fields(line)
		if len(f) > 3 && f[3] == "01" && strings.HasSuffix(f[2], fmt.Sprintf(":%04X", port)) {
			out = append(out, f[1])
		}
	}
	return out
}

func TestNaiveRefused(t *testing.T) {
	srv := newServer(t, false)
	for name, link := range map[string]string{
		"wrong password": fmt.Sprintf("naive+https://naive:wrong@127.0.0.1:%d?sni=naive.test&pinSHA256=%s", srv.port, srv.pin),
		"wrong pin":      fmt.Sprintf("naive+https://naive:secret@127.0.0.1:%d?sni=naive.test&pinSHA256=%s", srv.port, strings.Repeat("ab", 32)),
		"unknown cert":   fmt.Sprintf("naive+https://naive:secret@127.0.0.1:%d?sni=naive.test", srv.port),
	} {
		t.Run(name, func(t *testing.T) {
			p, err := profile.FromVLESS(link)
			if err != nil {
				t.Fatal(err)
			}
			_, socks := start(t, p)
			s, _ := site(t, 1)
			if resp, err := client(socks).Get(viaName(s)); err == nil {
				resp.Body.Close()
				t.Fatal("went through")
			}
		})
	}
}

// TestNaiveInsecureNotPinned: insecure=1 is pinned when the profile is
// imported; a transport that still asks for it is refused.
func TestNaiveInsecureNotPinned(t *testing.T) {
	p, err := profile.FromVLESS("naive+https://naive:secret@127.0.0.1:1?insecure=1")
	if err != nil {
		t.Fatal(err)
	}
	ob := xrayconf.Carried(p, "naive")[0]
	if err := naive.Attach(nil, ob.Tag, ob.Xray); err == nil || !strings.Contains(err.Error(), "не закреплён") {
		t.Fatalf("got %v", err)
	}
}

// TestCaddy goes through a real NaïveProxy server: Caddy built with klzgrad's
// forwardproxy, which TIGERSB_NAIVE_CADDY names. Without it the test is
// skipped.
func TestCaddy(t *testing.T) {
	bin := os.Getenv("TIGERSB_NAIVE_CADDY")
	if bin == "" {
		t.Skip("TIGERSB_NAIVE_CADDY is not set")
	}
	crt, pin := cert(t)
	dir := t.TempDir()
	key, _ := x509.MarshalPKCS8PrivateKey(crt.PrivateKey)
	os.WriteFile(filepath.Join(dir, "cert.pem"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: crt.Certificate[0]}), 0o600)
	os.WriteFile(filepath.Join(dir, "key.pem"), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}), 0o600)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	caddyfile := fmt.Sprintf(`{
	admin off
	auto_https disable_redirects
	order forward_proxy before respond
}
:%d {
	bind 127.0.0.1
	tls %s %s
	forward_proxy {
		basic_auth naive secret
		hide_ip
		hide_via
		probe_resistance
		acl {
			allow 127.0.0.1
		}
	}
	respond "обычный сайт"
}
`, port, filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem"))
	os.WriteFile(filepath.Join(dir, "Caddyfile"), []byte(caddyfile), 0o600)
	cmd := exec.Command(bin, "run", "--config", filepath.Join(dir, "Caddyfile"), "--adapter", "caddyfile")
	var log bytes.Buffer
	cmd.Stdout, cmd.Stderr = &log, &log
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Wait()
		if t.Failed() {
			t.Log(log.String())
		}
	})
	for i := 0; ; i++ {
		c, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			c.Close()
			break
		}
		if i == 100 {
			t.Fatal("Caddy did not start")
		}
		time.Sleep(50 * time.Millisecond)
	}

	// The site is what a prober sees without the password.
	h2 := &http.Client{Transport: &http2.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true, ServerName: "naive.test"}}}
	resp, err := h2.Get(fmt.Sprintf("https://127.0.0.1:%d/", port))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != "обычный сайт" {
		t.Errorf("prober sees %q", body)
	}

	for _, padding := range []string{"", "&padding=false"} {
		p, err := profile.FromVLESS(fmt.Sprintf("naive+https://naive:secret@127.0.0.1:%d?sni=naive.test&pinSHA256=%s%s", port, pin, padding))
		if err != nil {
			t.Fatal(err)
		}
		_, socks := start(t, p)
		roundTrip(t, socks)
	}
	p, _ := profile.FromVLESS(fmt.Sprintf("naive+https://naive:wrong@127.0.0.1:%d?sni=naive.test&pinSHA256=%s", port, pin))
	_, socks := start(t, p)
	s, _ := site(t, 1)
	if resp, err := client(socks).Get(viaName(s)); err == nil {
		resp.Body.Close()
		t.Fatal("went through with a wrong password")
	}
}
