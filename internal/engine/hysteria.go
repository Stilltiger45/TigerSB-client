package engine

import (
	"sync"
	"unsafe"

	// The clients ForgetHysteria drops live in this package.
	_ "github.com/xtls/xray-core/transport/internet/hysteria"
)

// hysteriaClients is Xray's cache of Hysteria clients, one per server
// address for the life of the process (transport/internet/hysteria,
// clientManager). Each keeps the settings of the core that dialed the
// server first, its UDP masks among them. The layout mirrors Xray's:
// the map, then the mutex. TestForgetHysteria checks it.
//
//go:linkname hysteriaClients github.com/xtls/xray-core/transport/internet/hysteria.manger
var hysteriaClients *struct {
	m     map[string]unsafe.Pointer
	mutex sync.Mutex
}

// ForgetHysteria drops the cached Hysteria clients, so that the next core
// dials its servers with its own settings: noise before QUIC in the smart
// mode only. Call it with no core running, after CloseConnections has closed
// the old clients' sockets.
func ForgetHysteria() {
	if hysteriaClients == nil {
		return
	}
	hysteriaClients.mutex.Lock()
	clear(hysteriaClients.m)
	hysteriaClients.mutex.Unlock()
}

// hysteriaCached reports how many Hysteria clients are cached, for tests.
func hysteriaCached() int {
	hysteriaClients.mutex.Lock()
	defer hysteriaClients.mutex.Unlock()
	return len(hysteriaClients.m)
}
