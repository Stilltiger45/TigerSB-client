// Package carry has what the outbounds of protocols Xray does not have (TUIC,
// mieru, NaïveProxy) share: each is an Xray outbound handler added under its
// transport's tag, so pick outbounds, the observatory and the per-site picker
// use it like any other outbound, and its traffic is counted like Xray's.
package carry

import (
	"context"
	stderrors "errors"
	gonet "net"

	"github.com/xtls/xray-core/common"
	"github.com/xtls/xray-core/common/buf"
	"github.com/xtls/xray-core/common/errors"
	"github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/session"
	"github.com/xtls/xray-core/common/signal"
	"github.com/xtls/xray-core/common/task"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/features/outbound"
	"github.com/xtls/xray-core/features/policy"
	"github.com/xtls/xray-core/features/stats"
	"github.com/xtls/xray-core/transport"
)

// Base is the part of an outbound handler that talks to Xray.
type Base struct {
	tag      string
	policy   policy.Session
	up, down stats.Counter
}

// NewBase reads the policy and traffic counters of the outbound tag from a
// created, not yet started Xray instance.
func NewBase(inst *core.Instance, tag string) (*Base, error) {
	pm, _ := inst.GetFeature(policy.ManagerType()).(policy.Manager)
	if pm == nil {
		return nil, stderrors.New("в ядре Xray нет менеджера политик")
	}
	b := &Base{tag: tag, policy: pm.ForLevel(0)}
	if sm, _ := inst.GetFeature(stats.ManagerType()).(stats.Manager); sm != nil {
		if pm.ForSystem().Stats.OutboundUplink {
			b.up, _ = stats.GetOrRegisterCounter(sm, "outbound>>>"+tag+">>>traffic>>>uplink")
		}
		if pm.ForSystem().Stats.OutboundDownlink {
			b.down, _ = stats.GetOrRegisterCounter(sm, "outbound>>>"+tag+">>>traffic>>>downlink")
		}
	}
	return b, nil
}

// Add adds the handler to the instance's outbounds.
func Add(inst *core.Instance, h outbound.Handler) error {
	ohm, _ := inst.GetFeature(outbound.ManagerType()).(outbound.Manager)
	if ohm == nil {
		return stderrors.New("в ядре Xray нет менеджера исходящих")
	}
	return ohm.AddHandler(context.Background(), h)
}

func (b *Base) Tag() string { return b.tag }

// Dispatch hands the connection to stream or packets by its network, and
// closes the link as Xray's outbounds do. name is the protocol for the log.
func (b *Base) Dispatch(ctx context.Context, link *transport.Link, name string,
	stream, packets func(context.Context, *transport.Link, net.Destination) error,
) {
	obs := session.OutboundsFromContext(ctx)
	ob := obs[len(obs)-1]
	ob.Name = name
	var err error
	switch {
	case !ob.Target.IsValid():
		err = stderrors.New("target not specified")
	case ob.Target.Network == net.Network_UDP:
		err = packets(ctx, link, ob.Target)
	default:
		err = stream(ctx, link, ob.Target)
	}
	if err != nil {
		errors.LogInfoInner(ctx, err, name, ": ", b.tag, " -> ", ob.Target)
		common.Interrupt(link.Writer)
	} else {
		common.Close(link.Writer)
	}
	common.Interrupt(link.Reader)
}

// Stream copies the link to conn and back until either side ends or the
// connection idles out. cancel ends ctx.
func (b *Base) Stream(ctx context.Context, cancel context.CancelFunc, link *transport.Link, conn gonet.Conn) error {
	timer := signal.CancelAfterInactivity(ctx, cancel, b.policy.Timeouts.ConnectionIdle)
	request := func() error {
		defer timer.SetTimeout(b.policy.Timeouts.DownlinkOnly)
		return buf.Copy(link.Reader, counted(buf.NewWriter(conn), b.up), buf.UpdateActivity(timer))
	}
	response := func() error {
		defer timer.SetTimeout(b.policy.Timeouts.UplinkOnly)
		return buf.Copy(buf.NewReader(conn), counted(link.Writer, b.down), buf.UpdateActivity(timer))
	}
	return task.Run(ctx, request, response)
}

// maxPacket is the largest UDP payload.
const maxPacket = 65535

// Packets copies the link's packets to pc and back. to turns a destination
// into the address pc takes, from turns a sender back. cancel ends ctx.
func (b *Base) Packets(ctx context.Context, cancel context.CancelFunc, link *transport.Link, pc gonet.PacketConn,
	target net.Destination, to func(net.Destination) gonet.Addr, from func(gonet.Addr) net.Destination,
) error {
	timer := signal.CancelAfterInactivity(ctx, cancel, b.policy.Timeouts.ConnectionIdle)
	go func() {
		<-ctx.Done()
		pc.Close()
	}()
	request := func() error {
		defer timer.SetTimeout(b.policy.Timeouts.DownlinkOnly)
		for {
			mb, err := link.Reader.ReadMultiBuffer()
			if err != nil {
				return err
			}
			timer.Update()
			for _, p := range mb {
				dst := target
				if p.UDP != nil {
					dst = *p.UDP
				}
				if _, err := pc.WriteTo(p.Bytes(), to(dst)); err != nil {
					buf.ReleaseMulti(mb)
					return err
				}
				if b.up != nil {
					b.up.Add(int64(p.Len()))
				}
			}
			buf.ReleaseMulti(mb)
		}
	}
	response := func() error {
		defer timer.SetTimeout(b.policy.Timeouts.UplinkOnly)
		scratch := make([]byte, maxPacket)
		for {
			n, addr, err := pc.ReadFrom(scratch)
			if err != nil {
				return err
			}
			timer.Update()
			p := buf.New()
			if n > int(p.Cap()) {
				p.Release()
				p = buf.NewWithSize(int32(n))
			}
			p.Write(scratch[:n])
			sender := from(addr)
			p.UDP = &sender
			if b.down != nil {
				b.down.Add(int64(n))
			}
			if err := link.Writer.WriteMultiBuffer(buf.MultiBuffer{p}); err != nil {
				return err
			}
		}
	}
	return task.Run(ctx, request, response)
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
