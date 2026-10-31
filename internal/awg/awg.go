// Package awg carries WireGuard and AmneziaWG transports. Each becomes an
// outbound handler with its own AmneziaWG device, whose tunnel ends in a
// userspace network stack: the handler dials the apps' connections in that
// stack, and the device sends them to the server over one UDP socket.
// AmneziaWG with no obfuscation is plain WireGuard, so it carries both.
package awg

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	stderrors "errors"
	"fmt"
	gonet "net"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/amnezia-vpn/amneziawg-go/v3/conn"
	"github.com/amnezia-vpn/amneziawg-go/v3/device"
	"github.com/amnezia-vpn/amneziawg-go/v3/tun/netstack"
	"github.com/xtls/xray-core/common/errors"
	"github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/transport"

	"github.com/Stilltiger45/TigerSB-client/internal/carry"
	"github.com/Stilltiger45/TigerSB-client/internal/engine"
	"github.com/Stilltiger45/TigerSB-client/internal/xrayconf"
	"github.com/Stilltiger45/TigerSB-client/pkg/profile"
)

// Hooks returns the engine.Start hooks that add the profile's WireGuard and
// AmneziaWG transports to Xray.
func Hooks(p *profile.Profile) []func(*core.Instance) error {
	var hooks []func(*core.Instance) error
	for _, ob := range xrayconf.Carried(p, "wireguard") {
		hooks = append(hooks, func(inst *core.Instance) error { return Attach(inst, ob.Tag, ob.Xray) })
	}
	return hooks
}

// config is a transport as pkg/profile writes it: Xray's WireGuard outbound
// with the tunnel's DNS and the AmneziaWG keys in the .conf spelling.
type config struct {
	Settings struct {
		SecretKey string   `json:"secretKey"`
		Address   []string `json:"address"`
		DNS       []string `json:"dns"`
		MTU       int      `json:"mtu"`
		Peers     []struct {
			PublicKey    string          `json:"publicKey"`
			PreSharedKey string          `json:"preSharedKey"`
			Endpoint     string          `json:"endpoint"`
			KeepAlive    json.RawMessage `json:"keepAlive"`
			AllowedIPs   []string        `json:"allowedIPs"`
		} `json:"peers"`
		Amnezia map[string]string `json:"amnezia"`
	} `json:"settings"`
}

// defaultMTU fits the tunnel's packets, with AmneziaWG's paddings, into
// mobile networks whose path is narrower than 1500.
const defaultMTU = 1280

// uapiKeys are the device's names of the AmneziaWG keys.
var uapiKeys = map[string]string{
	"Jc": "jc", "Jmin": "jmin", "Jmax": "jmax",
	"S1": "s1", "S2": "s2", "S3": "s3", "S4": "s4",
	"H1": "h1", "H2": "h2", "H3": "h3", "H4": "h4",
	"I1": "i1", "I2": "i2", "I3": "i3", "I4": "i4", "I5": "i5",
	"HeaderProtectionKey":    "header_protection_key",
	"ContentPaddingAddition": "content_padding_addition",
	"RekeyAfterTime":         "rekey_after_time",
	"RekeyTimeout":           "rekey_timeout",
	"RejectAfterTime":        "reject_after_time",
	"KeepaliveTimeout":       "keepalive_timeout",
	"MaxHandshakeAttempts":   "max_handshake_attempts",
	"RandomTrailers":         "random_trailers",
	"DisableCookies":         "disable_cookies",
}

// tunnel reads the transport: the tunnel's addresses and DNS servers, its
// MTU, and the device's settings (in its UAPI form) but for the server's
// address, which may be a name.
func tunnel(raw json.RawMessage) (addrs, dns []netip.Addr, mtu int, uapi, endpoint string, err error) {
	var c config
	if err = json.Unmarshal(raw, &c); err != nil {
		return
	}
	s := c.Settings
	if len(s.Peers) == 0 || s.Peers[0].Endpoint == "" {
		err = stderrors.New("нет сервера")
		return
	}
	for _, a := range s.Address {
		p, perr := netip.ParsePrefix(a)
		if perr != nil {
			ip, ierr := netip.ParseAddr(a)
			if ierr != nil {
				err = fmt.Errorf("неверный адрес %q", a)
				return
			}
			p = netip.PrefixFrom(ip, ip.BitLen())
		}
		addrs = append(addrs, p.Addr())
	}
	if len(addrs) == 0 {
		err = stderrors.New("нет адреса в туннеле")
		return
	}
	for _, d := range s.DNS {
		if ip, perr := netip.ParseAddr(d); perr == nil {
			dns = append(dns, ip)
		}
	}
	if len(dns) == 0 {
		dns = []netip.Addr{netip.MustParseAddr("1.1.1.1"), netip.MustParseAddr("8.8.8.8")}
	}
	mtu = s.MTU
	if mtu <= 0 {
		mtu = defaultMTU
	}
	var b strings.Builder
	key := func(k, v string) { fmt.Fprintf(&b, "%s=%s\n", k, v) }
	hexKey := func(name, v string) (string, error) {
		k, err := base64.StdEncoding.DecodeString(v)
		if err != nil || len(k) != 32 {
			return "", fmt.Errorf("неверный %s", name)
		}
		return hex.EncodeToString(k), nil
	}
	private, err := hexKey("закрытый ключ", s.SecretKey)
	if err != nil {
		return
	}
	key("private_key", private)
	for k, v := range s.Amnezia {
		name, ok := uapiKeys[k]
		if !ok {
			continue // AmneziaWG 1.5's J1-J3 and Itime, gone since 2.0
		}
		if k == "HeaderProtectionKey" {
			if v, err = hexKey("HeaderProtectionKey", v); err != nil {
				return
			}
		}
		key(name, v)
	}
	p := s.Peers[0]
	public, err := hexKey("открытый ключ сервера", p.PublicKey)
	if err != nil {
		return
	}
	key("public_key", public)
	if p.PreSharedKey != "" {
		psk, perr := hexKey("PresharedKey", p.PreSharedKey)
		if perr != nil {
			err = perr
			return
		}
		key("preshared_key", psk)
	}
	if len(p.KeepAlive) > 0 && string(p.KeepAlive) != "null" {
		var ka any
		json.Unmarshal(p.KeepAlive, &ka)
		switch v := ka.(type) {
		case float64:
			key("persistent_keepalive_interval", strconv.Itoa(int(v)))
		case string:
			key("persistent_keepalive_interval", v)
		}
	}
	allowed := p.AllowedIPs
	if len(allowed) == 0 {
		allowed = []string{"0.0.0.0/0", "::/0"}
	}
	for _, a := range allowed {
		key("allowed_ip", a)
	}
	return addrs, dns, mtu, b.String(), p.Endpoint, nil
}

// Attach adds the WireGuard or AmneziaWG outbound to a created, not yet
// started Xray instance. The device comes up once the server's name is
// resolved; connections meanwhile wait for it.
func Attach(inst *core.Instance, tag string, xray json.RawMessage) error {
	addrs, dns, mtu, uapi, endpoint, err := tunnel(xray)
	if err != nil {
		return fmt.Errorf("транспорт %s: %w", tag, err)
	}
	base, err := carry.NewBase(inst, tag)
	if err != nil {
		return err
	}
	tun, tnet, err := netstack.CreateNetTUN(addrs, dns, mtu)
	if err != nil {
		return fmt.Errorf("транспорт %s: %w", tag, err)
	}
	logf := func(format string, args ...any) {
		errors.LogWarning(context.Background(), "awg ", tag, ": ", fmt.Sprintf(format, args...))
	}
	b := &bind{}
	h := &Handler{
		Base:  base,
		addrs: addrs,
		bind:  b,
		dev:   device.NewDevice(tun, b, &device.Logger{Verbosef: verbosef, Errorf: logf}),
		net:   tnet,
		ready: make(chan struct{}),
	}
	h.life, h.end = context.WithCancel(context.Background())
	go h.up(uapi, endpoint)
	return carry.Add(inst, h)
}

// verbosef gets the devices' detailed log; tests read it.
var verbosef = device.DiscardLogf

// Handler is a WireGuard or AmneziaWG outbound. It implements Xray's
// outbound.Handler.
type Handler struct {
	*carry.Base
	bind  *bind
	dev   *device.Device
	net   *netstack.Net
	addrs []netip.Addr // the device's own in the tunnel

	ready chan struct{} // closed once the device is up or has failed
	err   error         // why it failed, set before ready closes

	life context.Context // ends with Close
	end  context.CancelFunc
}

// up resolves the server's address and brings the device up.
func (h *Handler) up(uapi, endpoint string) {
	defer close(h.ready)
	host, port, _ := gonet.SplitHostPort(endpoint)
	ip, err := netip.ParseAddr(host)
	if err != nil {
		ctx, cancel := context.WithTimeout(h.life, 15*time.Second)
		ips, rerr := gonet.DefaultResolver.LookupNetIP(ctx, "ip", host)
		cancel()
		if rerr != nil || len(ips) == 0 {
			h.err = fmt.Errorf("нет адреса у сервера %s: %v", host, rerr)
			return
		}
		ip = ips[0]
		for _, a := range ips {
			if a.Is4() {
				ip = a
				break
			}
		}
	}
	uapi += "endpoint=" + gonet.JoinHostPort(ip.Unmap().String(), port) + "\n"
	if err := h.dev.IpcSet(uapi); err != nil {
		h.err = err
		return
	}
	if h.err = h.dev.Up(); h.err == nil {
		go h.watchQuiet(uapi)
	}
}

// watchQuiet has the bind take a new socket when the server falls silent in
// the middle of a burst (bind.quiet), and sends a keepalive from it at once:
// the server learns the new address from it and resends there what it sent
// into the frozen flow.
func (h *Handler) watchQuiet(uapi string) {
	var pk device.NoisePublicKey
	for line := range strings.Lines(uapi) {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "public_key="); ok {
			pk.FromHex(v)
		}
	}
	peer := h.dev.LookupPeer(pk)
	if peer == nil {
		return
	}
	t := time.NewTicker(quietGap / 4)
	defer t.Stop()
	for {
		select {
		case <-h.life.Done():
			return
		case now := <-t.C:
			if h.bind.quiet(now) {
				peer.SendKeepalive()
			}
		}
	}
}

// wait waits for the device to come up.
func (h *Handler) wait(ctx context.Context) error {
	select {
	case <-h.ready:
		return h.err
	case <-ctx.Done():
		return ctx.Err()
	case <-h.life.Done():
		return gonet.ErrClosed
	}
}

func (h *Handler) Start() error                         { return nil }
func (h *Handler) SenderSettings() *serial.TypedMessage { return nil }
func (h *Handler) ProxySettings() *serial.TypedMessage  { return nil }

func (h *Handler) Close() error {
	h.end()
	h.dev.Close()
	return nil
}

// Dispatch carries one connection through the tunnel.
func (h *Handler) Dispatch(ctx context.Context, link *transport.Link) {
	h.Base.Dispatch(ctx, link, "wireguard", h.stream, h.packets)
}

func (h *Handler) stream(ctx context.Context, link *transport.Link, target net.Destination) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if err := h.wait(ctx); err != nil {
		return err
	}
	dctx, dcancel := context.WithTimeout(ctx, 15*time.Second)
	c, err := h.net.DialContext(dctx, "tcp", target.NetAddr())
	dcancel()
	if err != nil {
		return err
	}
	defer c.Close()
	return h.Stream(ctx, cancel, link, c)
}

func (h *Handler) packets(ctx context.Context, link *transport.Link, target net.Destination) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if err := h.wait(ctx); err != nil {
		return err
	}
	first, err := h.resolve(ctx, target.Address)
	if err != nil {
		return err
	}
	// The stack takes packets from a socket on one of its own addresses.
	i := slices.IndexFunc(h.addrs, func(a netip.Addr) bool { return a.Is6() == first.Is6() })
	if i < 0 {
		return fmt.Errorf("в туннеле нет адреса IPv%d для %s", map[bool]int{false: 4, true: 6}[first.Is6()], first)
	}
	pc, err := h.net.ListenUDPAddrPort(netip.AddrPortFrom(h.addrs[i], 0))
	if err != nil {
		return err
	}
	defer pc.Close()
	// Names are resolved in the tunnel once each.
	var mu sync.Mutex
	names := map[string]netip.Addr{}
	if target.Address.Family().IsDomain() {
		names[target.Address.Domain()] = first
	}
	to := func(d net.Destination) gonet.Addr {
		if d.Address.Family().IsDomain() {
			mu.Lock()
			ip, ok := names[d.Address.Domain()]
			mu.Unlock()
			if !ok {
				var err error
				if ip, err = h.resolve(ctx, d.Address); err != nil {
					return &gonet.UDPAddr{}
				}
				mu.Lock()
				names[d.Address.Domain()] = ip
				mu.Unlock()
			}
			return gonet.UDPAddrFromAddrPort(netip.AddrPortFrom(ip, uint16(d.Port)))
		}
		return &gonet.UDPAddr{IP: d.Address.IP(), Port: int(d.Port)}
	}
	from := func(a gonet.Addr) net.Destination {
		u, _ := a.(*gonet.UDPAddr)
		if u == nil {
			return net.UDPDestination(net.AnyIP, 0)
		}
		return net.UDPDestination(net.IPAddress(u.IP), net.Port(u.Port))
	}
	return h.Packets(ctx, cancel, link, pc, target, to, from)
}

// resolve finds an address in the tunnel, through the tunnel's DNS for a name.
func (h *Handler) resolve(ctx context.Context, a net.Address) (netip.Addr, error) {
	if !a.Family().IsDomain() {
		ip, _ := netip.AddrFromSlice(a.IP())
		return ip.Unmap(), nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ips, err := h.net.LookupContextHost(ctx, a.Domain())
	if err != nil {
		return netip.Addr{}, err
	}
	for _, s := range ips {
		if ip, err := netip.ParseAddr(s); err == nil {
			return ip.Unmap(), nil
		}
	}
	return netip.Addr{}, fmt.Errorf("нет адреса у %s", a.Domain())
}

// bind is the device's UDP socket to the server. engine.Track closes it on
// a change of network, like Xray's own connections; the next read or send
// then opens a new one on the new network, and the server, seeing the
// device's packets from a new address, answers there (WireGuard roams).
//
// The bind also takes a new socket when the server stops answering: DPI can
// freeze a UDP flow it does not know after some kilobytes, and a new socket
// is a new flow, as for TUIC and Hysteria 2 with their port hopping.
type bind struct {
	mu     sync.Mutex
	pc     *gonet.UDPConn
	closed bool

	unanswered int       // packets sent since the last one came in
	since      time.Time // when the first of them was sent
	renewed    time.Time // when the bind last took a new socket for silence
	heard      time.Time // when the last packet came in
	burst      int       // packets that came in on this socket since then
}

// A socket that sent this many packets over silence without a single one
// coming back is replaced, at most once per silence. While data flows the
// server answers within a round trip; keepalives alone (one in 25 s, which
// the server does not answer) take a minute and a half to get there.
//
// A download stops the other way: the server sends into the frozen flow and
// the phone, getting nothing, sends nothing either. So a socket that got a
// burst of burstPackets or more and then nothing for quietGap is replaced
// too. After an ordinary burst, a page that has loaded, that costs a new
// socket and one keepalive.
const (
	silence       = 3 * time.Second
	silentPackets = 4
	quietGap      = time.Second
	burstPackets  = 8
)

var _ conn.Bind = (*bind)(nil)

func (b *bind) Open(port uint16) ([]conn.ReceiveFunc, uint16, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.pc != nil && !b.closed {
		return nil, 0, conn.ErrBindAlreadyOpen
	}
	b.closed = false
	if err := b.listen(); err != nil {
		return nil, 0, err
	}
	return []conn.ReceiveFunc{b.receive}, uint16(b.pc.LocalAddr().(*gonet.UDPAddr).Port), nil
}

// listen opens a new socket; mu is held.
func (b *bind) listen() error {
	pc, err := gonet.ListenUDP("udp", nil)
	if err != nil {
		return err
	}
	engine.Track(pc)
	b.pc = pc
	return nil
}

// socket returns the open socket, after old was closed a new one.
func (b *bind) socket(old *gonet.UDPConn) (*gonet.UDPConn, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil, gonet.ErrClosed
	}
	if b.pc == old || b.pc == nil {
		if old != nil {
			old.Close()
		}
		if err := b.listen(); err != nil {
			b.pc = nil
			return nil, err
		}
	}
	return b.pc, nil
}

func (b *bind) current() *gonet.UDPConn {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.pc
}

func (b *bind) receive(packets [][]byte, sizes []int, eps []conn.Endpoint) (int, error) {
	pc := b.current()
	for {
		if pc != nil {
			n, ap, err := pc.ReadFromUDPAddrPort(packets[0])
			if err == nil {
				b.mu.Lock()
				b.unanswered = 0
				b.heard = time.Now()
				b.burst++
				b.mu.Unlock()
				sizes[0] = n
				eps[0] = endpoint(netip.AddrPortFrom(ap.Addr().Unmap(), ap.Port()))
				return 1, nil
			}
		}
		next, err := b.socket(pc)
		if stderrors.Is(err, gonet.ErrClosed) {
			return 0, gonet.ErrClosed
		}
		if err != nil {
			// No network for now: try again shortly.
			time.Sleep(time.Second)
			continue
		}
		pc = next
	}
}

func (b *bind) Send(bufs [][]byte, ep conn.Endpoint) error {
	e, ok := ep.(endpoint)
	if !ok {
		return conn.ErrWrongEndpointType
	}
	pc := b.sending()
	for try := 0; ; try++ {
		if pc != nil {
			var err error
			for _, p := range bufs {
				if _, err = pc.WriteToUDPAddrPort(p, netip.AddrPort(e)); err != nil {
					break
				}
			}
			if err == nil || try > 0 || !stderrors.Is(err, gonet.ErrClosed) {
				return err
			}
		}
		next, err := b.socket(pc)
		if err != nil {
			return err
		}
		pc = next
	}
}

// sending returns the socket to send through: a new one when the server has
// gone silent on the old.
func (b *bind) sending() *gonet.UDPConn {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	if b.unanswered == 0 {
		b.since = now
	}
	b.unanswered++
	if b.closed || b.pc == nil || b.unanswered < silentPackets ||
		now.Sub(b.since) < silence || now.Sub(b.renewed) < silence {
		return b.pc
	}
	verbosef("awg: no answer for %v, sending from a new socket", now.Sub(b.since).Round(time.Second))
	b.renew(now)
	return b.pc
}

// quiet tells whether the server fell silent after a burst, and if so takes
// a new socket.
func (b *bind) quiet(now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed || b.pc == nil || b.burst < burstPackets || now.Sub(b.heard) < quietGap {
		return false
	}
	verbosef("awg: silent for %v after %d packets, sending from a new socket", now.Sub(b.heard).Round(time.Second), b.burst)
	return b.renew(now)
}

// renew replaces the socket; mu is held.
func (b *bind) renew(now time.Time) bool {
	old := b.pc
	if err := b.listen(); err != nil {
		b.pc = old
		return false
	}
	old.Close()
	b.renewed, b.unanswered, b.burst = now, 0, 0
	return true
}

func (b *bind) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	if b.pc != nil {
		return b.pc.Close()
	}
	return nil
}

func (b *bind) SetMark(uint32) error { return nil }
func (b *bind) BatchSize() int       { return 1 }

func (b *bind) ParseEndpoint(s string) (conn.Endpoint, error) {
	ap, err := netip.ParseAddrPort(s)
	if err != nil {
		return nil, err
	}
	return endpoint(netip.AddrPortFrom(ap.Addr().Unmap(), ap.Port())), nil
}

// endpoint is the server's address.
type endpoint netip.AddrPort

func (e endpoint) ClearSrc()           {}
func (e endpoint) SrcToString() string { return "" }
func (e endpoint) DstToString() string { return netip.AddrPort(e).String() }
func (e endpoint) DstToBytes() []byte  { b, _ := netip.AddrPort(e).MarshalBinary(); return b }
func (e endpoint) DstIP() netip.Addr   { return netip.AddrPort(e).Addr() }
func (e endpoint) SrcIP() netip.Addr   { return netip.Addr{} }
