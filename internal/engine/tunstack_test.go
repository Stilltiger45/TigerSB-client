//go:build linux || windows

package engine

import (
	"context"
	"reflect"
	"testing"

	"github.com/xtls/xray-core/proxy/tun"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv6"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
)

// TestTunStackFields guards the unexported fields of Xray's TUN stack that
// RefuseIPv6 and Stop read: re-check on every Xray update.
func TestTunStackFields(t *testing.T) {
	dev := platformTun()
	s, err := tun.NewStack(context.Background(), tun.StackOptions{Tun: dev}, nil)
	if err != nil {
		t.Fatal(err)
	}
	f, ok := field(s, "stack")
	if !ok || f.Type() != reflect.TypeOf((*stack.Stack)(nil)) {
		t.Fatalf("the stack's gVisor stack is gone or changed: %v", ok)
	}
	if d := tunDevice(s); d != dev {
		t.Fatalf("the stack's device is gone or changed: %v", d)
	}
}

// TestRefuseIPv6 checks on a gVisor stack that IPv6 packets no longer reach
// the forwarder, which leaves them to the stack to refuse, and that IPv4
// ones still do.
func TestRefuseIPv6(t *testing.T) {
	s := stack.New(stack.Options{
		NetworkProtocols:   []stack.NetworkProtocolFactory{ipv4.NewProtocol, ipv6.NewProtocol},
		TransportProtocols: []stack.TransportProtocolFactory{tcp.NewProtocol, udp.NewProtocol},
	})
	defer s.Close()
	got := map[tcpip.TransportProtocolNumber]int{}
	for _, p := range []tcpip.TransportProtocolNumber{tcp.ProtocolNumber, udp.ProtocolNumber} {
		s.SetTransportProtocolHandler(p, func(stack.TransportEndpointID, *stack.PacketBuffer) bool {
			got[p]++
			return true
		})
	}
	if !refuseIPv6(s) {
		t.Fatal("the stack's handlers were not found")
	}
	v4 := stack.TransportEndpointID{LocalAddress: tcpip.AddrFrom4([4]byte{142, 250, 74, 46}), LocalPort: 443}
	v6 := stack.TransportEndpointID{LocalAddress: tcpip.AddrFrom16([16]byte{0x2a, 0x00, 0x14, 0x50}), LocalPort: 443}
	for _, p := range []tcpip.TransportProtocolNumber{tcp.ProtocolNumber, udp.ProtocolNumber} {
		h := defaultHandler(s, p)
		if h == nil {
			t.Fatalf("protocol %d: no handler", p)
		}
		if !h(v4, nil) {
			t.Errorf("protocol %d: IPv4 not taken", p)
		}
		if h(v6, nil) {
			t.Errorf("protocol %d: IPv6 taken", p)
		}
		if got[p] != 1 {
			t.Errorf("protocol %d: the forwarder saw %d packets, want 1 (IPv4 only)", p, got[p])
		}
	}
}
