// Package meter reads what the app shows about a running VPN: the traffic that
// went through the servers and the ping of the server in use.
package meter

import (
	"context"
	gonet "net"
	"net/http"
	"net/url"
	"time"

	"github.com/xtls/xray-core/app/observatory"
	"github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/features/extension"
	"github.com/xtls/xray-core/features/routing"
	"github.com/xtls/xray-core/features/stats"
	"github.com/xtls/xray-core/transport/internet/tagged"

	"github.com/Stilltiger45/TigerSB-client/pkg/profile"
)

// Traffic returns the bytes the outbounds sent and received since Xray
// started. The config must turn on outbound stats (xrayconf does).
func Traffic(inst *core.Instance, tags []string) (up, down int64) {
	m, _ := inst.GetFeature(stats.ManagerType()).(stats.Manager)
	if m == nil {
		return 0, 0
	}
	for _, t := range tags {
		if c := m.GetCounter("outbound>>>" + t + ">>>traffic>>>uplink"); c != nil {
			up += c.Value()
		}
		if c := m.GetCounter("outbound>>>" + t + ">>>traffic>>>downlink"); c != nil {
			down += c.Value()
		}
	}
	return up, down
}

// Ping times one request to probeURL (empty means profile.DefaultProbeURL)
// through the outbound tag. It returns -1 when the request fails.
func Ping(inst *core.Instance, tag string, probeURL string, timeout time.Duration) time.Duration {
	if tag == "" {
		return -1
	}
	dispatcher, _ := inst.GetFeature(routing.DispatcherType()).(routing.Dispatcher)
	if dispatcher == nil {
		return -1
	}
	if probeURL == "" {
		probeURL = profile.DefaultProbeURL
	}

	// Xray dials only from a context that carries its instance; its own
	// tests build one the same way.
	ctx := context.WithValue(context.Background(), core.XrayKey(1), inst)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	tr := &http.Transport{
		Proxy: func(*http.Request) (*url.URL, error) { return nil, nil },
		DialContext: func(ctx context.Context, network, addr string) (gonet.Conn, error) {
			dest, err := net.ParseDestination(network + ":" + addr)
			if err != nil {
				return nil, err
			}
			return tagged.Dialer(ctx, dispatcher, dest, tag)
		},
		DisableKeepAlives: true,
	}
	defer tr.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, probeURL, nil)
	if err != nil {
		return -1
	}
	start := time.Now()
	resp, err := (&http.Client{Transport: tr}).Do(req)
	if err != nil {
		return -1
	}
	resp.Body.Close()
	return time.Since(start)
}

// Fastest returns the live outbound of tags with the lowest delay the
// observatory measured, or the first one before any measurement. tags must
// not be empty.
func Fastest(inst *core.Instance, tags []string) string {
	best, delay := tags[0], int64(-1)
	obs, _ := inst.GetFeature(extension.ObservatoryType()).(extension.Observatory)
	if obs == nil {
		return best
	}
	m, err := obs.GetObservation(context.Background())
	res, ok := m.(*observatory.ObservationResult)
	if err != nil || !ok {
		return best
	}
	alive := map[string]int64{}
	for _, s := range res.Status {
		if s.Alive {
			alive[s.OutboundTag] = s.Delay
		}
	}
	for _, t := range tags {
		if d, ok := alive[t]; ok && (delay < 0 || d < delay) {
			best, delay = t, d
		}
	}
	return best
}
