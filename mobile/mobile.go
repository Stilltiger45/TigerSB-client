// Package mobile is the core API the Android app calls through gomobile bind.
// It keeps the same state as tigersb-core and runs Xray on the TUN that the
// app's VpnService opens.
//
// gomobile exports only simple types, so structured results are JSON strings.
package mobile

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/xtls/xray-core/core"

	"github.com/Stilltiger45/TigerSB-client/internal/engine"
	"github.com/Stilltiger45/TigerSB-client/internal/geodata"
	"github.com/Stilltiger45/TigerSB-client/internal/meter"
	"github.com/Stilltiger45/TigerSB-client/internal/netauto"
	"github.com/Stilltiger45/TigerSB-client/internal/smart"
	"github.com/Stilltiger45/TigerSB-client/internal/subscription"
	"github.com/Stilltiger45/TigerSB-client/internal/tuic"
	"github.com/Stilltiger45/TigerSB-client/internal/xrayconf"
	"github.com/Stilltiger45/TigerSB-client/pkg/profile"
)

// geodataMaxAge is how long downloaded lists stay fresh.
const geodataMaxAge = 24 * time.Hour

var (
	mu      sync.Mutex
	dataDir string
	inst    *core.Instance

	// What Stats needs about the running VPN.
	vpnTags, pingTags []string
	protocols         map[string]string
	probeURL          string
	started           time.Time
)

// Init sets the app's private directory and version. Call it once before
// anything else.
func Init(dir, version string) {
	mu.Lock()
	defer mu.Unlock()
	dataDir = dir
	subscription.UserAgent = "TigerSB-android/" + version
	startJournal()
}

// SetDevice tells subscription servers which device asks. Panels with a
// device limit (Remnawave HWID and similar) count devices by hwid.
func SetDevice(hwid, osName, osVersion, model string) {
	mu.Lock()
	defer mu.Unlock()
	subscription.ThisDevice = subscription.Device{HWID: hwid, OS: osName, OSVersion: osVersion, Model: model}
}

// profileInfo describes the imported profile for the app's screen.
type profileInfo struct {
	Name     string     `json:"name"`
	Updated  time.Time  `json:"updated"`
	Fetched  time.Time  `json:"fetched"`
	HasRU    bool       `json:"hasRU"`
	HasLists bool       `json:"hasLists"`
	Foreign  bool       `json:"foreign"`
	Exits    []exitInfo `json:"exits"`
	// DirectApps are left out of the VPN: ShieldApps, the panel's apps and
	// the user's own (UserApps).
	DirectApps []string `json:"directApps"`
	ShieldApps []string `json:"shieldApps"`
	UserApps   []string `json:"userApps"`
	// Key is the key the user imported, to copy and share.
	Key string `json:"key"`
}

// exitInfo is one exit with its transports (display names), best first.
type exitInfo struct {
	ID         string   `json:"id"`
	Role       string   `json:"role"`
	Transports []string `json:"transports"`
}

// AddKey adds a key (tigersb://, https:// or a share link), downloads the
// profile and the lists it needs, and returns the profile info as JSON.
func AddKey(key string) (string, error) {
	mu.Lock()
	defer mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	st, p, err := (&subscription.Client{}).Import(ctx, key)
	if err != nil {
		return "", err
	}
	if err := ensureGeodata(ctx, p); err != nil {
		return "", err
	}
	if err := subscription.Save(dataDir, st); err != nil {
		return "", err
	}
	return info(st, p)
}

// Refresh downloads the profile and the lists again. On failure the old
// profile stays usable.
func Refresh() (string, error) {
	mu.Lock()
	defer mu.Unlock()
	st, err := subscription.Load(dataDir)
	if err != nil {
		return "", noKey(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	p, err := (&subscription.Client{}).Refresh(ctx, st)
	if err != nil {
		return "", err
	}
	if err := subscription.Save(dataDir, st); err != nil {
		return "", err
	}
	if err := ensureGeodata(ctx, p); err != nil {
		return "", err
	}
	return info(st, p)
}

// Current returns the profile info as JSON, or "" when no key has been added.
func Current() (string, error) {
	mu.Lock()
	defer mu.Unlock()
	st, err := subscription.Load(dataDir)
	if errors.Is(err, subscription.ErrNoState) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	p, err := st.Open()
	if err != nil {
		return "", err
	}
	return info(st, p)
}

// Forget removes the key, the profile and the lists.
func Forget() error {
	mu.Lock()
	defer mu.Unlock()
	if inst != nil {
		return errors.New("сначала отключите VPN")
	}
	if err := os.RemoveAll(assetDir()); err != nil {
		return err
	}
	err := os.Remove(filepath.Join(dataDir, "state.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// Start runs the VPN on the TUN file descriptor fd in mode "auto", "main" or "all".
// mtu is the MTU the app gave the TUN (see PickMTU).
func Start(fd int, mode string, mtu int) error {
	mu.Lock()
	defer mu.Unlock()
	if inst != nil {
		return errors.New("VPN уже запущен")
	}
	m, err := xrayconf.ParseMode(mode)
	if err != nil {
		return err
	}
	st, err := subscription.Load(dataDir)
	if err != nil {
		return noKey(err)
	}
	p, err := st.Open()
	if err != nil {
		return err
	}
	if needed, extra := xrayconf.GeoData(p, m); needed {
		// Normally AddKey or Refresh already fetched the lists; this only
		// downloads what is missing.
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		err := geodata.Ensure(ctx, nil, assetDir(), p, 0, extra...)
		cancel()
		if err != nil {
			return err
		}
		if err := engine.SetAssetDir(assetDir()); err != nil {
			return err
		}
	}
	trim(filepath.Join(dataDir, xrayLog))
	cfg, err := xrayconf.Build(p, xrayconf.Options{
		Mode:     m,
		Inbounds: []xrayconf.Inbound{{Tag: "tun-in", Protocol: "tun", MTU: mtu}},
		LogFile:  filepath.Join(dataDir, xrayLog),
	})
	if err != nil {
		return err
	}
	// Xray's Android TUN reads the descriptor from this variable.
	if err := os.Setenv("XRAY_TUN_FD", strconv.Itoa(fd)); err != nil {
		return err
	}
	i, err := engine.Start(cfg, append(tuic.Hooks(p), smart.Hooks(p, m)...)...)
	if err != nil {
		return err
	}
	inst = i
	vpnTags, pingTags = xrayconf.Candidates(p)
	protocols = xrayconf.Protocols(p)
	probeURL = ""
	if p.Probe != nil {
		probeURL = p.Probe.URL
	}
	started = time.Now()
	return nil
}

// Stats returns what the app shows about the running VPN as JSON:
// {"up", "down": bytes through the servers since the start, "ping": ms to
// the server in use or -1, "uptime": seconds since the start}. The ping takes
// up to 5 seconds, so call it off the main thread.
func Stats() (string, error) {
	mu.Lock()
	i, all, def, url, since := inst, vpnTags, pingTags, probeURL, started
	mu.Unlock()
	if i == nil {
		return "", errors.New("VPN не запущен")
	}
	up, down := meter.Traffic(i, all)
	ping := int64(-1)
	if d := meter.Ping(i, def, url, 5*time.Second); d >= 0 {
		ping = d.Milliseconds()
	}
	b, err := json.Marshal(map[string]int64{
		"up": up, "down": down, "ping": ping, "uptime": int64(time.Since(since).Seconds()),
	})
	return string(b), err
}

// Healthy checks that a site opens through the server in use, within 6
// seconds. False when the VPN is not running or the request fails.
func Healthy() bool {
	mu.Lock()
	i, def, url := inst, pingTags, probeURL
	mu.Unlock()
	if i == nil {
		return false
	}
	if len(def) == 0 {
		return true // nothing to check through: everything goes direct
	}
	return meter.Ping(i, def, url, 6*time.Second) >= 0
}

// Protocol names the protocol of the server in use, such as
// "VLESS · Reality · gRPC", or "" when the VPN is not running.
func Protocol() string {
	mu.Lock()
	defer mu.Unlock()
	if inst == nil || len(pingTags) == 0 {
		return ""
	}
	return protocols[meter.Fastest(inst, pingTags)]
}

// SetStateKey sets the 32-byte key that encrypts the saved subscription (see
// subscription.StateKey) and encrypts a state saved without it. Call it right
// after Init.
func SetStateKey(key []byte) error {
	mu.Lock()
	defer mu.Unlock()
	subscription.StateKey = slices.Clone(key)
	st, err := subscription.Load(dataDir)
	if errors.Is(err, subscription.ErrNoState) {
		return nil
	}
	if err != nil {
		return err
	}
	return subscription.Save(dataDir, st)
}

// SetUserApps saves the apps the user leaves out of the VPN, a JSON array of
// Android package names. A running VPN applies them when it reconnects.
func SetUserApps(apps string) error {
	var list []string
	if err := json.Unmarshal([]byte(apps), &list); err != nil {
		return fmt.Errorf("список приложений: %w", err)
	}
	mu.Lock()
	defer mu.Unlock()
	b, _ := json.Marshal(list)
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dataDir, userAppsFile), b, 0o600)
}

const userAppsFile = "apps.json"

// userApps reads what SetUserApps saved; none when nothing was.
func userApps() []string {
	var list []string
	if b, err := os.ReadFile(filepath.Join(dataDir, userAppsFile)); err == nil {
		json.Unmarshal(b, &list)
	}
	return list
}

// Stop stops the VPN. The app closes the TUN descriptor afterwards.
func Stop() error {
	mu.Lock()
	defer mu.Unlock()
	if inst == nil {
		return nil
	}
	err := engine.Stop(inst)
	inst = nil
	return err
}

// Running reports whether the VPN is running.
func Running() bool {
	mu.Lock()
	defer mu.Unlock()
	return inst != nil
}

// PickMTU chooses the TUN MTU for the network the phone uses now: transport
// is "wifi", "cellular", "ethernet" or "", linkMTU is what the system reports
// for that network (0 if unknown).
func PickMTU(transport string, linkMTU int) int {
	return netauto.MTU(netauto.Transport(transport), linkMTU)
}

func assetDir() string { return filepath.Join(dataDir, "geodata") }

// ensureGeodata fetches the profile's lists and, if it can, the basic mode's
// Russian IPs, so the mode can be switched without a download. Only the
// profile's own lists are required here.
func ensureGeodata(ctx context.Context, p *profile.Profile) error {
	if geodata.Ensure(ctx, nil, assetDir(), p, geodataMaxAge, xrayconf.BasicDirectIPs...) == nil {
		return nil
	}
	return geodata.Ensure(ctx, nil, assetDir(), p, geodataMaxAge)
}

func noKey(err error) error {
	if errors.Is(err, subscription.ErrNoState) {
		return errors.New("ключ ещё не добавлен")
	}
	return err
}

func info(st *subscription.State, p *profile.Profile) (string, error) {
	blocked := p.Routing.Blocked
	in := profileInfo{
		Name:       p.Name,
		Updated:    p.Updated,
		Fetched:    st.Fetched,
		HasRU:      p.HasRole(profile.RoleRU),
		HasLists:   len(blocked.Domains)+len(blocked.IPs) > 0,
		Foreign:    st.Foreign,
		ShieldApps: xrayconf.ShieldApps,
		UserApps:   userApps(),
		Key:        st.Key,
	}
	for _, a := range slices.Concat(in.ShieldApps, p.Routing.Direct.Apps, in.UserApps) {
		if !slices.Contains(in.DirectApps, a) {
			in.DirectApps = append(in.DirectApps, a)
		}
	}
	if in.Name == "" {
		in.Name = p.ID
	}
	for _, e := range p.Exits {
		ei := exitInfo{ID: e.ID, Role: string(e.Role)}
		ts := slices.SortedStableFunc(slices.Values(e.Transports), func(a, b profile.Transport) int { return a.Prio - b.Prio })
		for _, t := range ts {
			ei.Transports = append(ei.Transports, cmp.Or(t.Name, t.ID))
		}
		in.Exits = append(in.Exits, ei)
	}
	b, err := json.Marshal(in)
	if err != nil {
		return "", fmt.Errorf("info: %w", err)
	}
	return string(b), nil
}
