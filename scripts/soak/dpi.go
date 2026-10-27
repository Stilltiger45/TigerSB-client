package main

import (
	"bytes"
	"encoding/binary"
	"io"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// Level is how hard the stand-in for DPI filters.
type Level int32

const (
	// Off lets everything through.
	Off Level = iota
	// Weak tells a flow by its first packet: a TCP connection whose first
	// segment carries the VLESS user id is reset, a UDP flow whose first
	// datagram is a QUIC handshake (Hysteria 2, TUIC) is dropped. A split
	// connection start and noise before QUIC get through.
	Weak
	// Strong cuts VLESS like Weak and finds QUIC in any of the first
	// strongPackets datagrams of a flow, so noise does not help: Hysteria 2
	// and TUIC are cut altogether, as where QUIC is blocked outright.
	Strong
)

const strongPackets = 20

var levelNames = []string{"off", "weak", "strong"}

func (l Level) String() string { return levelNames[l] }

// ParseLevel reads a level by name; "1" and "0" are the old -dpi values.
func ParseLevel(s string) (Level, bool) {
	switch s {
	case "1":
		return Weak, true
	case "0", "":
		return Off, true
	}
	for i, n := range levelNames {
		if n == s {
			return Level(i), true
		}
	}
	return Off, false
}

// DPI is the stand-in: boxes in front of the test servers that share one
// level. Changing the level cuts every connection and flow that passed
// before, as DPI switched on in the middle of a session does, so the
// app's connections are judged anew.
type DPI struct {
	level atomic.Int32
	sign  []byte // the VLESS user id

	mu    sync.Mutex
	conns map[net.Conn]bool
	boxes []*udpBox

	tcpBlocked, tcpPassed atomic.Int64
	udpBlocked, udpPassed atomic.Int64
}

func NewDPI(level Level, sign []byte) *DPI {
	d := &DPI{sign: sign, conns: map[net.Conn]bool{}}
	d.level.Store(int32(level))
	return d
}

func (d *DPI) Level() Level { return Level(d.level.Load()) }

// SetLevel changes the level and cuts what passed before.
func (d *DPI) SetLevel(l Level) {
	d.level.Store(int32(l))
	d.mu.Lock()
	for c := range d.conns {
		c.(*net.TCPConn).SetLinger(0)
		c.Close()
	}
	boxes := d.boxes
	d.mu.Unlock()
	for _, b := range boxes {
		b.cutAll()
	}
	d.Report("level " + l.String())
}

func (d *DPI) Report(why string) {
	log.Printf("dpi %s (%s): tcp blocked %d, passed %d; udp flows blocked %d, passed %d",
		d.Level(), why, d.tcpBlocked.Load(), d.tcpPassed.Load(), d.udpBlocked.Load(), d.udpPassed.Load())
}

// ServeTCP passes connections from the public port to target.
func (d *DPI) ServeTCP(l net.Listener, target string) {
	for {
		c, err := l.Accept()
		if err != nil {
			log.Fatal(err)
		}
		go d.serveTCP(c, target)
	}
}

func (d *DPI) serveTCP(c net.Conn, target string) {
	defer c.Close()
	d.track(c, true)
	defer d.track(c, false)
	first := make([]byte, 16384)
	c.SetReadDeadline(time.Now().Add(10 * time.Second))
	n, err := c.Read(first)
	if err != nil {
		return
	}
	c.SetReadDeadline(time.Time{})
	if d.Level() != Off && bytes.Contains(first[:n], d.sign) {
		d.tcpBlocked.Add(1)
		c.(*net.TCPConn).SetLinger(0)
		return
	}
	d.tcpPassed.Add(1)
	up, err := net.Dial("tcp", target)
	if err != nil {
		return
	}
	defer up.Close()
	if _, err := up.Write(first[:n]); err != nil {
		return
	}
	go func() {
		io.Copy(up, c)
		up.(*net.TCPConn).CloseWrite()
	}()
	io.Copy(c, up)
}

func (d *DPI) track(c net.Conn, on bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if on {
		d.conns[c] = true
	} else {
		delete(d.conns, c)
	}
}

// ServeUDP passes datagrams from the public port to target and back, one
// flow per client address.
func (d *DPI) ServeUDP(pc net.PacketConn, target string) {
	b := &udpBox{dpi: d, pc: pc, target: target, flows: map[string]*flow{}}
	d.mu.Lock()
	d.boxes = append(d.boxes, b)
	d.mu.Unlock()
	go b.expire()
	buf := make([]byte, 65535)
	for {
		n, from, err := pc.ReadFrom(buf)
		if err != nil {
			log.Fatal(err)
		}
		b.forward(buf[:n], from)
	}
}

type udpBox struct {
	dpi    *DPI
	pc     net.PacketConn
	target string

	mu    sync.Mutex
	flows map[string]*flow
}

type flow struct {
	up      *net.UDPConn
	packets int
	judged  bool // passed the checks of the level
	blocked atomic.Bool
	last    atomic.Int64
}

const flowIdle = 2 * time.Minute

func (b *udpBox) forward(p []byte, from net.Addr) {
	b.mu.Lock()
	f := b.flows[from.String()]
	if f == nil {
		raddr, _ := net.ResolveUDPAddr("udp", b.target)
		up, err := net.DialUDP("udp", nil, raddr)
		if err != nil {
			b.mu.Unlock()
			return
		}
		f = &flow{up: up}
		b.flows[from.String()] = f
		go b.back(f, from)
	}
	f.last.Store(time.Now().UnixNano())
	level := b.dpi.Level()
	if !f.blocked.Load() && !f.judged {
		quic := isQUICHandshake(p)
		switch {
		case level == Off:
			f.judged = true
		case level == Weak && f.packets == 0 || level == Strong && f.packets < strongPackets:
			if quic {
				f.blocked.Store(true)
				b.dpi.udpBlocked.Add(1)
			}
		}
		f.packets++
		if !f.blocked.Load() && (level == Off || level == Weak || f.packets >= strongPackets) {
			f.judged = true
			b.dpi.udpPassed.Add(1)
		}
	}
	b.mu.Unlock()
	if !f.blocked.Load() {
		f.up.Write(p)
	}
}

// back carries the server's answers to the client.
func (b *udpBox) back(f *flow, to net.Addr) {
	buf := make([]byte, 65535)
	for {
		n, err := f.up.Read(buf)
		if err != nil {
			return
		}
		if !f.blocked.Load() {
			b.pc.WriteTo(buf[:n], to)
		}
	}
}

// cutAll blocks every flow that exists: the client has to start new ones,
// which the level judges.
func (b *udpBox) cutAll() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, f := range b.flows {
		f.blocked.Store(true)
	}
}

func (b *udpBox) expire() {
	for range time.Tick(30 * time.Second) {
		b.mu.Lock()
		for k, f := range b.flows {
			if time.Since(time.Unix(0, f.last.Load())) > flowIdle {
				f.up.Close()
				delete(b.flows, k)
			}
		}
		b.mu.Unlock()
	}
}

// isQUICHandshake reports whether the datagram is a QUIC v1 or v2 Initial,
// the packet that starts a QUIC connection: a long header, padded to at
// least 1200 bytes.
func isQUICHandshake(p []byte) bool {
	if len(p) < 1200 || p[0]&0xc0 != 0xc0 {
		return false
	}
	v := binary.BigEndian.Uint32(p[1:5])
	return v == 1 || v == 0x6b3343cf
}
