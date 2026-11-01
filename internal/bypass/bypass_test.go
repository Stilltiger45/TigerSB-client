package bypass

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"
)

func TestUnsetDialsAsSystem(t *testing.T) {
	Set(0, nil)
	t.Cleanup(func() { Set(0, nil) })
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	c, err := Dialer(time.Second).Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	u, err := ListenUDP("udp", nil)
	if err != nil {
		t.Fatal(err)
	}
	u.Close()
}

// The resolver asks the uplink's DNS servers, whatever server the system
// configuration names.
func TestResolverAsksUplinkDNS(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	asked := make(chan struct{}, 1)
	go func() {
		b := make([]byte, 512)
		if _, _, err := pc.ReadFrom(b); err == nil {
			asked <- struct{}{}
		}
	}()
	port := pc.LocalAddr().(*net.UDPAddr).Port
	u := &uplink{index: 1, dns: []string{net.JoinHostPort("127.0.0.1", itoa(port))}}
	current.Store(u)
	t.Cleanup(func() { Set(0, nil) })
	// Index 1 is the loopback interface on Linux, so binding to it is harmless.
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	c, err := Resolver.Dial(ctx, "udp", "192.0.2.1:53")
	if err != nil {
		t.Fatal(err)
	}
	c.Write([]byte("q"))
	c.Close()
	select {
	case <-asked:
	case <-time.After(time.Second):
		t.Fatal("the uplink's DNS server was not asked")
	}
}

func itoa(n int) string { return strconv.Itoa(n) }
