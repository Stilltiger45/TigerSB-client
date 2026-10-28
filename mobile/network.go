package mobile

import (
	"context"
	gonet "net"
	"sync/atomic"
	"time"

	"github.com/Stilltiger45/TigerSB-client/internal/engine"
	"github.com/Stilltiger45/TigerSB-client/internal/steady"
)

// A change of network or a channel that comes back after an outage does not
// restart the VPN: the tunnel and the core stay, only the core's connections
// to the network are closed. The apps' connections that ran over them are
// closed with them, so the apps reconnect at once, and the new connections go
// over the network that works now.

// SwitchNetwork moves the running VPN to the network the phone switched to:
// transport is its kind, as for SetNetwork. Call PickMTU for that network
// first. The old network's connections are closed, the transports are
// checked again, and the smart mode takes the DPI preset that worked last on
// this kind of network and tunes it again. It returns how many connections
// were closed.
func SwitchNetwork(transport string) int {
	mu.Lock()
	i := inst
	changed := network != transport
	network = transport
	mu.Unlock()
	n := engine.CloseConnections()
	if i == nil {
		return n
	}
	if changed {
		steady.SetPreset(i, savedPresets()[transport])
	}
	steady.Renew(i)
	go tune(i, transport)
	return n
}

// ResetConnections closes the running VPN's connections to the network and
// has the transports checked again, for a channel that comes
// back after an outage on the same network or that DPI started to cut: the
// apps' connections stuck on the dead ones reconnect at once, and the smart
// mode tunes its DPI preset again. It returns how many
// connections were closed.
func ResetConnections() int {
	n := engine.CloseConnections()
	mu.Lock()
	i, kind := inst, network
	mu.Unlock()
	if i != nil {
		steady.Renew(i)
		go tune(i, kind)
	}
	return n
}

// onlineHosts are reached directly, by address, to tell whether the phone's
// network has internet at all.
var onlineHosts = []string{"77.88.8.8:53", "1.1.1.1:53", "8.8.8.8:53"}

// onlineTimeout bounds Online.
var onlineTimeout = 3 * time.Second

// Online reports whether the phone's network reaches the internet by itself,
// outside the VPN (the app's own connections bypass it). When it does not,
// restarting the VPN fixes nothing and the app only waits. It takes up to 3
// seconds.
func Online() bool {
	ctx, cancel := context.WithTimeout(context.Background(), onlineTimeout)
	defer cancel()
	var ok atomic.Bool
	done := make(chan struct{}, len(onlineHosts))
	for _, h := range onlineHosts {
		go func() {
			defer func() { done <- struct{}{} }()
			var d gonet.Dialer
			if c, err := d.DialContext(ctx, "tcp", h); err == nil {
				c.Close()
				ok.Store(true)
				cancel()
			}
		}()
	}
	for range onlineHosts {
		<-done
	}
	return ok.Load()
}
