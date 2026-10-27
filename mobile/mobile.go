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
	"runtime"
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
	"github.com/Stilltiger45/TigerSB-client/internal/steady"
	"github.com/Stilltiger45/TigerSB-client/internal/subscription"
	"github.com/Stilltiger45/TigerSB-client/internal/tuic"
	"github.com/Stilltiger45/TigerSB-client/internal/xrayconf"
	"github.com/Stilltiger45/TigerSB-client/pkg/profile"
)

// geodataMaxAge is how long downloaded lists stay fresh.
const geodataMaxAge = 24 * time.Hour

var (
	mu sync.Mutex
	// netMu lets one AddKey or Refresh download at a time, without holding
	// mu over the network.
	netMu      sync.Mutex
	dataDir    string
	appVersion string
	inst       *core.Instance
	// network is the kind of network the phone is on (SetNetwork).
	network string

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
	appVersion = version
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
	// ID names the profile among the saved ones.
	ID       string     `json:"id"`
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
	// Signed marks a profile from a TigerSB panel whose signature the app
	// checked (a tigersb:// key): the app shows a check mark by its name.
	Signed bool `json:"signed"`
}

// exitInfo is one exit with its transports (display names), best first.
type exitInfo struct {
	ID         string   `json:"id"`
	Role       string   `json:"role"`
	Transports []string `json:"transports"`
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
	st, _, err := loadActive()
	if err != nil {
		return noKey(err)
	}
	p, err := st.Open()
	if err != nil {
		return err
	}
	// Normally AddKey or Refresh already fetched the lists. When they are
	// missing, the VPN starts without them and fetches them through itself.
	needed, extra := xrayconf.GeoData(p, m)
	noGeo := needed && !geodata.Ready(assetDir(), p, extra...)
	if needed && !noGeo {
		if err := engine.SetAssetDir(assetDir()); err != nil {
			return err
		}
	}
	trim(filepath.Join(dataDir, xrayLog))
	var ev *smart.Evasion
	if m == xrayconf.ModeAuto {
		ev = smart.NewEvasion(p, savedPresets()[network])
	}
	cfg, err := xrayconf.Build(p, xrayconf.Options{
		Mode:     m,
		Inbounds: []xrayconf.Inbound{{Tag: "tun-in", Protocol: "tun", MTU: mtu}},
		LogFile:  filepath.Join(dataDir, xrayLog),
		Evasion:  ev != nil,
		NoGeo:    noGeo,
	})
	if err != nil {
		return err
	}
	// Xray's Android TUN reads the descriptor from this variable.
	if err := os.Setenv("XRAY_TUN_FD", strconv.Itoa(fd)); err != nil {
		return err
	}
	i, err := engine.Start(cfg, slices.Concat(tuic.Hooks(p, xrayconf.QUICNoise), steady.Hooks(p), smart.Hooks(p, m, ev))...)
	if err != nil {
		return err
	}
	inst = i
	if ev != nil {
		go tune(i, network)
	}
	if noGeo {
		Note("списков маршрутизации ещё нет: подключаю без них и скачиваю через VPN")
		need := append([]string{}, extra...)
		inBackground(func() { fetchLists(i, p, 0, need) })
	}
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
	tag := inUse(i, def)
	if d := meter.Check(i, tag, meter.CheckURLs(xrayconf.BaseTag(tag), url), 5*time.Second); d >= 0 {
		ping = d.Milliseconds()
	}
	b, err := json.Marshal(map[string]int64{
		"up": up, "down": down, "ping": ping, "uptime": int64(time.Since(since).Seconds()),
	})
	return string(b), err
}

// Healthy checks the channel through the server in use like the app's ping:
// t.me and YouTube through a server abroad, two requests each over one
// connection, within 5 seconds. False when the VPN is not running or nothing
// answers.
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
	tag := inUse(i, def)
	return meter.Check(i, tag, meter.CheckURLs(xrayconf.BaseTag(tag), url), 5*time.Second) >= 0
}

// inUse returns the outbound most traffic goes through now: the smart
// picker's choice for traffic without a domain, the transport a pick
// outbound keeps, or the fastest of def.
func inUse(i *core.Instance, def []string) string {
	if t := smart.InUse(i); t != "" {
		return t
	}
	for _, role := range []profile.Role{profile.RoleForeign, profile.RoleRU} {
		if t := steady.Current(i, xrayconf.PickTag(role)); t != "" {
			return t
		}
	}
	return meter.Fastest(i, def)
}

// About returns, as JSON, the versions the app's about screen shows: the app
// itself, the Xray core and Go.
func About() string {
	mu.Lock()
	v := appVersion
	mu.Unlock()
	b, _ := json.Marshal(map[string]string{"version": v, "xray": core.Version(), "go": runtime.Version()})
	return string(b)
}

// Protocol names the protocol of the server in use, such as
// "VLESS · Reality · gRPC", or "" when the VPN is not running.
func Protocol() string {
	mu.Lock()
	defer mu.Unlock()
	if inst == nil || len(pingTags) == 0 {
		return ""
	}
	tag := inUse(inst, pingTags)
	name := protocols[xrayconf.BaseTag(tag)]
	if tag != xrayconf.BaseTag(tag) && name != "" {
		name += " · обход DPI"
	}
	return name
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

// SetNetwork tells the core what network the phone is on now: "wifi",
// "cellular", "ethernet" or "". Call it before Start: the smart mode
// remembers the DPI preset that worked on each kind of network.
func SetNetwork(transport string) {
	mu.Lock()
	defer mu.Unlock()
	network = transport
}

// tune picks the DPI preset for the network the core started on, notes the
// result in the journal and remembers it for the next start there.
func tune(i *core.Instance, kind string) {
	tuneMu.Lock()
	res, err := smart.Tune(i, 8*time.Second)
	tuneMu.Unlock()
	mu.Lock()
	defer mu.Unlock()
	if inst != i || err != nil || network != kind {
		return // stopped, restarted or moved to another network meanwhile
	}
	Note("обход DPI (" + cmp.Or(kind, "сеть ?") + "): " + res.String())
	saved := savedPresets()
	if saved[kind] != res.Preset {
		saved[kind] = res.Preset
		b, _ := json.Marshal(saved)
		os.WriteFile(filepath.Join(dataDir, presetsFile), b, 0o600)
	}
}

// tuneMu lets one tuning run at a time: a quick change of networks starts a
// new one while the last still runs.
var tuneMu sync.Mutex

const presetsFile = "dpi.json"

// savedPresets reads the DPI preset that worked last on each kind of network.
func savedPresets() map[string]int {
	m := map[string]int{}
	if b, err := os.ReadFile(filepath.Join(dataDir, presetsFile)); err == nil {
		json.Unmarshal(b, &m)
	}
	return m
}

// PickMTU chooses the TUN MTU for the network the phone uses now: transport
// is "wifi", "cellular", "ethernet" or "", linkMTU is what the system reports
// for that network (0 if unknown). It measures how large a packet reaches the
// server without being split (netauto.PathMTU), which takes up to a few
// seconds, so call it off the main thread, before Start or SwitchNetwork:
// the connections dialled from then on are tuned for this network (the LTE
// mode on mobile data, segments sized to the path).
func PickMTU(transport string, linkMTU int) int {
	t := netauto.Transport(transport)
	path := pathMTU(t)
	mtu := netauto.MTU(t, linkMTU, path)
	engine.SetLink(engine.Link{Mobile: t == netauto.Cellular, MSS: netauto.MSS(path)})
	return mtu
}

var (
	linkMu sync.Mutex
	// lastPath is the last measurement, reused for a while on the same
	// network and server.
	lastPath struct {
		key  string
		at   time.Time
		size int
	}
)

// PathKnown reports whether the last PickMTU measured the path. An
// unmeasured one, as on mobile networks that drop pings, only guesses a
// safe MTU, which is no reason to rebuild a working tunnel.
func PathKnown() bool {
	linkMu.Lock()
	defer linkMu.Unlock()
	return lastPath.size > 0
}

// pathReuse is how long a measured path stays trusted.
const pathReuse = 5 * time.Minute

// pathFallback answers pings from Russia when the server does not: the path to
// it shows at least what the phone's own network carries.
const pathFallback = "77.88.8.8"

// pathMTU measures the path to the profile's server, or to pathFallback when
// the server drops pings; 0 when neither answers.
func pathMTU(t netauto.Transport) int {
	mu.Lock()
	host := ""
	if st, _, err := loadActive(); err == nil {
		if p, err := st.Open(); err == nil {
			host = xrayconf.ServerHost(p)
		}
	}
	mu.Unlock()
	key := string(t) + " " + host
	linkMu.Lock()
	if lastPath.key == key && time.Since(lastPath.at) < pathReuse {
		size := lastPath.size
		linkMu.Unlock()
		return size
	}
	linkMu.Unlock()

	// Both paths are measured at once: a reconnect waits for this.
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	fallback := make(chan int, 1)
	go func() { fallback <- netauto.PathMTU(ctx, pathFallback) }()
	size, to := 0, "до сервера"
	if host != "" {
		size = netauto.PathMTU(ctx, host)
	}
	if size == 0 {
		size, to = <-fallback, "до Яндекса"
	}
	kind := cmp.Or(string(t), "сеть ?")
	if t == netauto.Cellular {
		kind += ", режим LTE"
	}
	if size > 0 {
		Note(fmt.Sprintf("%s: путь %s пропускает %d байт без дробления", kind, to, size))
	} else {
		Note(kind + ": путь не измерить, ping не отвечает")
	}
	linkMu.Lock()
	lastPath.key, lastPath.at, lastPath.size = key, time.Now(), size
	linkMu.Unlock()
	return size
}

func assetDir() string { return filepath.Join(dataDir, "geodata") }

func noKey(err error) error {
	if errors.Is(err, subscription.ErrNoState) {
		return errors.New("ключ ещё не добавлен")
	}
	return err
}

func info(id string, st *subscription.State, p *profile.Profile) (string, error) {
	blocked := p.Routing.Blocked
	in := profileInfo{
		ID:         id,
		Name:       p.Name,
		Updated:    p.Updated,
		Fetched:    st.Fetched,
		HasRU:      p.HasRole(profile.RoleRU),
		HasLists:   len(blocked.Domains)+len(blocked.IPs) > 0,
		Foreign:    st.Foreign,
		ShieldApps: xrayconf.ShieldApps,
		UserApps:   userApps(),
		Key:        st.Key,
		Signed:     st.PK != "",
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
