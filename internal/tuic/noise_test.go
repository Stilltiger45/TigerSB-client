package tuic

import (
	"context"
	"net"
	"testing"
	"time"

	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"

	"github.com/Stilltiger45/TigerSB-client/internal/xrayconf"
)

// TestNoiseGoesFirst checks that a QUIC connection's socket sends the noise
// before anything of QUIC.
func TestNoiseGoesFirst(t *testing.T) {
	server, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	d := trackedDialer{N.SystemDialer, xrayconf.QUICNoise}
	c, err := d.DialContext(context.Background(), "udp", M.ParseSocksaddr(server.LocalAddr().String()))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.Write([]byte("quic"))

	server.SetReadDeadline(time.Now().Add(3 * time.Second))
	b := make([]byte, 2048)
	for i, n := range xrayconf.QUICNoise {
		size, _, err := server.ReadFrom(b)
		if err != nil {
			t.Fatal(err)
		}
		if size < n.Size[0] || size > n.Size[1] {
			t.Errorf("noise packet %d is %d bytes, want %d-%d", i, size, n.Size[0], n.Size[1])
		}
	}
	size, _, err := server.ReadFrom(b)
	if err != nil || string(b[:size]) != "quic" {
		t.Fatalf("after the noise got %q, %v", b[:size], err)
	}
}
