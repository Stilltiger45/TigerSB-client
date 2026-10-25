package engine

import (
	"context"
	"reflect"
	"unsafe"

	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/features/inbound"
	"github.com/xtls/xray-core/proxy"
	"github.com/xtls/xray-core/proxy/tun"
)

// Stop closes the instance and the network stacks of its TUN inbounds.
//
// Xray's TUN inbound has no Close: closing the instance leaves its gVisor
// stack running and still reading the TUN descriptor. On Android the app then
// closes that descriptor and opens a new TUN for the next start, which
// usually gets the same number, so the stale stack takes packets from the new
// tunnel and connections stall. Every reconnect (a mode switch, a new
// network) added one more such stack.
func Stop(inst *core.Instance) error {
	var stacks []interface{ Close() error }
	if m, _ := inst.GetFeature(inbound.ManagerType()).(inbound.Manager); m != nil {
		for _, h := range m.ListHandlers(context.Background()) {
			if s := tunStack(h); s != nil {
				stacks = append(stacks, s)
			}
		}
	}
	err := inst.Close()
	for _, s := range stacks {
		s.Close()
	}
	return err
}

// tunStack returns the network stack of a TUN inbound, or nil for any other
// inbound. The stack is an unexported field of tun.Handler.
func tunStack(h inbound.Handler) interface{ Close() error } {
	g, ok := h.(interface{ GetInbound() proxy.Inbound })
	if !ok {
		return nil
	}
	t, ok := g.GetInbound().(*tun.Handler)
	if !ok || t == nil {
		return nil
	}
	f := reflect.ValueOf(t).Elem().FieldByName("stack")
	if !f.IsValid() || f.Kind() != reflect.Interface {
		return nil
	}
	f = reflect.NewAt(f.Type(), unsafe.Pointer(f.UnsafeAddr())).Elem()
	if f.IsNil() {
		return nil
	}
	s, _ := f.Interface().(interface{ Close() error })
	return s
}
