package engine

import (
	"context"
	"testing"
	"time"

	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/transport/internet"
	"github.com/xtls/xray-core/transport/internet/hysteria"
	"github.com/xtls/xray-core/transport/internet/tls"
)

// TestForgetHysteria dials a Hysteria server that is not there, which
// leaves a client in Xray's cache, and checks that ForgetHysteria sees and
// drops it: the mirrored layout of the cache is right.
func TestForgetHysteria(t *testing.T) {
	ForgetHysteria()
	if n := hysteriaCached(); n != 0 {
		t.Fatalf("%d cached after forgetting", n)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ms := &internet.MemoryStreamConfig{
		ProtocolName:     "hysteria",
		ProtocolSettings: &hysteria.Config{Version: 2},
		SecurityType:     "tls",
		SecuritySettings: &tls.Config{ServerName: "example.com"},
	}
	hysteria.Dial(ctx, xnet.UDPDestination(xnet.LocalHostIP, 9), ms)
	if n := hysteriaCached(); n != 1 {
		t.Fatalf("%d cached after a dial", n)
	}
	ForgetHysteria()
	if n := hysteriaCached(); n != 0 {
		t.Fatalf("%d cached after forgetting", n)
	}
}
