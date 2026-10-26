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
	"github.com/xtls/xray-core/common"
	"github.com/xtls/xray-core/common/buf"
	"github.com/xtls/xray-core/common/errors"
	"github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/common/session"
	"github.com/xtls/xray-core/common/signal"
	"github.com/xtls/xray-core/common/task"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/features/outbound"
	"github.com/xtls/xray-core/features/policy"
	"github.com/xtls/xray-core/features/stats"
	"github.com/xtls/xray-core/transport"

	"github.com/Stilltiger45/TigerSB-client/internal/xrayconf"
	"github.com/Stilltiger45/TigerSB-client/pkg/profile"
)

// Hooks returns the engine.Start hooks that add the profile's TUIC
// transports to Xray.
func Hooks(p *profile.Profile) []func(*core.Instance) error {
	var hooks []func(*core.Instance) error
	for _, ob := range xrayconf.TUIC(p) {
		hooks = append(hooks, func(inst *core.Instance) error { return Attach(inst, ob.Tag, ob.Xray) })
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

// Attach adds the TUIC outbound to a created, not yet started Xray instance.
func Attach(inst *core.Instance, tag string, xray json.RawMessage) error {
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
		Dialer:            N.SystemDialer,
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
	ohm, _ := inst.GetFeature(outbound.ManagerType()).(outbound.Manager)
	pm, _ := inst.GetFeature(policy.ManagerType()).(policy.Manager)
	if ohm == nil || pm == nil {
		cancel()
		return stderrors.New("в ядре Xray нет менеджера исходящих или политик")
	}
	h := &Handler{tag: tag, client: client, cancel: cancel, policy: pm.ForLevel(0)}
	if sm, _ := inst.GetFeature(stats.ManagerType()).(stats.Manager); sm != nil {
		if pm.ForSystem().Stats.OutboundUplink {
			h.up, _ = stats.GetOrRegisterCounter(sm, "outbound>>>"+tag+">>>traffic>>>uplink")
		}
		if pm.ForSystem().Stats.OutboundDownlink {
			h.down, _ = stats.GetOrRegisterCounter(sm, "outbound>>>"+tag+">>>traffic>>>downlink")
		}
	}
	return ohm.AddHandler(context.Background(), h)
}

// Handler is a TUIC outbound. It implements Xray's outbound.Handler.
type Handler struct {
	tag      string
	client   *tuic.Client
	cancel   context.CancelFunc
	policy   policy.Session
	up, down stats.Counter
}

func (h *Handler) Tag() string                          { return h.tag }
func (h *Handler) Start() error                         { return nil }
func (h *Handler) SenderSettings() *serial.TypedMessage { return nil }
func (h *Handler) ProxySettings() *serial.TypedMessage  { return nil }

func (h *Handler) Close() error {
	h.cancel()
	return h.client.CloseWithError(os.ErrClosed)
}

// Dispatch carries one connection through the TUIC server.
func (h *Handler) Dispatch(ctx context.Context, link *transport.Link) {
	obs := session.OutboundsFromContext(ctx)
	ob := obs[len(obs)-1]
	ob.Name = "tuic"
	var err error
	switch {
	case !ob.Target.IsValid():
		err = stderrors.New("target not specified")
	case ob.Target.Network == net.Network_UDP:
		err = h.packets(ctx, link, ob.Target)
	default:
		err = h.stream(ctx, link, ob.Target)
	}
	if err != nil {
		errors.LogInfoInner(ctx, err, "tuic: ", h.tag, " -> ", ob.Target)
		common.Interrupt(link.Writer)
	} else {
		common.Close(link.Writer)
	}
	common.Interrupt(link.Reader)
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
	timer := signal.CancelAfterInactivity(ctx, cancel, h.policy.Timeouts.ConnectionIdle)
	request := func() error {
		defer timer.SetTimeout(h.policy.Timeouts.DownlinkOnly)
		return buf.Copy(link.Reader, counted(buf.NewWriter(conn), h.up), buf.UpdateActivity(timer))
	}
	response := func() error {
		defer timer.SetTimeout(h.policy.Timeouts.UplinkOnly)
		return buf.Copy(buf.NewReader(conn), counted(link.Writer, h.down), buf.UpdateActivity(timer))
	}
	return task.Run(ctx, request, response)
}

// maxPacket is the largest UDP payload.
const maxPacket = 65535

func (h *Handler) packets(ctx context.Context, link *transport.Link, target net.Destination) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	pc, err := h.client.ListenPacket(ctx)
	if err != nil {
		return err
	}
	defer pc.Close()
	timer := signal.CancelAfterInactivity(ctx, cancel, h.policy.Timeouts.ConnectionIdle)
	go func() {
		<-ctx.Done()
		pc.Close()
	}()
	request := func() error {
		defer timer.SetTimeout(h.policy.Timeouts.DownlinkOnly)
		for {
			mb, err := link.Reader.ReadMultiBuffer()
			if err != nil {
				return err
			}
			timer.Update()
			for _, b := range mb {
				dst := target
				if b.UDP != nil {
					dst = *b.UDP
				}
				if _, err := pc.WriteTo(b.Bytes(), socksaddr(dst)); err != nil {
					buf.ReleaseMulti(mb)
					return err
				}
				if h.up != nil {
					h.up.Add(int64(b.Len()))
				}
			}
			buf.ReleaseMulti(mb)
		}
	}
	response := func() error {
		defer timer.SetTimeout(h.policy.Timeouts.UplinkOnly)
		scratch := make([]byte, maxPacket)
		for {
			n, addr, err := pc.ReadFrom(scratch)
			if err != nil {
				return err
			}
			timer.Update()
			b := buf.New()
			if n > int(b.Cap()) {
				b.Release()
				b = buf.NewWithSize(int32(n))
			}
			b.Write(scratch[:n])
			from := destination(addr)
			b.UDP = &from
			if h.down != nil {
				h.down.Add(int64(n))
			}
			if err := link.Writer.WriteMultiBuffer(buf.MultiBuffer{b}); err != nil {
				return err
			}
		}
	}
	return task.Run(ctx, request, response)
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

// counted adds the size of what passes through w to c, when there is one.
func counted(w buf.Writer, c stats.Counter) buf.Writer {
	if c == nil {
		return w
	}
	return &countWriter{w, c}
}

type countWriter struct {
	buf.Writer
	c stats.Counter
}

func (w *countWriter) WriteMultiBuffer(mb buf.MultiBuffer) error {
	w.c.Add(int64(mb.Len()))
	return w.Writer.WriteMultiBuffer(mb)
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
