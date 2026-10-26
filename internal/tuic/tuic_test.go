package tuic_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"errors"
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

	"github.com/gofrs/uuid/v5"
	"github.com/sagernet/sing-quic/tuic"
	sbuf "github.com/sagernet/sing/common/buf"
	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	aTLS "github.com/sagernet/sing/common/tls"
	"github.com/xtls/xray-core/core"

	"github.com/Stilltiger45/TigerSB-client/internal/certpin"
	"github.com/Stilltiger45/TigerSB-client/internal/engine"
	"github.com/Stilltiger45/TigerSB-client/internal/meter"
	tigertuic "github.com/Stilltiger45/TigerSB-client/internal/tuic"
	"github.com/Stilltiger45/TigerSB-client/internal/xrayconf"
	"github.com/Stilltiger45/TigerSB-client/pkg/profile"
)

const testUUID = "b831381d-6324-4d53-ad4f-8cda48b30811"

// selfSigned makes a certificate for tuic.example.org, as a TUIC server
// without a real domain would have.
func selfSigned(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "tuic.example.org"},
		DNSNames:     []string{"tuic.example.org"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// serverTLS hands a standard TLS config to the sing-quic server.
type serverTLS struct{ c *tls.Config }

func (s *serverTLS) ServerName() string                 { return "" }
func (s *serverTLS) SetServerName(string)               {}
func (s *serverTLS) NextProtos() []string               { return s.c.NextProtos }
func (s *serverTLS) SetNextProtos(p []string)           { s.c.NextProtos = p }
func (s *serverTLS) HandshakeTimeout() time.Duration    { return 0 }
func (s *serverTLS) SetHandshakeTimeout(time.Duration)  {}
func (s *serverTLS) STDConfig() (*tls.Config, error)    { return s.c, nil }
func (s *serverTLS) Clone() aTLS.Config                 { return &serverTLS{s.c.Clone()} }
func (s *serverTLS) Client(net.Conn) (aTLS.Conn, error) { return nil, errors.ErrUnsupported }
func (s *serverTLS) Start() error                       { return nil }
func (s *serverTLS) Close() error                       { return nil }
func (s *serverTLS) Server(net.Conn) (aTLS.Conn, error) { return nil, errors.ErrUnsupported }

// exit is what the test TUIC server does with traffic: TCP goes to its
// destination, UDP packets come back to the sender (an echo).
type exit struct{}

func done(c io.Closer, onClose N.CloseHandlerFunc) {
	c.Close()
	if onClose != nil {
		onClose(nil)
	}
}

func (exit) NewConnectionEx(ctx context.Context, conn net.Conn, _, dst M.Socksaddr, onClose N.CloseHandlerFunc) {
	defer done(conn, onClose)
	up, err := net.Dial("tcp", dst.String())
	if err != nil {
		return
	}
	defer up.Close()
	go io.Copy(up, conn)
	io.Copy(conn, up)
}

func (exit) NewPacketConnectionEx(ctx context.Context, conn N.PacketConn, _, _ M.Socksaddr, onClose N.CloseHandlerFunc) {
	defer done(conn, onClose)
	for {
		b := sbuf.NewPacket()
		from, err := conn.ReadPacket(b)
		if err != nil {
			b.Release()
			return
		}
		if conn.WritePacket(b, from) != nil {
			return
		}
	}
}

// tuicServer runs a TUIC v5 server for testUUID with the password "pw" and
// returns its UDP port.
func tuicServer(t *testing.T) int {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	svc, err := tuic.NewService[int](tuic.ServiceOptions{
		Context:    context.Background(),
		Logger:     logger.NOP(),
		TLSConfig:  &serverTLS{&tls.Config{Certificates: []tls.Certificate{selfSigned(t)}, NextProtos: []string{"h3"}}},
		Handler:    exit{},
		UDPTimeout: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	svc.UpdateUsers([]int{1}, [][16]byte{uuid.Must(uuid.FromString(testUUID))}, []string{"pw"})
	if err := svc.Start(pc); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { svc.Close(); pc.Close() })
	return pc.LocalAddr().(*net.UDPAddr).Port
}

// freePort finds a port that is free for both TCP and UDP: the SOCKS inbound
// listens on both, and Windows keeps some UDP ports reserved even when the
// same TCP port is free ("forbidden by its access permissions").
func freePort(t *testing.T) int {
	t.Helper()
	for range 50 {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := l.Addr().(*net.TCPAddr).Port
		c, err := net.ListenPacket("udp", fmt.Sprintf("127.0.0.1:%d", port))
		l.Close()
		if err == nil {
			c.Close()
			return port
		}
	}
	t.Fatal("no port free for both TCP and UDP")
	return 0
}

// start runs the client's own config for the profile, with a SOCKS port for
// TCP and a UDP port that forwards to 1.2.3.4:7777.
func start(t *testing.T, p *profile.Profile) (inst *core.Instance, socks, udp int) {
	t.Helper()
	socks, udp = freePort(t), freePort(t)
	cfg, err := xrayconf.Build(p, xrayconf.Options{
		Mode:     xrayconf.ModeAll,
		LogLevel: "none",
		Inbounds: []xrayconf.Inbound{{Tag: "socks-in", Protocol: "socks", Listen: "127.0.0.1", Port: socks}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(cfg), `"tuic"`) {
		t.Fatalf("TUIC outbound left in the Xray config: %s", cfg)
	}
	var c map[string]any
	if err := json.Unmarshal(cfg, &c); err != nil {
		t.Fatal(err)
	}
	c["inbounds"] = append(c["inbounds"].([]any), map[string]any{
		"tag": "udp-in", "protocol": "dokodemo-door", "listen": "127.0.0.1", "port": udp,
		"settings": map[string]any{"address": "1.2.3.4", "port": 7777, "network": "udp"},
	})
	cfg, _ = json.Marshal(c)
	inst, err = engine.Start(cfg, tigertuic.Hooks(p)...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { inst.Close() })
	return inst, socks, udp
}

func get(t *testing.T, socks int, target string) (string, error) {
	t.Helper()
	proxy, _ := url.Parse(fmt.Sprintf("socks5://127.0.0.1:%d", socks))
	c := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxy)}, Timeout: 5 * time.Second}
	resp, err := c.Get(target)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	return string(b), err
}

func TestTUICSelfSignedServer(t *testing.T) {
	for _, relay := range []string{"native", "quic"} {
		t.Run(relay, func(t *testing.T) { testSelfSigned(t, relay) })
	}
}

func testSelfSigned(t *testing.T, relay string) {
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "через tuic") }))
	defer site.Close()
	port := tuicServer(t)

	link := fmt.Sprintf("tuic://%s:pw@127.0.0.1:%d?sni=tuic.example.org&alpn=h3&congestion_control=bbr&udp_relay_mode=%s&allow_insecure=1#TUIC", testUUID, port, relay)
	p, err := profile.FromVLESS(link)
	if err != nil {
		t.Fatal(err)
	}
	if dropped, err := certpin.Profile(context.Background(), p); err != nil || dropped != 0 {
		t.Fatalf("pin: dropped %d, %v", dropped, err)
	}
	if x := string(p.Exits[0].Transports[0].Xray); !strings.Contains(x, "pinnedPeerCertSha256") || strings.Contains(x, "allowInsecure") {
		t.Fatalf("not pinned: %s", x)
	}
	inst, socks, udp := start(t, p)

	got, err := get(t, socks, site.URL)
	if err != nil || got != "через tuic" {
		t.Fatalf("TCP: got %q, %v", got, err)
	}

	c, err := net.Dial("udp", fmt.Sprintf("127.0.0.1:%d", udp))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 16)
	n, err := c.Read(b)
	if err != nil || string(b[:n]) != "ping" {
		t.Fatalf("UDP: got %q, %v", b[:n], err)
	}

	all, _ := xrayconf.Candidates(p)
	if up, down := meter.Traffic(inst, all); up == 0 || down == 0 {
		t.Errorf("traffic not counted: up %d, down %d", up, down)
	}
}

func TestTUICWrongCertificateRefused(t *testing.T) {
	port := tuicServer(t)
	link := fmt.Sprintf("tuic://%s:pw@127.0.0.1:%d?sni=tuic.example.org&pcs=%s", testUUID, port, strings.Repeat("ab", 32))
	p, err := profile.FromVLESS(link)
	if err != nil {
		t.Fatal(err)
	}
	_, socks, _ := start(t, p)
	if got, err := get(t, socks, "http://example.com/"); err == nil {
		t.Fatalf("went through a server with another certificate: %q", got)
	}
}

func TestTUICUnpinnedInsecureRefused(t *testing.T) {
	p, err := profile.FromVLESS(fmt.Sprintf("tuic://%s:pw@127.0.0.1:1?allow_insecure=1", testUUID))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := xrayconf.Build(p, xrayconf.Options{Mode: xrayconf.ModeAll})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Start(cfg, tigertuic.Hooks(p)...); err == nil {
		t.Fatal("started without checking the certificate")
	}
}

// TestTUICAfterNetworkChange checks that a change of network closes TUIC's
// connection too and that the next request opens a new one.
func TestTUICAfterNetworkChange(t *testing.T) {
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "через tuic") }))
	defer site.Close()
	port := tuicServer(t)
	p, err := profile.FromVLESS(fmt.Sprintf("tuic://%s:pw@127.0.0.1:%d?sni=tuic.example.org&alpn=h3&allow_insecure=1#TUIC", testUUID, port))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := certpin.Profile(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	_, socks, _ := start(t, p)
	if got, err := get(t, socks, site.URL); err != nil || got != "через tuic" {
		t.Fatalf("before: got %q, %v", got, err)
	}
	if n := engine.CloseConnections(); n == 0 {
		t.Fatal("TUIC's connection was not closed")
	}
	var got string
	for range 3 {
		if got, err = get(t, socks, site.URL); err == nil {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if got != "через tuic" {
		t.Fatalf("after: got %q, %v", got, err)
	}
}
