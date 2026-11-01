// Package bypass keeps the core's own connections out of the VPN on systems
// where the core runs behind its own TUN. Android leaves the app out of its
// VPN; Windows has nothing like it, so every socket the core opens to the
// network is bound to the interface that carries the system's traffic
// outside the TUN (Set), and names are asked of that interface's DNS
// servers. Until Set is called, sockets go where the system routes them.
package bypass

import (
	"context"
	"net"
	"net/http"
	"sync/atomic"
	"syscall"
	"time"
)

// uplink is the interface the core's sockets leave through.
type uplink struct {
	index int
	dns   []string // host:port
}

var current atomic.Pointer[uplink]

// Set binds the sockets opened from now on to the interface with this index
// and sends name lookups to dns, its DNS servers as IP addresses. Index 0
// unbinds them.
func Set(index int, dns []string) {
	u := &uplink{index: index}
	for _, s := range dns {
		u.dns = append(u.dns, net.JoinHostPort(s, "53"))
	}
	current.Store(u)
}

// Index is the interface the sockets are bound to, 0 for none.
func Index() int {
	if u := current.Load(); u != nil {
		return u.index
	}
	return 0
}

// Control binds a socket to the interface Set chose. It suits
// net.Dialer.Control and net.ListenConfig.Control.
func Control(network, _ string, c syscall.RawConn) error {
	i := Index()
	if i == 0 {
		return nil
	}
	var err error
	if cerr := c.Control(func(fd uintptr) { err = bind(fd, network, i) }); cerr != nil {
		return cerr
	}
	return err
}

// Dialer is a dialer whose connections bypass the VPN.
func Dialer(timeout time.Duration) *net.Dialer {
	return &net.Dialer{Timeout: timeout, Control: Control}
}

// ListenUDP is net.ListenUDP for a socket that bypasses the VPN.
func ListenUDP(network string, laddr *net.UDPAddr) (*net.UDPConn, error) {
	lc := net.ListenConfig{Control: Control}
	addr := ""
	if laddr != nil {
		addr = laddr.String()
	}
	pc, err := lc.ListenPacket(context.Background(), network, addr)
	if err != nil {
		return nil, err
	}
	return pc.(*net.UDPConn), nil
}

// Resolver asks the DNS servers of the interface Set chose, through that
// interface, while one is chosen; otherwise the system's resolver. Install
// makes it the process's net.DefaultResolver.
var Resolver = &net.Resolver{
	PreferGo: true,
	Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
		u := current.Load()
		d := net.Dialer{Control: Control}
		if u == nil || u.index == 0 || len(u.dns) == 0 {
			return d.DialContext(ctx, network, address)
		}
		var first error
		for _, s := range u.dns {
			c, err := d.DialContext(ctx, network, s)
			if err == nil {
				return c, nil
			}
			if first == nil {
				first = err
			}
		}
		return nil, first
	},
}

// Install makes Resolver the resolver of the whole process, the cores'
// included. Call it once at the start, on systems that need it.
func Install() { net.DefaultResolver = Resolver }

// Transport is http.DefaultTransport with connections that bypass the VPN.
func Transport() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.DialContext = Dialer(30 * time.Second).DialContext
	return t
}
