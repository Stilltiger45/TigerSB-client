// Package meter reads what the app shows about a running VPN: the traffic that
// went through the servers and the ping of the server in use.
package meter

import (
	"context"
	"io"
	gonet "net"
	"net/http"
	"net/url"
	"strings"
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

// ChannelURLs are what Check requests through a server abroad: sites that
// are blocked or slowed down in Russia, so an answer shows that the VPN
// channel itself works, not only that the server is up.
var ChannelURLs = []string{"https://t.me/", "https://www.youtube.com/generate_204"}

// CheckURLs returns what Check requests through the outbound tag: the
// ChannelURLs through a server abroad, and probeURL (empty means
// profile.DefaultProbeURL) through an exit in Russia, where those are blocked.
func CheckURLs(tag, probeURL string) []string {
	ru := string(profile.RoleRU) + "/"
	if strings.HasPrefix(tag, ru) || strings.Contains(tag, ":"+ru) {
		if probeURL == "" {
			probeURL = profile.DefaultProbeURL
		}
		return []string{probeURL}
	}
	return ChannelURLs
}

// Check tells whether the channel through the outbound tag works and how
// fast: it requests every URL twice over one kept-alive connection, at once
// for all URLs, and returns the best time, which is the second request on an
// established TLS session unless the first was quicker. It returns -1 when
// no URL answered within timeout. Any HTTP answer counts, a redirect too.
func Check(inst *core.Instance, tag string, urls []string, timeout time.Duration) time.Duration {
	if tag == "" || len(urls) == 0 {
		return -1
	}
	dispatcher, _ := inst.GetFeature(routing.DispatcherType()).(routing.Dispatcher)
	if dispatcher == nil {
		return -1
	}
	ctx := context.WithValue(context.Background(), core.XrayKey(1), inst)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	results := make(chan time.Duration, len(urls))
	for _, u := range urls {
		go func() { results <- keepAlive(ctx, dispatcher, tag, u) }()
	}
	best := time.Duration(-1)
	for range urls {
		if d := <-results; d >= 0 && (best < 0 || d < best) {
			best = d
		}
	}
	return best
}

// keepAlive times two GETs of u through one connection and returns the
// quicker, or -1 when neither answered.
func keepAlive(ctx context.Context, dispatcher routing.Dispatcher, tag, u string) time.Duration {
	tr := &http.Transport{
		Proxy: func(*http.Request) (*url.URL, error) { return nil, nil },
		DialContext: func(ctx context.Context, network, addr string) (gonet.Conn, error) {
			dest, err := net.ParseDestination(network + ":" + addr)
			if err != nil {
				return nil, err
			}
			return tagged.Dialer(ctx, dispatcher, dest, tag)
		},
		MaxIdleConnsPerHost: 1,
	}
	defer tr.CloseIdleConnections()
	client := &http.Client{
		Transport:     tr,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	best := time.Duration(-1)
	for range 2 {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return -1
		}
		start := time.Now()
		resp, err := client.Do(req)
		if err != nil {
			break
		}
		// Read a page's start so the connection can carry the second
		// request; a longer one is cut and the second dials anew.
		io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		resp.Body.Close()
		if d := time.Since(start); best < 0 || d < best {
			best = d
		}
	}
	return best
}

// BulkURLs are what Bulk downloads through a server abroad: big answers
// from sites that stay up.
var BulkURLs = []string{"https://speed.cloudflare.com/__down?bytes=131072", "https://www.youtube.com/"}

// BulkSize is how much Bulk downloads: well past the 16 KB after which
// Russian DPI freezes a connection abroad, so a frozen channel, which still
// passes the small requests of Check, fails it.
const BulkSize = 96 << 10

// Bulk tells whether BulkSize bytes come through the outbound tag within
// timeout from any of urls, all requested at once. A URL whose whole answer
// is shorter tells nothing; when no URL tells anything Bulk says yes.
func Bulk(inst *core.Instance, tag string, urls []string, timeout time.Duration) bool {
	dispatcher, _ := inst.GetFeature(routing.DispatcherType()).(routing.Dispatcher)
	if tag == "" || dispatcher == nil {
		return false
	}
	ctx := context.WithValue(context.Background(), core.XrayKey(1), inst)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	results := make(chan int, len(urls))
	for _, u := range urls {
		go func() { results <- download(ctx, dispatcher, tag, u) }()
	}
	failed := false
	for range urls {
		switch <-results {
		case 1:
			return true
		case -1:
			failed = true
		}
	}
	return !failed
}

// download reads BulkSize bytes of u through tag: 1 when they came, 0 when
// the whole answer was shorter, -1 when it stopped or failed.
func download(ctx context.Context, dispatcher routing.Dispatcher, tag, u string) int {
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return -1
	}
	resp, err := (&http.Client{Transport: tr}).Do(req)
	if err != nil {
		return -1
	}
	defer resp.Body.Close()
	n, err := io.Copy(io.Discard, io.LimitReader(resp.Body, BulkSize))
	switch {
	case n >= BulkSize:
		return 1
	case err == nil:
		return 0
	}
	return -1
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
