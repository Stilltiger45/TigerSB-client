package awg_test

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amnezia-vpn/amneziawg-go/v3/conn"
	"github.com/amnezia-vpn/amneziawg-go/v3/device"
	"github.com/amnezia-vpn/amneziawg-go/v3/tun/netstack"
	"github.com/xtls/xray-core/core"
	"golang.org/x/crypto/curve25519"

	"github.com/Stilltiger45/TigerSB-client/internal/awg"
	"github.com/Stilltiger45/TigerSB-client/internal/engine"
	"github.com/Stilltiger45/TigerSB-client/internal/meter"
	"github.com/Stilltiger45/TigerSB-client/internal/xrayconf"
	"github.com/Stilltiger45/TigerSB-client/pkg/profile"
)

// site is the address of the test site inside the server's tunnel: public,
// so that the client's routing sends it through the VPN.
const site = "1.2.3.4"

func keyPair(t *testing.T) (private, public []byte) {
	t.Helper()
	private = make([]byte, 32)
	rand.Read(private)
	private[0] &= 248
	private[31] = private[31]&127 | 64
	public, err := curve25519.X25519(private, curve25519.Basepoint)
	if err != nil {
		t.Fatal(err)
	}
	return private, public
}

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

// server runs an AmneziaWG server with the obfuscation keys (in the .conf
// spelling) on a free UDP port, for one client with the public key
// clientPub at 10.9.0.2. Inside its tunnel, http://1.2.3.4/ answers
// "через AWG" and 1.2.3.4:7777 echoes UDP. It returns the port and its
// public key.
func server(t *testing.T, clientPub []byte, keys map[string]string) (int, []byte) {
	return serverLog(t, clientPub, keys, nil)
}

// journal keeps what a device logs, for the message of a failed test.
type journal struct {
	mu    sync.Mutex
	lines []string
}

func (j *journal) logf(format string, args ...any) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.lines = append(j.lines, fmt.Sprintf(format, args...))
}

func (j *journal) String() string {
	j.mu.Lock()
	defer j.mu.Unlock()
	// One line: CI shows the first line of a failure only.
	return strings.Join(j.lines[max(0, len(j.lines)-25):], " | ")
}

func serverLog(t *testing.T, clientPub []byte, keys map[string]string, j *journal) (int, []byte) {
	t.Helper()
	tun, tnet, err := netstack.CreateNetTUN([]netip.Addr{netip.MustParseAddr("10.9.0.1"), netip.MustParseAddr(site)}, nil, 1420)
	if err != nil {
		t.Fatal(err)
	}
	logger := device.NewLogger(device.LogLevelError, "server: ")
	if j != nil {
		logger = &device.Logger{Verbosef: j.logf, Errorf: j.logf}
	}
	// The default bind: the standard one fails every read on Windows
	// ("wsarecvmsg: … invalid pointer"), and AmneziaWG servers there run
	// the default one too.
	dev := device.NewDevice(tun, conn.NewDefaultBind(), logger)
	t.Cleanup(dev.Close)
	priv, pub := keyPair(t)
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := pc.LocalAddr().(*net.UDPAddr).Port
	pc.Close()
	var b strings.Builder
	fmt.Fprintf(&b, "private_key=%s\nlisten_port=%d\n", hex.EncodeToString(priv), port)
	for k, v := range keys {
		name := map[string]string{
			"HeaderProtectionKey": "header_protection_key", "ContentPaddingAddition": "content_padding_addition",
			"DisableCookies": "disable_cookies", "RekeyAfterTime": "rekey_after_time",
		}[k]
		if name == "" {
			name = strings.ToLower(k)
		}
		if k == "HeaderProtectionKey" {
			raw, _ := base64.StdEncoding.DecodeString(v)
			v = hex.EncodeToString(raw)
		}
		fmt.Fprintf(&b, "%s=%s\n", name, v)
	}
	fmt.Fprintf(&b, "public_key=%s\nallowed_ip=10.9.0.2/32\n", hex.EncodeToString(clientPub))
	if err := dev.IpcSet(b.String()); err != nil {
		t.Fatal(err)
	}
	if err := dev.Up(); err != nil {
		t.Fatal(err)
	}
	l, err := tnet.ListenTCPAddrPort(netip.MustParseAddrPort(site + ":80"))
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/big" {
			w.Write(bytes.Repeat([]byte("x"), bigSize))
			return
		}
		fmt.Fprint(w, "через AWG")
	})}
	go srv.Serve(l)
	t.Cleanup(func() { srv.Close() })
	u, err := tnet.ListenUDPAddrPort(netip.MustParseAddrPort(site + ":7777"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { u.Close() })
	go func() {
		buf := make([]byte, 2048)
		for {
			n, from, err := u.ReadFrom(buf)
			if err != nil {
				return
			}
			u.WriteTo(buf[:n], from)
		}
	}()
	return port, pub
}

// conf writes the client's .conf, as the AmneziaWG apps export it.
func conf(clientPriv, serverPub []byte, port int, keys map[string]string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[Interface]\nPrivateKey = %s\nAddress = 10.9.0.2/32\nDNS = 1.1.1.1\nMTU = 1280\n", b64(clientPriv))
	for k, v := range keys {
		fmt.Fprintf(&b, "%s = %s\n", k, v)
	}
	fmt.Fprintf(&b, "\n[Peer]\nPublicKey = %s\nEndpoint = 127.0.0.1:%d\nAllowedIPs = 0.0.0.0/0, ::/0\nPersistentKeepalive = 25\n", b64(serverPub), port)
	return b.String()
}

// start runs the client's config for the profile with a SOCKS port, and a
// UDP port that forwards to 1.2.3.4:7777.
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
	if strings.Contains(string(cfg), `"wireguard"`) {
		t.Fatalf("WireGuard outbound left in the Xray config: %s", cfg)
	}
	var c map[string]any
	if err := json.Unmarshal(cfg, &c); err != nil {
		t.Fatal(err)
	}
	c["inbounds"] = append(c["inbounds"].([]any), map[string]any{
		"tag": "udp-in", "protocol": "dokodemo-door", "listen": "127.0.0.1", "port": udp,
		"settings": map[string]any{"address": site, "port": 7777, "network": "udp"},
	})
	cfg, _ = json.Marshal(c)
	inst, err = engine.Start(cfg, awg.Hooks(p)...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { inst.Close() })
	return inst, socks, udp
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func get(socks int, timeout time.Duration) (string, error) {
	proxy, _ := url.Parse(fmt.Sprintf("socks5://127.0.0.1:%d", socks))
	c := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxy)}, Timeout: timeout}
	resp, err := c.Get("http://" + site + "/")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	return string(b), err
}

func echo(t *testing.T, udp int) {
	t.Helper()
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
}

func hpk() string {
	k := make([]byte, 32)
	rand.Read(k)
	return b64(k)
}

var versions = []struct {
	name string
	keys map[string]string
}{
	{"WireGuard", nil},
	{"AmneziaWG", map[string]string{"Jc": "4", "Jmin": "40", "Jmax": "70", "S1": "52", "S2": "71", "H1": "1922474861", "H2": "1404438543", "H3": "1129425706", "H4": "1716093580"}},
	{"AmneziaWG 2", map[string]string{
		"Jc": "5", "Jmin": "61", "Jmax": "190", "S1": "88", "S2": "47", "S3": "29", "S4": "17",
		"H1": "234567-345678", "H2": "3456789-4567890", "H3": "56789012-67890123", "H4": "456789012-567890123",
		"I1": "<r 2><b 0x858000010002000000001c><rc 28><t>",
	}},
	{"AmneziaWG 3", map[string]string{
		"Jc": "3", "Jmin": "50", "Jmax": "100", "S1": "24", "S2": "32", "S3": "16", "S4": "12",
		"H1": "1", "H2": "2", "H3": "3", "H4": "4",
		"HeaderProtectionKey": hpk(), "ContentPaddingAddition": "32-128", "RekeyAfterTime": "100-120",
		"I1": "<b 0x16030100><r 32>",
	}},
}

// TestAmneziaWG goes through a WireGuard server and AmneziaWG 1, 2 and 3
// servers, over TCP and UDP, from a .conf as the AmneziaWG apps export it.
func TestAmneziaWG(t *testing.T) {
	for _, v := range versions {
		t.Run(v.name, func(t *testing.T) {
			cpriv, cpub := keyPair(t)
			var j, cj journal
			awg.SetVerbose(cj.logf)
			defer awg.SetVerbose(func(string, ...any) {})
			port, spub := serverLog(t, cpub, v.keys, &j)
			key, err := profile.ParseKey(conf(cpriv, spub, port, v.keys))
			if err != nil {
				t.Fatal(err)
			}
			p, err := profile.FromVLESS(key.Raw)
			if err != nil {
				t.Fatal(err)
			}
			if d := xrayconf.Describe(p.Exits[0].Transports[0].Xray); d != v.name {
				t.Errorf("described as %q", d)
			}
			inst, socks, udp := start(t, p)
			got, err := get(socks, 10*time.Second)
			if err != nil || got != "через AWG" {
				t.Fatalf("TCP: got %q, %v; server: %s || client: %s", got, err, &j, &cj)
			}
			echo(t, udp)
			all, _ := xrayconf.Candidates(p)
			if up, down := meter.Traffic(inst, all); up == 0 || down == 0 {
				t.Errorf("traffic not counted: up %d, down %d", up, down)
			}
			// A change of network closes the socket; the next connection
			// opens a new one and the server follows it.
			if engine.CloseConnections() == 0 {
				t.Error("the socket to the server is not tracked")
			}
			if got, err := get(socks, 10*time.Second); err != nil || got != "через AWG" {
				t.Fatalf("after a change of network: got %q, %v", got, err)
			}
			began := time.Now()
			if err := engine.Stop(inst); err != nil {
				t.Fatal(err)
			}
			if d := time.Since(began); d > time.Second {
				t.Errorf("stopping took %v", d)
			}
		})
	}
}

// TestAmneziaWGMismatch: a client whose headers differ from the server's
// does not get through, and stopping does not wait for it.
func TestAmneziaWGMismatch(t *testing.T) {
	keys := versions[2].keys
	cpriv, cpub := keyPair(t)
	port, spub := server(t, cpub, keys)
	other := map[string]string{}
	for k, v := range keys {
		other[k] = v
	}
	other["H1"] = "100-200"
	key, err := profile.ParseKey(conf(cpriv, spub, port, other))
	if err != nil {
		t.Fatal(err)
	}
	p, err := profile.FromVLESS(key.Raw)
	if err != nil {
		t.Fatal(err)
	}
	inst, socks, _ := start(t, p)
	if got, err := get(socks, 3*time.Second); err == nil {
		t.Fatalf("went through with other headers: %q", got)
	}
	began := time.Now()
	engine.Stop(inst)
	if d := time.Since(began); d > time.Second {
		t.Errorf("stopping took %v", d)
	}
}

// bigSize is the size of http://1.2.3.4/big.
const bigSize = 40 << 10

// freezer passes UDP between clients and the server on port like DPI that
// freezes a flow it does not know: after limit bytes from the server, a
// client's flow (its address and port) gets nothing more. It returns its
// own port.
func freezer(t *testing.T, port, limit int) int {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() })
	server := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port}
	var mu sync.Mutex
	flows := map[string]net.Conn{}
	go func() {
		buf := make([]byte, 65535)
		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			mu.Lock()
			up := flows[from.String()]
			if up == nil {
				if up, err = net.DialUDP("udp", nil, server); err != nil {
					mu.Unlock()
					continue
				}
				flows[from.String()] = up
				t.Cleanup(func() { up.Close() })
				go func() {
					back := make([]byte, 65535)
					sent := 0
					for {
						n, err := up.Read(back)
						if err != nil {
							return
						}
						if sent += n; sent <= limit {
							pc.WriteTo(back[:n], from)
						}
					}
				}()
			}
			mu.Unlock()
			up.Write(buf[:n])
		}
	}()
	return pc.LocalAddr().(*net.UDPAddr).Port
}

// TestAmneziaWGFrozenFlow: DPI freezes the client's UDP flow after 16 KB;
// the client takes a new socket, a new flow, and the download goes on.
func TestAmneziaWGFrozenFlow(t *testing.T) {
	keys := versions[3].keys
	cpriv, cpub := keyPair(t)
	port, spub := server(t, cpub, keys)
	key, err := profile.ParseKey(conf(cpriv, spub, freezer(t, port, 16<<10), keys))
	if err != nil {
		t.Fatal(err)
	}
	p, err := profile.FromVLESS(key.Raw)
	if err != nil {
		t.Fatal(err)
	}
	_, socks, _ := start(t, p)
	proxy, _ := url.Parse(fmt.Sprintf("socks5://127.0.0.1:%d", socks))
	c := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxy)}, Timeout: 60 * time.Second}
	began := time.Now()
	resp, err := c.Get("http://" + site + "/big")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	n, err := io.Copy(io.Discard, resp.Body)
	if err != nil || n != bigSize {
		t.Fatalf("got %d of %d bytes: %v", n, bigSize, err)
	}
	t.Logf("%d KB through a flow frozen after 16 KB in %v", bigSize>>10, time.Since(began).Round(time.Millisecond))
}
