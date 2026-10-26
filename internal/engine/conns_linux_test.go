package engine_test

import (
	"context"
	"net"
	"testing"

	"github.com/xtls/xray-core/transport/internet"
	"golang.org/x/sys/unix"

	"github.com/Stilltiger45/TigerSB-client/internal/engine"
)

func sockopt(t *testing.T, c net.Conn, opt int) int {
	t.Helper()
	raw, err := c.(*net.TCPConn).SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var v int
	raw.Control(func(fd uintptr) { v, err = unix.GetsockoptInt(int(fd), unix.IPPROTO_TCP, opt) })
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// TestLinkTunesConnections checks the LTE mode's options on what the cores
// dial, and that the panel's own options win.
func TestLinkTunesConnections(t *testing.T) {
	dest := listen(t)
	engine.SetLink(engine.Link{Mobile: true, MSS: 1200})
	t.Cleanup(func() { engine.SetLink(engine.Link{}) })

	c, err := internet.DialSystem(context.Background(), dest, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if v := sockopt(t, c, unix.TCP_USER_TIMEOUT); v != 30000 {
		t.Errorf("TCP_USER_TIMEOUT %d, want 30000", v)
	}
	if v := sockopt(t, c, unix.TCP_KEEPIDLE); v != 30 {
		t.Errorf("TCP_KEEPIDLE %d, want 30", v)
	}
	if v := sockopt(t, c, unix.TCP_KEEPINTVL); v != 10 {
		t.Errorf("TCP_KEEPINTVL %d, want 10", v)
	}
	if v := sockopt(t, c, unix.TCP_MAXSEG); v > 1200 {
		t.Errorf("TCP_MAXSEG %d, want at most 1200", v)
	}

	panel := &internet.SocketConfig{TcpUserTimeout: 5000, TcpKeepAliveIdle: 60}
	c2, err := internet.DialSystem(context.Background(), dest, panel)
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()
	if v := sockopt(t, c2, unix.TCP_USER_TIMEOUT); v != 5000 {
		t.Errorf("panel's TCP_USER_TIMEOUT: got %d, want 5000", v)
	}
	if v := sockopt(t, c2, unix.TCP_KEEPIDLE); v != 60 {
		t.Errorf("panel's TCP_KEEPIDLE: got %d, want 60", v)
	}

	engine.SetLink(engine.Link{})
	c3, err := internet.DialSystem(context.Background(), dest, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c3.Close()
	if v := sockopt(t, c3, unix.TCP_USER_TIMEOUT); v != 0 {
		t.Errorf("Wi-Fi: TCP_USER_TIMEOUT %d, want 0", v)
	}
}
