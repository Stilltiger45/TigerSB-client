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
	"github.com/Stilltiger45/TigerSB-client/internal/netauto"
	"github.com/Stilltiger45/TigerSB-client/internal/subscription"
	"github.com/Stilltiger45/TigerSB-client/internal/xrayconf"
	"github.com/Stilltiger45/TigerSB-client/pkg/profile"
)

// geodataMaxAge is how long downloaded lists stay fresh.
const geodataMaxAge = 24 * time.Hour

var (
	mu      sync.Mutex
	dataDir string
	inst    *core.Instance
)

// Init sets the app's private directory and version. Call it once before
// anything else.
func Init(dir, version string) {
	mu.Lock()
	defer mu.Unlock()
	dataDir = dir
	subscription.UserAgent = "TigerSB-android/" + version
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
	Name       string     `json:"name"`
	Updated    time.Time  `json:"updated"`
	Fetched    time.Time  `json:"fetched"`
	HasRU      bool       `json:"hasRU"`
	HasLists   bool       `json:"hasLists"`
	Foreign    bool       `json:"foreign"`
	Exits      []exitInfo `json:"exits"`
	DirectApps []string   `json:"directApps"`
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
	if err := geodata.Ensure(ctx, nil, assetDir(), p, geodataMaxAge); err != nil {
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
	if err := geodata.Ensure(ctx, nil, assetDir(), p, geodataMaxAge); err != nil {
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

// Start runs the VPN on the TUN file descriptor fd in mode "main" or "all".
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
	if xrayconf.HasGeoData(p) {
		// Normally AddKey or Refresh already fetched the lists; this only
		// downloads what is missing.
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		err := geodata.Ensure(ctx, nil, assetDir(), p, 0)
		cancel()
		if err != nil {
			return err
		}
		if err := engine.SetAssetDir(assetDir()); err != nil {
			return err
		}
	}
	cfg, err := xrayconf.Build(p, xrayconf.Options{
		Mode:     m,
		Inbounds: []xrayconf.Inbound{{Tag: "tun-in", Protocol: "tun", MTU: mtu}},
	})
	if err != nil {
		return err
	}
	// Xray's Android TUN reads the descriptor from this variable.
	if err := os.Setenv("XRAY_TUN_FD", strconv.Itoa(fd)); err != nil {
		return err
	}
	i, err := engine.Start(cfg)
	if err != nil {
		return err
	}
	inst = i
	return nil
}

// Stop stops the VPN. The app closes the TUN descriptor afterwards.
func Stop() error {
	mu.Lock()
	defer mu.Unlock()
	if inst == nil {
		return nil
	}
	err := inst.Close()
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
		DirectApps: p.Routing.Direct.Apps,
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
