// Package mieru carries mieru transports. Xray has no mieru, so each mieru
// transport of a profile becomes an outbound handler built on mieru's own
// client library and is added to Xray under the transport's tag.
package mieru

import (
	"context"
	"encoding/base64"
	"encoding/json"
	stderrors "errors"
	"fmt"
	gonet "net"
	"strconv"
	"strings"

	mclient "github.com/enfein/mieru/v3/apis/client"
	mcommon "github.com/enfein/mieru/v3/apis/common"
	mlog "github.com/enfein/mieru/v3/apis/log"
	"github.com/enfein/mieru/v3/apis/model"
	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/transport"
	"google.golang.org/protobuf/proto"

	"github.com/Stilltiger45/TigerSB-client/internal/carry"
	"github.com/Stilltiger45/TigerSB-client/internal/engine"
	"github.com/Stilltiger45/TigerSB-client/internal/xrayconf"
	"github.com/Stilltiger45/TigerSB-client/pkg/profile"
)

func init() {
	// mieru logs to the standard output by itself; the client keeps its own
	// journal, so only mieru's errors are of interest.
	mlog.SetLevel("ERROR")
}

// Hooks returns the engine.Start hooks that add the profile's mieru
// transports to Xray.
func Hooks(p *profile.Profile) []func(*core.Instance) error {
	var hooks []func(*core.Instance) error
	for _, ob := range xrayconf.Carried(p, "mieru") {
		hooks = append(hooks, func(inst *core.Instance) error { return Attach(inst, ob.Tag, ob.Xray) })
	}
	return hooks
}

// config is a mieru transport as pkg/profile writes it, in Xray's style:
// the fields of a mierus:// link, or the whole client config of a mieru://
// link.
type config struct {
	Settings struct {
		Address  string `json:"address"`
		Username string `json:"username"`
		Password string `json:"password"`
		Profile  string `json:"profile"`
		Ports    []struct {
			Port     string `json:"port"`
			Protocol string `json:"protocol"`
		} `json:"ports"`
		MTU            int32  `json:"mtu"`
		Multiplexing   string `json:"multiplexing"`
		HandshakeMode  string `json:"handshakeMode"`
		TrafficPattern string `json:"trafficPattern"`
		Config         string `json:"config"`
	} `json:"settings"`
}

// clientProfile builds mieru's client profile from the transport.
func clientProfile(raw json.RawMessage) (*appctlpb.ClientProfile, error) {
	var c config
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, err
	}
	s := c.Settings
	if s.Config != "" {
		return fromConfig(s.Config)
	}
	if s.Username == "" || s.Password == "" || s.Address == "" || len(s.Ports) == 0 {
		return nil, stderrors.New("нет адреса, портов, имени или пароля")
	}
	server := &appctlpb.ServerEndpoint{}
	if gonet.ParseIP(s.Address) != nil {
		server.IpAddress = proto.String(s.Address)
	} else {
		server.DomainName = proto.String(s.Address)
	}
	for _, b := range s.Ports {
		pr, ok := appctlpb.TransportProtocol_value[strings.ToUpper(b.Protocol)]
		if !ok {
			return nil, fmt.Errorf("протокол %q не поддерживается", b.Protocol)
		}
		binding := &appctlpb.PortBinding{Protocol: appctlpb.TransportProtocol(pr).Enum()}
		if n, err := strconv.Atoi(b.Port); err == nil {
			binding.Port = proto.Int32(int32(n))
		} else {
			binding.PortRange = proto.String(b.Port)
		}
		server.PortBindings = append(server.PortBindings, binding)
	}
	p := &appctlpb.ClientProfile{
		ProfileName: proto.String(or(s.Profile, "default")),
		User:        &appctlpb.User{Name: proto.String(s.Username), Password: proto.String(s.Password)},
		Servers:     []*appctlpb.ServerEndpoint{server},
	}
	if s.MTU != 0 {
		p.Mtu = proto.Int32(s.MTU)
	}
	if s.Multiplexing != "" {
		level, ok := appctlpb.MultiplexingLevel_value[s.Multiplexing]
		if !ok {
			return nil, fmt.Errorf("уровень мультиплексирования %q не поддерживается", s.Multiplexing)
		}
		p.Multiplexing = &appctlpb.MultiplexingConfig{Level: appctlpb.MultiplexingLevel(level).Enum()}
	}
	if s.HandshakeMode != "" {
		mode, ok := appctlpb.HandshakeMode_value[s.HandshakeMode]
		if !ok {
			return nil, fmt.Errorf("режим рукопожатия %q не поддерживается", s.HandshakeMode)
		}
		p.HandshakeMode = appctlpb.HandshakeMode(mode).Enum()
	}
	if s.TrafficPattern != "" {
		b, err := base64.StdEncoding.DecodeString(s.TrafficPattern)
		if err != nil {
			return nil, stderrors.New("неверный traffic-pattern")
		}
		tp := &appctlpb.TrafficPattern{}
		if err := proto.Unmarshal(b, tp); err != nil {
			return nil, stderrors.New("неверный traffic-pattern")
		}
		p.TrafficPattern = tp
	}
	return p, nil
}

// fromConfig takes the active profile of a client config from a mieru://
// link.
func fromConfig(enc string) (*appctlpb.ClientProfile, error) {
	b, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return nil, stderrors.New("неверная ссылка mieru://")
	}
	c := &appctlpb.ClientConfig{}
	if err := proto.Unmarshal(b, c); err != nil {
		return nil, stderrors.New("неверная ссылка mieru://")
	}
	for _, p := range c.GetProfiles() {
		if p.GetProfileName() == c.GetActiveProfile() {
			return p, nil
		}
	}
	if len(c.GetProfiles()) == 0 {
		return nil, stderrors.New("в ссылке mieru:// нет профиля")
	}
	return c.GetProfiles()[0], nil
}

// Attach adds the mieru outbound to a created, not yet started Xray instance.
func Attach(inst *core.Instance, tag string, xray json.RawMessage) error {
	p, err := clientProfile(xray)
	if err != nil {
		return fmt.Errorf("транспорт %s: %w", tag, err)
	}
	client := mclient.NewClient()
	err = client.Store(&mclient.ClientConfig{
		Profile:      p,
		Dialer:       trackedDialer{},
		PacketDialer: trackedDialer{},
		Resolver:     gonet.DefaultResolver,
	})
	if err != nil {
		return fmt.Errorf("транспорт %s: %w", tag, err)
	}
	if err := client.Start(); err != nil {
		return fmt.Errorf("транспорт %s: %w", tag, err)
	}
	base, err := carry.NewBase(inst, tag)
	if err != nil {
		client.Stop()
		return err
	}
	return carry.Add(inst, &Handler{Base: base, client: client})
}

// Handler is a mieru outbound. It implements Xray's outbound.Handler.
type Handler struct {
	*carry.Base
	client mclient.Client
}

func (h *Handler) Start() error                         { return nil }
func (h *Handler) Close() error                         { return h.client.Stop() }
func (h *Handler) SenderSettings() *serial.TypedMessage { return nil }
func (h *Handler) ProxySettings() *serial.TypedMessage  { return nil }

// Dispatch carries one connection through the mieru server.
func (h *Handler) Dispatch(ctx context.Context, link *transport.Link) {
	h.Base.Dispatch(ctx, link, "mieru", h.stream, h.packets)
}

func (h *Handler) stream(ctx context.Context, link *transport.Link, target net.Destination) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	conn, err := h.client.DialContext(ctx, addrSpec(target, "tcp"))
	if err != nil {
		return err
	}
	defer conn.Close()
	return h.Stream(ctx, cancel, link, conn)
}

// packets carries UDP as mieru does: a UDP associate over a stream.
func (h *Handler) packets(ctx context.Context, link *transport.Link, target net.Destination) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	conn, err := h.client.DialContext(ctx, addrSpec(target, "udp"))
	if err != nil {
		return err
	}
	pc := mcommon.NewUDPAssociateWrapper(mcommon.NewPacketOverStreamTunnel(conn))
	defer pc.Close()
	to := func(d net.Destination) gonet.Addr { return addrSpec(d, "udp") }
	from := func(a gonet.Addr) net.Destination {
		u, _ := a.(*gonet.UDPAddr)
		if u == nil {
			return net.UDPDestination(net.AnyIP, 0)
		}
		return net.UDPDestination(net.IPAddress(u.IP), net.Port(u.Port))
	}
	return h.Packets(ctx, cancel, link, pc, target, to, from)
}

// addrSpec turns an Xray destination into mieru's form.
func addrSpec(d net.Destination, network string) model.NetAddrSpec {
	a := model.AddrSpec{Port: int(d.Port)}
	if d.Address.Family().IsDomain() {
		a.FQDN = d.Address.Domain()
	} else {
		a.IP = d.Address.IP()
	}
	return model.NetAddrSpec{AddrSpec: a, Net: network}
}

func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// trackedDialer dials like the system and hands what it opens to
// engine.Track, so that a change of network closes mieru's connections like
// Xray's own.
type trackedDialer struct{}

func (trackedDialer) DialContext(ctx context.Context, network, address string) (gonet.Conn, error) {
	var d gonet.Dialer
	c, err := d.DialContext(ctx, network, address)
	if err == nil {
		engine.Track(c)
	}
	return c, err
}

func (trackedDialer) ListenPacket(_ context.Context, network, laddr, _ string) (gonet.PacketConn, error) {
	var local *gonet.UDPAddr
	if laddr != "" {
		a, err := gonet.ResolveUDPAddr(network, laddr)
		if err != nil {
			return nil, err
		}
		local = a
	}
	pc, err := gonet.ListenUDP(network, local)
	if err != nil {
		return nil, err
	}
	engine.Track(pc)
	return pc, nil
}
