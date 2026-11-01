package engine

import (
	"context"
	"reflect"
	"unsafe"

	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/features/inbound"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
)

// field reads an unexported field of the struct v points to.
func field(v any, name string) (reflect.Value, bool) {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() || rv.Elem().Kind() != reflect.Struct {
		return reflect.Value{}, false
	}
	f := rv.Elem().FieldByName(name)
	if !f.IsValid() {
		return reflect.Value{}, false
	}
	return reflect.NewAt(f.Type(), unsafe.Pointer(f.UnsafeAddr())).Elem(), true
}

// gvisorStack returns the gVisor stack behind the network stack of a TUN
// inbound, nil when it is another kind or not started.
func gvisorStack(s any) *stack.Stack {
	f, ok := field(s, "stack")
	if !ok {
		return nil
	}
	g, _ := f.Interface().(*stack.Stack)
	return g
}

// tunDevice returns the device under the network stack of a TUN inbound.
func tunDevice(s any) any {
	f, ok := field(s, "tun")
	if !ok || (f.Kind() == reflect.Interface && f.IsNil()) {
		return nil
	}
	return f.Interface()
}

// RefuseIPv6 makes the TUN inbounds of inst refuse IPv6 at once: a TCP
// connection gets a reset, a UDP packet "port unreachable". Xray's own
// stack takes every connection before routing it, so an IPv6 one looked
// open and was then closed by the routing rule that blocks IPv6. A browser
// that resolves names itself, over DNS-over-HTTPS, gets IPv6 addresses for
// sites like YouTube and kept failing on them instead of falling back to
// IPv4. It returns how many stacks it changed.
func RefuseIPv6(inst *core.Instance) int {
	m, _ := inst.GetFeature(inbound.ManagerType()).(inbound.Manager)
	if m == nil {
		return 0
	}
	n := 0
	for _, h := range m.ListHandlers(context.Background()) {
		if s := tunStack(h); s != nil {
			if g := gvisorStack(s); g != nil && refuseIPv6(g) {
				n++
			}
		}
	}
	return n
}

func refuseIPv6(s *stack.Stack) bool {
	done := false
	for _, p := range []tcpip.TransportProtocolNumber{tcp.ProtocolNumber, udp.ProtocolNumber} {
		h := defaultHandler(s, p)
		if h == nil {
			continue
		}
		s.SetTransportProtocolHandler(p, func(id stack.TransportEndpointID, pkt *stack.PacketBuffer) bool {
			if id.LocalAddress.Len() == 16 {
				// Unhandled: the stack answers with a reset or an ICMP error.
				return false
			}
			return h(id, pkt)
		})
		done = true
	}
	return done
}

// defaultHandler reads the handler a protocol's packets go to when no
// endpoint takes them, which the TUN inbound set to its forwarder.
func defaultHandler(s *stack.Stack, p tcpip.TransportProtocolNumber) func(stack.TransportEndpointID, *stack.PacketBuffer) bool {
	protocols, ok := field(s, "transportProtocols")
	if !ok || protocols.Kind() != reflect.Map {
		return nil
	}
	state := protocols.MapIndex(reflect.ValueOf(p))
	if !state.IsValid() || state.IsNil() {
		return nil
	}
	h, ok := field(state.Interface(), "defaultHandler")
	if !ok {
		return nil
	}
	f, _ := h.Interface().(func(stack.TransportEndpointID, *stack.PacketBuffer) bool)
	return f
}
