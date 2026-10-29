package mieru_test

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	mcommon "github.com/enfein/mieru/v3/apis/common"
	"github.com/enfein/mieru/v3/apis/constant"
	"github.com/enfein/mieru/v3/apis/model"
	mserver "github.com/enfein/mieru/v3/apis/server"
	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/xtls/xray-core/core"
	"google.golang.org/protobuf/proto"

	"github.com/Stilltiger45/TigerSB-client/internal/engine"
	"github.com/Stilltiger45/TigerSB-client/internal/meter"
	"github.com/Stilltiger45/TigerSB-client/internal/mieru"
	"github.com/Stilltiger45/TigerSB-client/internal/xrayconf"
	"github.com/Stilltiger45/TigerSB-client/pkg/profile"
)

// mieruServer runs a mieru server for the user "baozi" with the password
// "manlianpenfen" on a free port of the protocol (TCP or UDP), as mita
// does: TCP goes to its destination, UDP packets come back to the sender (an
// echo). It returns the port.
func mieruServer(t *testing.T, protocol string) int {
	t.Helper()
	port := freePort(t)
	s := mserver.NewServer()
	err := s.Store(&mserver.ServerConfig{Config: &appctlpb.ServerConfig{
		PortBindings: []*appctlpb.PortBinding{{
			Port:     proto.Int32(int32(port)),
			Protocol: appctlpb.TransportProtocol(appctlpb.TransportProtocol_value[protocol]).Enum(),
		}},
		Users: []*appctlpb.User{{Name: proto.String("baozi"), Password: proto.String("manlianpenfen")}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Stop() })
	go func() {
		for {
			conn, req, err := s.Accept()
			if err != nil {
				return
			}
			go serve(conn, req)
		}
	}()
	return port
}

func serve(conn net.Conn, req *model.Request) {
	defer conn.Close()
	ok := &model.Response{Reply: constant.Socks5ReplySuccess, BindAddr: model.AddrSpec{IP: net.IPv4zero, Port: 1}}
	switch req.Command {
	case constant.Socks5ConnectCmd:
		up, err := net.Dial("tcp", req.DstAddr.String())
		if err != nil {
			return
		}
		defer up.Close()
		if ok.WriteToSocks5(conn) != nil {
			return
		}
		go io.Copy(up, conn)
		io.Copy(conn, up)
	case constant.Socks5UDPAssociateCmd:
		if ok.WriteToSocks5(conn) != nil {
			return
		}
		tunnel := mcommon.NewPacketOverStreamTunnel(conn)
		b := make([]byte, 65535)
		for {
			n, err := tunnel.Read(b)
			if err != nil {
				return
			}
			// The packet carries the socks5 UDP header with the destination:
			// sent back as it is, it comes from that destination.
			if _, err := tunnel.Write(b[:n]); err != nil {
				return
			}
		}
	}
}

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
	if strings.Contains(string(cfg), `"mieru"`) {
		t.Fatalf("mieru outbound left in the Xray config: %s", cfg)
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
	inst, err = engine.Start(cfg, mieru.Hooks(p)...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { inst.Close() })
	return inst, socks, udp
}

func get(socks int, target string) (string, error) {
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

// TestMieru goes through a mieru server over TCP and over UDP, with links
// as "mieru export config simple" writes them, and with mieru:// too.
func TestMieru(t *testing.T) {
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "через mieru") }))
	defer site.Close()
	for _, protocol := range []string{"TCP", "UDP"} {
		t.Run(protocol, func(t *testing.T) {
			port := mieruServer(t, protocol)
			links := []string{
				fmt.Sprintf("mierus://baozi:manlianpenfen@127.0.0.1?handshake-mode=HANDSHAKE_NO_WAIT&mtu=1400&multiplexing=MULTIPLEXING_HIGH&port=%d&profile=default&protocol=%s#Mieru", port, protocol),
				full(t, port, protocol),
			}
			for _, link := range links {
				p, err := profile.FromVLESS(link)
				if err != nil {
					t.Fatal(err)
				}
				if d := xrayconf.Describe(p.Exits[0].Transports[0].Xray); d != "Mieru · "+protocol {
					t.Errorf("described as %q", d)
				}
				inst, socks, udp := start(t, p)
				// By name: the address 127.0.0.1 would go direct.
				got, err := get(socks, strings.Replace(site.URL, "127.0.0.1", "localhost", 1))
				if err != nil || got != "через mieru" {
					t.Fatalf("%s: TCP: got %q, %v", link[:8], got, err)
				}
				echo(t, udp)
				all, _ := xrayconf.Candidates(p)
				if up, down := meter.Traffic(inst, all); up == 0 || down == 0 {
					t.Errorf("traffic not counted: up %d, down %d", up, down)
				}
			}
		})
	}
}

// full writes a mieru:// link, as "mieru export config" does.
func full(t *testing.T, port int, protocol string) string {
	t.Helper()
	c := &appctlpb.ClientConfig{
		ActiveProfile: proto.String("default"),
		Profiles: []*appctlpb.ClientProfile{{
			ProfileName: proto.String("default"),
			User:        &appctlpb.User{Name: proto.String("baozi"), Password: proto.String("manlianpenfen")},
			Servers: []*appctlpb.ServerEndpoint{{
				IpAddress: proto.String("127.0.0.1"),
				PortBindings: []*appctlpb.PortBinding{{
					Port:     proto.Int32(int32(port)),
					Protocol: appctlpb.TransportProtocol(appctlpb.TransportProtocol_value[protocol]).Enum(),
				}},
			}},
		}},
	}
	b, err := proto.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	return "mieru://" + base64.StdEncoding.EncodeToString(b)
}

func TestMieruWrongPassword(t *testing.T) {
	port := mieruServer(t, "TCP")
	p, err := profile.FromVLESS("mierus://baozi:wrong@127.0.0.1?profile=default&port=" + strconv.Itoa(port) + "&protocol=TCP")
	if err != nil {
		t.Fatal(err)
	}
	_, socks, _ := start(t, p)
	if got, err := get(socks, "http://example.com/"); err == nil {
		t.Fatalf("went through with a wrong password: %q", got)
	}
}
