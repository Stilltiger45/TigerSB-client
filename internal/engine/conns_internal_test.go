package engine

import (
	"net"
	"runtime"
	"testing"
)

// TestTrackForgetsCollected checks that the list of connections does not
// grow with the ones Xray closed and dropped.
func TestTrackForgetsCollected(t *testing.T) {
	CloseConnections()
	for range 1000 {
		c, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
		if err != nil {
			t.Fatal(err)
		}
		Track(c)
		c.Close()
		if len(conns)%100 == 0 {
			runtime.GC()
		}
	}
	runtime.GC()
	connMu.Lock()
	n := len(conns)
	connMu.Unlock()
	if n > 300 {
		t.Fatalf("%d connections remembered after 1000 were closed and dropped", n)
	}
}
