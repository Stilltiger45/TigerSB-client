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

// MTUs are the TUN MTUs the app picks from, largest first.
var MTUs = []int{1480, 1460, 1280}

// MTU returns the TUN MTU: the largest of MTUs that fits the network.
// Traffic sent through the VPN is re-packed by Xray, but direct UDP (QUIC to
// banks, games, calls) leaves with the size the app chose, so the TUN must
// not promise more than the real network carries without fragmenting.
// linkMTU is what the system reports for the active network and pathMTU what
// PathMTU measured towards the server, 0 when unknown. Without a measurement
// mobile data gets 1460: its packets are often wrapped (GTP, 464XLAT), and
// Android reports 1500 for it all the same.
func MTU(t Transport, linkMTU, pathMTU int) int {
	limit := maxMTU
	if linkMTU >= minMTU && linkMTU < limit {
		limit = linkMTU
	}
	if pathMTU >= minMTU && pathMTU < limit {
		limit = pathMTU
	}
	if pathMTU == 0 && t == Cellular {
		limit = min(limit, 1460)
	}
	for _, m := range MTUs {
		if m <= limit {
			return m
		}
	}
	return minMTU
}

// MSS returns the TCP segment size for connections to the servers on a path
// that carries pathMTU bytes, so the network never has to split them: 0 when
// the path is clean or unknown and the system's own choice is right.
func MSS(pathMTU int) int {
	if pathMTU < minMTU || pathMTU >= maxMTU {
		return 0
	}
	return pathMTU - 40 // IPv4 and TCP headers
}
