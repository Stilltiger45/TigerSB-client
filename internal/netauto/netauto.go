// Package netauto picks network settings from the network the device is on,
// so the user never has to tune them.
package netauto

// Transport is the kind of the active network.
type Transport string

const (
	Wifi     Transport = "wifi"
	Cellular Transport = "cellular"
	Ethernet Transport = "ethernet"
)

// Bounds of an MTU worth trusting: IPv6 needs at least 1280, and the TUN gains
// nothing above Ethernet's 1500 when packets leave the device unwrapped.
const (
	minMTU = 1280
	maxMTU = 1500
)

// MTU returns the TUN MTU. Traffic sent through the VPN is re-packed by Xray,
// but direct UDP (QUIC to banks, games, calls) leaves with the size the app
// chose, so the TUN must not promise more than the real network carries.
// linkMTU is what the system reports for the active network, 0 if unknown.
func MTU(t Transport, linkMTU int) int {
	if linkMTU >= minMTU && linkMTU <= maxMTU {
		return linkMTU
	}
	if t == Cellular {
		// Mobile networks often wrap packets (GTP, 464XLAT) and drop full
		// 1500-byte datagrams.
		return 1400
	}
	return maxMTU
}
