package naive

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	stderrors "errors"
	"fmt"
	gonet "net"
	"net/netip"
	"time"

	"github.com/apernet/quic-go"
	"github.com/apernet/quic-go/http3"
	"github.com/xtls/xray-core/common/net"

	"github.com/Stilltiger45/TigerSB-client/internal/engine"
	"github.com/Stilltiger45/TigerSB-client/internal/xrayconf"
)

// quicDialer opens HTTP/3 connections to a naive+quic:// server.
type quicDialer struct {
	tls   *tls.Config
	noise []xrayconf.NoisePacket
	h3    http3.Transport
}

// quicTLS reads the TLS of a NaïveProxy transport over QUIC: the server's
// name, HTTP/3 and the pinned certificate, if any.
func quicTLS(xray json.RawMessage, address string) (*tls.Config, error) {
	var ob struct {
		Stream struct {
			TLS struct {
				ServerName string   `json:"serverName"`
				ALPN       []string `json:"alpn"`
				Pin        string   `json:"pinnedPeerCertSha256"`
			} `json:"tlsSettings"`
		} `json:"streamSettings"`
	}
	if err := json.Unmarshal(xray, &ob); err != nil {
		return nil, err
	}
	t := ob.Stream.TLS
	c := &tls.Config{
		ServerName: t.ServerName,
		NextProtos: t.ALPN,
		MinVersion: tls.VersionTLS13,
	}
	if c.ServerName == "" {
		c.ServerName = address
	}
	if len(c.NextProtos) == 0 {
		c.NextProtos = []string{http3.NextProtoH3}
	}
	if t.Pin != "" {
		pin, err := hex.DecodeString(t.Pin)
		if err != nil || len(pin) != sha256.Size {
			return nil, stderrors.New("неверный pinnedPeerCertSha256")
		}
		c.InsecureSkipVerify = true
		c.VerifyPeerCertificate = func(certs [][]byte, _ [][]*x509.Certificate) error {
			if len(certs) > 0 {
				if sum := sha256.Sum256(certs[0]); bytes.Equal(sum[:], pin) {
					return nil
				}
			}
			return stderrors.New("сертификат сервера не совпадает с закреплённым")
		}
	}
	return c, nil
}

// dial opens a QUIC connection to the server from a socket of its own, which
// engine.Track closes on a change of network, and speaks HTTP/3 in it.
func (d *quicDialer) dial(ctx context.Context, server net.Destination) (serverConn, error) {
	addr, err := resolve(ctx, server)
	if err != nil {
		return nil, err
	}
	pc, err := gonet.ListenUDP("udp", nil)
	if err != nil {
		return nil, err
	}
	engine.Track(pc)
	xrayconf.SendNoise(func(b []byte) { pc.WriteTo(b, addr) }, d.noise)
	qc, err := quic.Dial(ctx, pc, addr, d.tls.Clone(), &quic.Config{
		// Chrome's: a connection the network dropped silently is noticed
		// and replaced instead of new streams hanging in it.
		MaxIdleTimeout:  30 * time.Second,
		KeepAlivePeriod: 15 * time.Second,
	})
	if err != nil {
		pc.Close()
		return nil, fmt.Errorf("сервер NaïveProxy по QUIC: %w", err)
	}
	go func() {
		<-qc.Context().Done()
		pc.Close()
	}()
	return h3Conn{d.h3.NewClientConn(qc), qc}, nil
}

// resolve finds the server's address, IPv4 first.
func resolve(ctx context.Context, server net.Destination) (*gonet.UDPAddr, error) {
	if server.Address.Family().IsIP() {
		return &gonet.UDPAddr{IP: server.Address.IP(), Port: int(server.Port)}, nil
	}
	ips, err := gonet.DefaultResolver.LookupNetIP(ctx, "ip", server.Address.Domain())
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("нет адреса у %s", server.Address.Domain())
	}
	ip := ips[0]
	for _, a := range ips {
		if a.Unmap().Is4() {
			ip = a
			break
		}
	}
	return gonet.UDPAddrFromAddrPort(netip.AddrPortFrom(ip.Unmap(), uint16(server.Port))), nil
}

// h3Conn is an HTTP/3 connection. QUIC multiplexes the streams itself and
// makes a new one wait for room, so the connection takes streams until it
// is gone.
type h3Conn struct {
	*http3.ClientConn
	qc *quic.Conn
}

func (c h3Conn) closed() bool { return c.qc.Context().Err() != nil }
func (c h3Conn) usable() bool { return !c.closed() }
func (c h3Conn) Close() error { return c.qc.CloseWithError(0, "") }
