package main

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/features/routing"
	"github.com/xtls/xray-core/transport/internet/tagged"

	"github.com/Stilltiger45/TigerSB-client/internal/certpin"
	"github.com/Stilltiger45/TigerSB-client/internal/engine"
	"github.com/Stilltiger45/TigerSB-client/internal/steady"
	"github.com/Stilltiger45/TigerSB-client/internal/tuic"
	"github.com/Stilltiger45/TigerSB-client/internal/xrayconf"
	"github.com/Stilltiger45/TigerSB-client/pkg/profile"
	"github.com/Stilltiger45/TigerSB-client/scripts/soak/load"
)

// freePort returns a port free on network: "tcp", "udp", or "tcp+udp" for a
// SOCKS inbound, which takes the same port for both. The port is free when
// it is picked; Windows often hands a just-freed TCP port's number to a UDP
// socket, so "tcp+udp" checks both at once.
func freePort(t *testing.T, network string) int {
	t.Helper()
	if network == "udp" {
		pc, err := net.ListenPacket("udp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer pc.Close()
		return pc.LocalAddr().(*net.UDPAddr).Port
	}
	for range 50 {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := l.Addr().(*net.TCPAddr).Port
		if network == "tcp" {
			l.Close()
			return port
		}
		pc, err := net.ListenPacket("udp", fmt.Sprintf("127.0.0.1:%d", port))
		l.Close()
		if err == nil {
			pc.Close()
			return port
		}
	}
	t.Fatal("no port free on both TCP and UDP")
	return 0
}

// client runs the app's core for the profile in mode. TUIC sends noise
// before QUIC only with noise, as the app always does: without it, the test
// shows the DPI cutting QUIC.
func client(t *testing.T, p *profile.Profile, mode xrayconf.Mode, noise bool) *core.Instance {
	t.Helper()
	auto := mode == xrayconf.ModeAuto
	cfg, err := xrayconf.Build(p, xrayconf.Options{
		Mode:     mode,
		Inbounds: []xrayconf.Inbound{{Tag: "socks-in", Protocol: "socks", Listen: "127.0.0.1", Port: freePort(t, "tcp+udp")}},
		Evasion:  auto,
		LogLevel: "none",
	})
	if err != nil {
		t.Fatal(err)
	}
	var n []xrayconf.NoisePacket
	if noise {
		n = xrayconf.QUICNoise
	}
	inst, err := engine.Start(cfg, append(tuic.Hooks(p, n), steady.Hooks(p, auto, 3)...)...)
	if err != nil {
		t.Fatal(err)
	}
	return inst
}

// through dials the bulk server through the outbound tag of inst.
func through(inst *core.Instance, tag string) load.Dial {
	return func(ctx context.Context) (net.Conn, error) {
		d, _ := inst.GetFeature(routing.DispatcherType()).(routing.Dispatcher)
		ctx = context.WithValue(ctx, core.XrayKey(1), inst)
		return tagged.Dialer(ctx, d, xnet.TCPDestination(xnet.ParseAddress(BulkIP), BulkPort), tag)
	}
}

// TestDPILevels runs the soak servers behind each level of the stand-in for
// DPI and checks what gets through: noise before QUIC beats the weak DPI, a
// split connection start beats both, and the strong DPI cuts QUIC whatever
// comes first.
func TestDPILevels(t *testing.T) {
	if testing.Short() {
		t.Skip("starts three servers and a dozen cores")
	}
	c := Config{
		VLESS:    freePort(t, "tcp"),
		Hysteria: freePort(t, "udp"),
		TUIC:     freePort(t, "udp"),
		ID:       "5783a3e7-e373-51cd-8642-c83782b807c5",
		Password: "pw",
	}
	s, err := Start(c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.xray.Close() })

	p, _, err := profile.FromLinks("mix", []string{
		fmt.Sprintf("vless://%s@127.0.0.1:%d?security=none#vless", c.ID, c.VLESS),
		fmt.Sprintf("hysteria2://%s@127.0.0.1:%d/?sni=soak.example.org&insecure=1#hy", c.Password, c.Hysteria),
		fmt.Sprintf("tuic://%s:%s@127.0.0.1:%d?sni=soak.example.org&alpn=h3&congestion_control=bbr&allow_insecure=1#tuic", c.ID, c.Password, c.TUIC),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if dropped, err := certpin.Profile(ctx, p); err != nil || dropped != 0 {
		t.Fatalf("pinning: %d dropped, %v", dropped, err)
	}
	all, _ := xrayconf.Candidates(p)
	vless, hy, tu := all[0], all[1], all[2]
	if udp := xrayconf.UDPTransports(p); len(udp) != 2 || udp[0] != hy || udp[1] != tu {
		t.Fatalf("transports %v, over UDP %v", all, udp)
	}
	split := xrayconf.VariantTag(3, vless)

	type check struct {
		mode  xrayconf.Mode
		noise bool
		open  map[string]bool
	}
	for _, tc := range []struct {
		level  Level
		checks []check
	}{
		{Off, []check{
			{xrayconf.ModeAuto, true, map[string]bool{vless: true, split: true, hy: true, tu: true}},
			{xrayconf.ModeAll, false, map[string]bool{tu: true}},
		}},
		{Weak, []check{
			{xrayconf.ModeAuto, true, map[string]bool{vless: false, split: true, hy: true, tu: true}},
			{xrayconf.ModeAll, false, map[string]bool{tu: false}},
		}},
		{Strong, []check{
			{xrayconf.ModeAuto, true, map[string]bool{vless: false, split: true, hy: false, tu: false}},
		}},
	} {
		s.DPI.SetLevel(tc.level)
		for _, c := range tc.checks {
			// The app closes its connections when the channel fails, so
			// that QUIC is dialed anew: Xray keeps one Hysteria 2 client
			// per server for the whole process.
			engine.CloseConnections()
			inst := client(t, p, c.mode, c.noise)
			for tag, ok := range c.open {
				ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
				_, err := load.Download(ctx, through(inst, tag), 256<<10)
				cancel()
				if (err == nil) != ok {
					t.Errorf("DPI %s, %s mode, noise %v, %s: download error %v, want it to open: %v", tc.level, c.mode, c.noise, tag, err, ok)
				}
			}
			inst.Close()
		}
	}
	if s.DPI.udpBlocked.Load() == 0 || s.DPI.tcpBlocked.Load() == 0 {
		t.Error("the DPI cut nothing")
	}
}

// TestStreamBothWays keeps a stream going through the bulk server's echo.
func TestStreamBothWays(t *testing.T) {
	b := &bulkServer{}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go b.serve(l)
	dial := func(ctx context.Context) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "tcp", l.Addr().String())
	}
	r := load.Stream(context.Background(), dial, 2*time.Second, 512<<10, time.Second, nil)
	if r.Down < 512<<10 || r.Up < r.Down || r.Reconnects != 0 || r.MaxGap > time.Second {
		t.Errorf("stream: %+v", r)
	}
	if d, err := load.Upload(context.Background(), dial, 3<<20); err != nil {
		t.Errorf("upload: %v", err)
	} else if _, err := load.Download(context.Background(), dial, 3<<20); err != nil {
		t.Errorf("download after %v: %v", d, err)
	}
}
