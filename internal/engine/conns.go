package engine

import (
	"context"
	"io"
	gonet "net"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"weak"

	"github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/transport/internet"
)

// Every connection the cores open to the network goes through the dialer
// below, which remembers it, so that CloseConnections can drop them all at
// once. That is how the app follows a new network without restarting the
// core and the tunnel: the connections of the old network are closed, Xray
// closes the apps' connections that ran over them, and the apps reconnect at
// once, over the new network, instead of waiting for their own timeouts.

func init() {
	inner := &internet.DefaultSystemDialer{}
	internet.UseAlternativeSystemDialer(inner)
	internet.RegisterDialerController(beforeConnect)
	internet.UseAlternativeSystemDialer(&dialer{inner: inner})
}

// Link describes the network the phone is on now. The panel's own socket
// options always win.
type Link struct {
	// Mobile is the LTE mode: TCP keepalives every 10 s after 30 s of
	// silence hold the operator's NAT open, and a connection whose data goes
	// unacknowledged for 30 s, lost in a handover, is dropped and redialled
	// instead of hanging.
	Mobile bool
	// MSS, when set, caps the TCP segments so that a path narrower than
	// 1500 bytes never has to split them (netauto.MSS).
	MSS int
}

var link atomic.Pointer[Link]

// SetLink tunes the TCP connections dialled from now on for the network.
func SetLink(l Link) { link.Store(&l) }

func currentLink() Link {
	if l := link.Load(); l != nil {
		return *l
	}
	return Link{}
}

// beforeConnect sets the link's options that must precede the handshake. Xray
// applies the panel's socket options after it, so they override these.
func beforeConnect(network, _ string, c syscall.RawConn) error {
	if len(network) < 3 || network[:3] != "tcp" {
		return nil
	}
	l := currentLink()
	return c.Control(func(fd uintptr) { linkOptions(fd, l) })
}

// mobileKeepAlive is the LTE mode's keepalive.
var mobileKeepAlive = gonet.KeepAliveConfig{Enable: true, Idle: 30 * time.Second, Interval: 10 * time.Second, Count: -1}

type dialer struct{ inner internet.SystemDialer }

func (d *dialer) Dial(ctx context.Context, src net.Address, dest net.Destination, sockopt *internet.SocketConfig) (net.Conn, error) {
	c, err := d.inner.Dial(ctx, src, dest, sockopt)
	if err != nil {
		return nil, err
	}
	// Go sets the keepalive after the handshake, over anything set before,
	// so the LTE mode sets its own here unless the panel chose one.
	if tc, ok := c.(*gonet.TCPConn); ok && currentLink().Mobile &&
		(sockopt == nil || sockopt.TcpKeepAliveIdle == 0 && sockopt.TcpKeepAliveInterval == 0) {
		tc.SetKeepAliveConfig(mobileKeepAlive)
	}
	Track(c)
	return c, nil
}

func (d *dialer) DestIpAddress() net.IP { return d.inner.DestIpAddress() }

// The connections are held weakly: one that Xray closed and dropped is
// collected as usual and leaves an empty slot, swept on the next additions.
var (
	connMu    sync.Mutex
	conns     []func() io.Closer
	sweepMark = 64
)

// Track remembers a connection to the network for CloseConnections. The
// cores' own dials are tracked already; a handler that dials by itself, like
// TUIC's, tracks what it opens. Kinds of connections it does not know are
// left alone.
func Track(c any) {
	var ref func() io.Closer
	switch v := c.(type) {
	case *gonet.TCPConn:
		ref = weakRef(v)
	case *gonet.UDPConn:
		ref = weakRef(v)
	case *internet.PacketConnWrapper:
		ref = weakRef(v)
	default:
		return
	}
	connMu.Lock()
	defer connMu.Unlock()
	conns = append(conns, ref)
	if len(conns) >= sweepMark {
		live := conns[:0]
		for _, r := range conns {
			if r() != nil {
				live = append(live, r)
			}
		}
		clear(conns[len(live):])
		conns = live
		sweepMark = max(64, 2*len(live))
	}
}

func weakRef[T any, P interface {
	*T
	io.Closer
}](c P) func() io.Closer {
	w := weak.Make((*T)(c))
	return func() io.Closer {
		if p := w.Value(); p != nil {
			return P(p)
		}
		return nil
	}
}

// CloseConnections closes every connection the cores have open to the
// network and returns how many were open. Closing one that is closed
// already does nothing.
func CloseConnections() int {
	connMu.Lock()
	refs := conns
	conns = nil
	sweepMark = 64
	connMu.Unlock()
	n := 0
	for _, r := range refs {
		if c := r(); c != nil {
			if c.Close() == nil {
				n++
			}
		}
	}
	return n
}
