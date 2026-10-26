package netauto

import (
	"context"
	"testing"
)

func TestMTU(t *testing.T) {
	for _, c := range []struct {
		t          Transport
		link, path int
		want       int
	}{
		{Wifi, 0, 0, 1480},
		{Wifi, 1500, 1500, 1480},
		{Wifi, 0, 1480, 1480},
		{Wifi, 0, 1460, 1460},
		{Wifi, 1492, 0, 1480}, // PPPoE
		{Cellular, 0, 0, 1460},
		{Cellular, 1500, 0, 1460},
		{Cellular, 1500, 1500, 1480},
		{Cellular, 0, 1280, 1280},
		{Cellular, 1358, 0, 1280},
		{Wifi, 9000, 0, 1480},
		{Ethernet, 576, 0, 1480},
		{"", 0, 0, 1480},
	} {
		if got := MTU(c.t, c.link, c.path); got != c.want {
			t.Errorf("MTU(%q, %d, %d) = %d, want %d", c.t, c.link, c.path, got, c.want)
		}
	}
	if MSS(1500) != 0 || MSS(0) != 0 || MSS(1460) != 1420 {
		t.Errorf("MSS %d %d %d", MSS(1500), MSS(0), MSS(1460))
	}
}

// TestPathMTULoopback measures the loopback path, which carries any of Sizes.
// It needs ICMP sockets, which Android gives every app; elsewhere the system
// may not, and then the path is unknown.
func TestPathMTULoopback(t *testing.T) {
	got := PathMTU(context.Background(), "127.0.0.1")
	if got == 0 {
		t.Skip("no ICMP socket here (net.ipv4.ping_group_range)")
	}
	if got != 1500 {
		t.Errorf("loopback path %d, want 1500", got)
	}
}

func TestEchoRequestSize(t *testing.T) {
	b := echoRequest(7, 1460)
	if len(b) != 1440 || b[0] != 8 || checksum(b) != 0 {
		t.Errorf("echo of %d bytes, type %d, checksum %#x", len(b), b[0], checksum(b))
	}
}
