// Package desktop runs the VPN on a computer: a system service holds the core
// and the TUN, and the app, which runs without administrator rights, controls
// it over a local connection (Serve). The service does on its own what the
// Android app's VPN service does: it follows the computer's network and
// watches the channel.
package desktop

import (
	"net/netip"
	"slices"
)

// Uplink is the network the computer reaches the internet through outside
// the VPN: the interface with the best default route other than the TUN.
type Uplink struct {
	// Index is the interface's index.
	Index int
	// Kind is "wifi", "cellular", "ethernet" or "" when unknown, as the core
	// names networks (mobile.SetNetwork).
	Kind string
	// MTU is the interface's, 0 when unknown.
	MTU int
	// DNS are the interface's DNS servers.
	DNS []netip.Addr
	// Addrs are the interface's own addresses: when they change, the
	// connections from the old ones are dead.
	Addrs []netip.Addr
}

func (u Uplink) dns() []string {
	var out []string
	for _, a := range u.DNS {
		out = append(out, a.String())
	}
	return out
}

// sameAddrs reports whether u still has every address it had before.
func (u Uplink) keeps(before Uplink) bool {
	for _, a := range before.Addrs {
		if !slices.Contains(u.Addrs, a) {
			return false
		}
	}
	return true
}

// Platform is what the service needs from the system.
type Platform interface {
	// Uplink returns the network the computer uses now, ok false when it
	// has none.
	Uplink() (u Uplink, ok bool, err error)
	// WatchNetwork calls changed, from any goroutine, whenever the
	// interfaces, their addresses or the routes change. Stop ends it.
	WatchNetwork(changed func()) (stop func(), err error)
	// ConfigureTUN gives the TUN the core created by name its addresses,
	// the routes of all traffic, its DNS server and the MTU.
	ConfigureTUN(name string, mtu int) error
	// ReleaseTUN undoes what ConfigureTUN set that outlives the TUN.
	ReleaseTUN(name string)
}

// Addresses inside the TUN, the same as on Android. DNS goes to tunDNS and
// the core answers it.
var (
	tunAddr  = netip.MustParsePrefix("172.19.0.1/30")
	tunDNS   = netip.MustParseAddr("172.19.0.2")
	tunAddr6 = netip.MustParsePrefix("fdfe:dcba:9876::1/126")
)

// TUNName is the name of the network adapter the service creates.
const TUNName = "TigerSB"
