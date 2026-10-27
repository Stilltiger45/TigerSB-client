package main

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"strconv"
	"time"

	"github.com/gofrs/uuid/v5"
	"github.com/sagernet/sing-quic/tuic"
	sbuf "github.com/sagernet/sing/common/buf"
	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	aTLS "github.com/sagernet/sing/common/tls"
)

// runTUIC serves TUIC v5 for user id and password on pc. TCP to BulkIP goes
// to the bulk server, the rest to its destination.
func runTUIC(pc net.PacketConn, cert tls.Certificate, id, password, bulk string) error {
	svc, err := tuic.NewService[int](tuic.ServiceOptions{
		Context:           context.Background(),
		Logger:            logger.NOP(),
		TLSConfig:         &serverTLS{&tls.Config{Certificates: []tls.Certificate{cert}, NextProtos: []string{"h3"}}},
		CongestionControl: "bbr",
		Handler:           exit{bulk},
		UDPTimeout:        time.Minute,
	})
	if err != nil {
		return err
	}
	svc.UpdateUsers([]int{1}, [][16]byte{uuid.Must(uuid.FromString(id))}, []string{password})
	return svc.Start(pc)
}

// serverTLS hands a standard TLS config to the sing-quic server.
type serverTLS struct{ c *tls.Config }

func (s *serverTLS) ServerName() string                 { return "" }
func (s *serverTLS) SetServerName(string)               {}
func (s *serverTLS) NextProtos() []string               { return s.c.NextProtos }
func (s *serverTLS) SetNextProtos(p []string)           { s.c.NextProtos = p }
func (s *serverTLS) HandshakeTimeout() time.Duration    { return 0 }
func (s *serverTLS) SetHandshakeTimeout(time.Duration)  {}
func (s *serverTLS) STDConfig() (*tls.Config, error)    { return s.c, nil }
func (s *serverTLS) Clone() aTLS.Config                 { return &serverTLS{s.c.Clone()} }
func (s *serverTLS) Client(net.Conn) (aTLS.Conn, error) { return nil, errors.ErrUnsupported }
func (s *serverTLS) Start() error                       { return nil }
func (s *serverTLS) Close() error                       { return nil }
func (s *serverTLS) Server(net.Conn) (aTLS.Conn, error) { return nil, errors.ErrUnsupported }

// exit is where the TUIC server sends traffic.
type exit struct{ bulk string }

func done(c io.Closer, onClose N.CloseHandlerFunc) {
	c.Close()
	if onClose != nil {
		onClose(nil)
	}
}

func (e exit) NewConnectionEx(ctx context.Context, conn net.Conn, _, dst M.Socksaddr, onClose N.CloseHandlerFunc) {
	defer done(conn, onClose)
	addr := dst.String()
	if dst.Addr.String() == BulkIP && dst.Port == BulkPort {
		addr = e.bulk
	}
	up, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return
	}
	defer up.Close()
	go func() {
		io.Copy(up, conn)
		up.(*net.TCPConn).CloseWrite()
	}()
	io.Copy(conn, up)
}

// NewPacketConnectionEx relays UDP: each packet to its destination, the
// answers back.
func (exit) NewPacketConnectionEx(ctx context.Context, conn N.PacketConn, _, _ M.Socksaddr, onClose N.CloseHandlerFunc) {
	defer done(conn, onClose)
	out, err := net.ListenPacket("udp", ":0")
	if err != nil {
		return
	}
	defer out.Close()
	go func() {
		buf := make([]byte, 65535)
		for {
			out.SetReadDeadline(time.Now().Add(2 * time.Minute))
			n, from, err := out.ReadFrom(buf)
			if err != nil {
				conn.Close()
				return
			}
			b := sbuf.NewPacket()
			b.Write(buf[:n])
			if conn.WritePacket(b, M.SocksaddrFromNet(from)) != nil {
				return
			}
		}
	}()
	for {
		b := sbuf.NewPacket()
		dst, err := conn.ReadPacket(b)
		if err != nil {
			b.Release()
			return
		}
		addr, err := net.ResolveUDPAddr("udp", net.JoinHostPort(dst.AddrString(), strconv.Itoa(int(dst.Port))))
		if err == nil {
			out.WriteTo(b.Bytes(), addr)
		}
		b.Release()
	}
}
