// Package tuic carries TUIC v5 transports. Xray has no TUIC, so each TUIC
// transport of a profile becomes an outbound handler built on sing-quic (the
// TUIC client of sing-box) and is added to Xray under the transport's tag:
// pick outbounds, the observatory and the per-site picker then use it like any
// other outbound.
package tuic

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
	"os"
	"time"

	"github.com/gofrs/uuid/v5"
	"github.com/sagernet/sing-quic/tuic"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	aTLS "github.com/sagernet/sing/common/tls"
	"github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/transport"

	"github.com/Stilltiger45/TigerSB-client/internal/carry"
	"github.com/Stilltiger45/TigerSB-client/internal/engine"
	"github.com/Stilltiger45/TigerSB-client/internal/xrayconf"
	"github.com/Stilltiger45/TigerSB-client/pkg/profile"
)

// Hooks returns the engine.Start hooks that add the profile's TUIC
// transports to Xray. noise (xrayconf.QUICNoise, or nil in tests) goes to
// the server before each QUIC connection.
func Hooks(p *profile.Profile, noise []xrayconf.NoisePacket) []func(*core.Instance) error {
	var hooks []func(*core.Instance) error
	for _, ob := range xrayconf.Carried(p, "tuic") {
		hooks = append(hooks, func(inst *core.Instance) error { return Attach(inst, ob.Tag, ob.Xray, noise) })
	}
	return hooks
}

// config is a TUIC transport as pkg/profile writes it, in Xray's style.
type config struct {
	Settings struct {
		Address           string `json:"address"`
		Port              uint16 `json:"port"`
		UUID              string `json:"uuid"`
		Password          string `json:"password"`
		CongestionControl string `json:"congestionControl"`
		UDPRelayMode      string `json:"udpRelayMode"`
		ZeroRTTHandshake  bool   `json:"zeroRTTHandshake"`
	} `json:"settings"`
	Stream struct {
		TLS struct {
			ServerName    string   `json:"serverName"`
			ALPN          []string `json:"alpn"`
			Pin           string   `json:"pinnedPeerCertSha256"`
			AllowInsecure bool     `json:"allowInsecure"`
		} `json:"tlsSettings"`
	} `json:"streamSettings"`
}

// Attach adds the TUIC outbound to a created, not yet started Xray instance;
// noise goes to the server before each QUIC connection.
func Attach(inst *core.Instance, tag string, xray json.RawMessage, noise []xrayconf.NoisePacket) error {
	var c config
	if err := json.Unmarshal(xray, &c); err != nil {
		return fmt.Errorf("транспорт %s: %w", tag, err)
	}
	s := c.Settings
	id, err := uuid.FromString(s.UUID)
	if err != nil {
		return fmt.Errorf("транспорт %s: неверный uuid", tag)
	}
	t := c.Stream.TLS
	if t.AllowInsecure && t.Pin == "" {
		// Like Xray, refuse to connect without checking the certificate.
		return fmt.Errorf("транспорт %s: сертификат сервера не закреплён", tag)
	}
	server := M.ParseSocksaddrHostPort(s.Address, s.Port)
	tlsConf := &tls.Config{
		ServerName: or(t.ServerName, s.Address),
		NextProtos: t.ALPN,
		MinVersion: tls.VersionTLS13,
	}
	if len(tlsConf.NextProtos) == 0 {
		tlsConf.NextProtos = []string{"h3"}
	}
	if t.Pin != "" {
		pin, err := hex.DecodeString(t.Pin)
		if err != nil || len(pin) != sha256.Size {
			return fmt.Errorf("транспорт %s: неверный pinnedPeerCertSha256", tag)
		}
		tlsConf.InsecureSkipVerify = true
		tlsConf.VerifyPeerCertificate = func(certs [][]byte, _ [][]*x509.Certificate) error {
			if len(certs) > 0 {
				if sum := sha256.Sum256(certs[0]); bytes.Equal(sum[:], pin) {
					return nil
				}
			}
			return stderrors.New("сертификат сервера не совпадает с закреплённым")
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	client, err := tuic.NewClient(tuic.ClientOptions{
		Context:           ctx,
		Dialer:            trackedDialer{N.SystemDialer, noise},
		ServerAddress:     server,
		TLSConfig:         &stdConfig{tlsConf},
		UUID:              id,
		Password:          s.Password,
		CongestionControl: s.CongestionControl,
		UDPStream:         s.UDPRelayMode == "quic",
		ZeroRTTHandshake:  s.ZeroRTTHandshake,
	})
	if err != nil {
		cancel()
		return fmt.Errorf("транспорт %s: %w", tag, err)
	}
	base, err := carry.NewBase(inst, tag)
	if err != nil {
		cancel()
		return err
	}
	return carry.Add(inst, &Handler{Base: base, client: client, cancel: cancel})
}

// Handler is a TUIC outbound. It implements Xray's outbound.Handler.
type Handler struct {
	*carry.Base
	client *tuic.Client
	cancel context.CancelFunc
}

func (h *Handler) Start() error                         { return nil }
func (h *Handler) SenderSettings() *serial.TypedMessage { return nil }
func (h *Handler) ProxySettings() *serial.TypedMessage  { return nil }

func (h *Handler) Close() error {
	h.cancel()
	return h.client.CloseWithError(os.ErrClosed)
}

// Dispatch carries one connection through the TUIC server.
func (h *Handler) Dispatch(ctx context.Context, link *transport.Link) {
	h.Base.Dispatch(ctx, link, "tuic", h.stream, h.packets)
}

func (h *Handler) stream(ctx context.Context, link *transport.Link, target net.Destination) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	conn, err := h.client.DialConn(ctx, socksaddr(target))
	if err != nil {
		return err
	}
	defer conn.Close()
	// The request goes out with the first data; send it now, so that a server
	// that speaks first (SMTP, SSH) is reached too.
	if _, err := conn.Write(nil); err != nil {
		return err
	}
	return h.Stream(ctx, cancel, link, conn)
}

func (h *Handler) packets(ctx context.Context, link *transport.Link, target net.Destination) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	pc, err := h.client.ListenPacket(ctx)
	if err != nil {
		return err
	}
	defer pc.Close()
	to := func(d net.Destination) gonet.Addr { return socksaddr(d) }
	return h.Packets(ctx, cancel, link, pc, target, to, destination)
}

// socksaddr turns an Xray destination into sing's form.
func socksaddr(d net.Destination) M.Socksaddr {
	port := uint16(d.Port)
	if d.Address.Family().IsDomain() {
		return M.ParseSocksaddrHostPort(d.Address.Domain(), port)
	}
	ip, _ := netip.AddrFromSlice(d.Address.IP())
	return M.SocksaddrFrom(ip.Unmap(), port)
}

// destination turns a packet's source into Xray's form.
func destination(a gonet.Addr) net.Destination {
	s := M.SocksaddrFromNet(a)
	if s.IsFqdn() {
		return net.UDPDestination(net.DomainAddress(s.Fqdn), net.Port(s.Port))
	}
	return net.UDPDestination(net.IPAddress(s.Addr.AsSlice()), net.Port(s.Port))
}

// stdConfig hands a standard TLS config to sing-quic, which only takes it
// back with STDConfig to run QUIC.
type stdConfig struct{ c *tls.Config }

func (s *stdConfig) ServerName() string                { return s.c.ServerName }
func (s *stdConfig) SetServerName(name string)         { s.c.ServerName = name }
func (s *stdConfig) NextProtos() []string              { return s.c.NextProtos }
func (s *stdConfig) SetNextProtos(p []string)          { s.c.NextProtos = p }
func (s *stdConfig) HandshakeTimeout() time.Duration   { return 0 }
func (s *stdConfig) SetHandshakeTimeout(time.Duration) {}
func (s *stdConfig) STDConfig() (*tls.Config, error)   { return s.c, nil }
func (s *stdConfig) Clone() aTLS.Config                { return &stdConfig{s.c.Clone()} }
func (s *stdConfig) Client(gonet.Conn) (aTLS.Conn, error) {
	return nil, stderrors.New("TUIC uses TLS only inside QUIC")
}

func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// trackedDialer dials like the system dialer and hands what it opens to
// engine.Track, so that a change of network closes TUIC's connections like
// Xray's own. The UDP socket of each QUIC connection first sends noise.
type trackedDialer struct {
	N.Dialer
	noise []xrayconf.NoisePacket
}

func (d trackedDialer) DialContext(ctx context.Context, network string, dest M.Socksaddr) (gonet.Conn, error) {
	c, err := d.Dialer.DialContext(ctx, network, dest)
	if err != nil {
		return nil, err
	}
	engine.Track(c)
	if N.NetworkName(network) == N.NetworkUDP {
		xrayconf.SendNoise(func(b []byte) { c.Write(b) }, d.noise)
	}
	return c, nil
}

func (d trackedDialer) ListenPacket(ctx context.Context, dest M.Socksaddr) (gonet.PacketConn, error) {
	c, err := d.Dialer.ListenPacket(ctx, dest)
	if err == nil {
		engine.Track(c)
	}
	return c, err
}
