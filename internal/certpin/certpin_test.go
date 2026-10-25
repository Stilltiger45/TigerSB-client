package certpin_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Stilltiger45/TigerSB-client/internal/certpin"
	"github.com/Stilltiger45/TigerSB-client/internal/engine"
	"github.com/Stilltiger45/TigerSB-client/internal/xrayconf"
	"github.com/Stilltiger45/TigerSB-client/pkg/profile"
)

// selfSigned makes a certificate for hy.example.org, as a Hysteria server
// without a real domain would have.
func selfSigned(t *testing.T) (certPEM, keyPEM string, sum string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "hy.example.org"},
		DNSNames:     []string{"hy.example.org"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	kb, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	s := sha256.Sum256(der)
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb})),
		hex.EncodeToString(s[:])
}

func freePort(t *testing.T, network string) int {
	t.Helper()
	if network == "udp" {
		c, err := net.ListenPacket("udp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		return c.LocalAddr().(*net.UDPAddr).Port
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// hysteriaServer runs Xray as a Hysteria 2 server with the password "pw" and
// returns its UDP port.
func hysteriaServer(t *testing.T, certPEM, keyPEM, obfs string) int {
	t.Helper()
	port := freePort(t, "udp")
	stream := map[string]any{
		"network":  "hysteria",
		"security": "tls",
		"tlsSettings": map[string]any{
			"alpn": []string{"h3"},
			"certificates": []any{map[string]any{
				"certificate": strings.Split(strings.TrimSpace(certPEM), "\n"),
				"key":         strings.Split(strings.TrimSpace(keyPEM), "\n"),
			}},
		},
		"hysteriaSettings": map[string]any{"version": 2},
	}
	if obfs != "" {
		stream["finalmask"] = map[string]any{"udp": []any{map[string]any{"type": "salamander", "settings": map[string]any{"password": obfs}}}}
	}
	cfg, _ := json.Marshal(map[string]any{
		"log": map[string]any{"loglevel": "none"},
		"inbounds": []any{map[string]any{
			"tag": "hy-in", "protocol": "hysteria", "listen": "127.0.0.1", "port": port,
			"settings":       map[string]any{"version": 2, "clients": []any{map[string]any{"auth": "pw"}}},
			"streamSettings": stream,
		}},
		"outbounds": []any{map[string]any{"protocol": "freedom"}},
	})
	inst, err := engine.Start(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { inst.Close() })
	return port
}

// throughVPN starts the client's own config for the profile with a SOCKS
// port and fetches url through it.
func throughVPN(t *testing.T, p *profile.Profile, target string) string {
	t.Helper()
	port := freePort(t, "tcp")
	cfg, err := xrayconf.Build(p, xrayconf.Options{
		Mode:     xrayconf.ModeAll,
		LogLevel: "none",
		Inbounds: []xrayconf.Inbound{{Tag: "socks-in", Protocol: "socks", Listen: "127.0.0.1", Port: port}},
	})
	if err != nil {
		t.Fatal(err)
	}
	inst, err := engine.Start(cfg)
	if err != nil {
		t.Fatalf("%v\n%s", err, cfg)
	}
	defer inst.Close()
	proxy, _ := url.Parse(fmt.Sprintf("socks5://127.0.0.1:%d", port))
	c := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxy)}, Timeout: 10 * time.Second}
	resp, err := c.Get(target)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}

func TestHysteria2SelfSignedIsPinned(t *testing.T) {
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "через hysteria") }))
	defer site.Close()
	certPEM, keyPEM, sum := selfSigned(t)

	for _, obfs := range []string{"", "mask"} {
		t.Run("obfs="+obfs, func(t *testing.T) {
			port := hysteriaServer(t, certPEM, keyPEM, obfs)
			link := fmt.Sprintf("hysteria2://pw@127.0.0.1:%d/?sni=hy.example.org&insecure=1", port)
			if obfs != "" {
				link += "&obfs=salamander&obfs-password=" + obfs
			}
			p, err := profile.FromVLESS(link)
			if err != nil {
				t.Fatal(err)
			}
			if dropped, err := certpin.Profile(context.Background(), p); err != nil || dropped != 0 {
				t.Fatalf("pin: dropped %d, %v", dropped, err)
			}
			xray := string(p.Exits[0].Transports[0].Xray)
			if !strings.Contains(xray, sum) || strings.Contains(xray, "allowInsecure") {
				t.Fatalf("not pinned: %s", xray)
			}
			if got := throughVPN(t, p, site.URL); got != "через hysteria" {
				t.Errorf("got %q", got)
			}
		})
	}
}

func TestTLSSelfSignedIsPinned(t *testing.T) {
	srv := httptest.NewTLSServer(http.NotFoundHandler())
	defer srv.Close()
	addr := strings.TrimPrefix(srv.URL, "https://")
	s := sha256.Sum256(srv.Certificate().Raw)
	want := hex.EncodeToString(s[:])

	p, _, err := profile.FromLinks("x", []string{
		"trojan://secret@" + addr + "?insecure=1#self-signed",
		"trojan://secret@127.0.0.1:1?insecure=1#down",
		"trojan://secret@tr.example.org:443#normal",
	})
	if err != nil {
		t.Fatal(err)
	}
	certpin.Timeout = 2 * time.Second
	dropped, err := certpin.Profile(context.Background(), p)
	if err != nil || dropped != 1 {
		t.Fatalf("dropped %d, %v", dropped, err)
	}
	ts := p.Exits[0].Transports
	if len(ts) != 2 || ts[0].Name != "self-signed" || ts[1].Name != "normal" {
		t.Fatalf("transports %+v", ts)
	}
	if !strings.Contains(string(ts[0].Xray), want) || strings.Contains(string(ts[0].Xray), "allowInsecure") {
		t.Errorf("not pinned: %s", ts[0].Xray)
	}
	// Real Xray accepts the pinned outbound.
	if _, err := xrayconf.Build(p, xrayconf.Options{Mode: xrayconf.ModeAll}); err != nil {
		t.Fatal(err)
	}

	// A single server that does not answer is an error.
	p, _ = profile.FromVLESS("trojan://secret@127.0.0.1:1?insecure=1")
	if _, err := certpin.Profile(context.Background(), p); err == nil {
		t.Error("unreachable server pinned")
	}
}
