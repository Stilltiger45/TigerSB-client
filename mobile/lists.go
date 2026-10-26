package mobile

import (
	"context"
	"errors"
	gonet "net"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/core"

	"github.com/Stilltiger45/TigerSB-client/internal/geodata"
	"github.com/Stilltiger45/TigerSB-client/internal/xrayconf"
	"github.com/Stilltiger45/TigerSB-client/pkg/profile"
)

// The routing lists (geosite.dat, geoip.dat) weigh tens of megabytes on
// GitHub, which a weak or filtered mobile network may not deliver. So nothing
// waits for them: the VPN starts without the lists it lacks, they are fetched
// in the background, through the VPN once it runs, and the app reconnects
// when they are in (TakeRestart).

var (
	// fetchMu lets one download of the lists run at a time.
	fetchMu sync.Mutex
	// restartWanted is set when the lists the running core started without
	// have arrived.
	restartWanted atomic.Bool
)

// inBackground runs the downloads of the lists; tests replace it so that
// nothing writes into their folders after they end.
var inBackground = func(f func()) { go f() }

// TakeRestart reports, once, that the routing lists the VPN started without
// have been downloaded, so the app should reconnect to use them.
func TakeRestart() bool {
	return restartWanted.Swap(false)
}

// fetchLists downloads the lists the profile and the basic mode use, retrying
// with growing pauses. With i, the download goes through that core, and only
// while it runs. need, when not nil, are the extra lists the core i started
// without: once they and the profile's are in, the app is asked to reconnect.
func fetchLists(i *core.Instance, p *profile.Profile, maxAge time.Duration, need []string) {
	fetchMu.Lock()
	defer fetchMu.Unlock()
	var client *http.Client
	if i != nil {
		client = viaCore(i)
	}
	pause := 20 * time.Second
	for attempt := 1; attempt <= 6; attempt++ {
		if i != nil && !running(i) {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		err := ensureGeodata(ctx, client, p, maxAge)
		cancel()
		if err == nil && need != nil && !geodata.Ready(assetDir(), p, need...) {
			err = errors.New("не хватает списков режима")
		}
		if err == nil {
			if need != nil && running(i) {
				Note("списки маршрутизации скачаны, переподключаю")
				restartWanted.Store(true)
			}
			return
		}
		if attempt == 1 || attempt == 6 {
			Note("списки маршрутизации: " + err.Error())
		}
		// A new core fetches for itself: this one stops waiting once its
		// core is gone.
		for end := time.Now().Add(pause); time.Now().Before(end); time.Sleep(2 * time.Second) {
			if i != nil && !running(i) {
				return
			}
		}
		pause *= 2
	}
}

// running reports whether i is the core in use now.
func running(i *core.Instance) bool {
	mu.Lock()
	defer mu.Unlock()
	return i != nil && inst == i
}

// viaCore returns an HTTP client whose connections go through the core's
// routing, like any app's: through the VPN where the mode sends the site.
func viaCore(i *core.Instance) *http.Client {
	return &http.Client{Transport: &http.Transport{
		Proxy: func(*http.Request) (*url.URL, error) { return nil, nil },
		DialContext: func(ctx context.Context, network, addr string) (gonet.Conn, error) {
			dest, err := xnet.ParseDestination(network + ":" + addr)
			if err != nil {
				return nil, err
			}
			return core.Dial(ctx, i, dest)
		},
		ForceAttemptHTTP2: true,
	}}
}

// ensureGeodata fetches the profile's lists and, if it can, the basic mode's
// Russian IPs, so the mode can be switched without a download. Only the
// profile's own lists are required here.
func ensureGeodata(ctx context.Context, c *http.Client, p *profile.Profile, maxAge time.Duration) error {
	if geodata.Ensure(ctx, c, assetDir(), p, maxAge, xrayconf.BasicDirectIPs...) == nil {
		return nil
	}
	return geodata.Ensure(ctx, c, assetDir(), p, maxAge)
}
