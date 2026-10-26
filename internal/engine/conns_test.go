package engine_test

import (
	"context"
	"net"
	"testing"

	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/transport/internet"

	"github.com/Stilltiger45/TigerSB-client/internal/engine"
)

// listen accepts connections on the loopback and keeps them open.
func listen(t *testing.T) xnet.Destination {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			t.Cleanup(func() { c.Close() })
		}
	}()
	port := ln.Addr().(*net.TCPAddr).Port
	return xnet.TCPDestination(xnet.LocalHostIP, xnet.Port(port))
}

// TestCloseConnections checks that what the cores dial can be closed at once,
// as the app does when the phone changes networks.
func TestCloseConnections(t *testing.T) {
	dest := listen(t)
	engine.CloseConnections()
	var conns []net.Conn
	for range 3 {
		c, err := internet.DialSystem(context.Background(), dest, nil)
		if err != nil {
			t.Fatal(err)
		}
		conns = append(conns, c)
	}
	conns[0].Close()
	if n := engine.CloseConnections(); n != 2 {
		t.Fatalf("closed %d connections, want the 2 still open", n)
	}
	for _, c := range conns {
		if _, err := c.Write([]byte("x")); err == nil {
			t.Fatal("a connection is still open")
		}
	}
	if n := engine.CloseConnections(); n != 0 {
		t.Fatalf("closed %d connections again", n)
	}
}

// TestCloseUDP checks UDP, which Hysteria 2 and TUIC run over.
func TestCloseUDP(t *testing.T) {
	engine.CloseConnections()
	c, err := internet.DialSystem(context.Background(), xnet.UDPDestination(xnet.LocalHostIP, 9), nil)
	if err != nil {
		t.Fatal(err)
	}
	if n := engine.CloseConnections(); n != 1 {
		t.Fatalf("closed %d connections, want 1", n)
	}
	if _, err := c.Write([]byte("x")); err == nil {
		t.Fatal("the UDP connection is still open")
	}
}
