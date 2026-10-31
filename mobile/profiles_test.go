package mobile

import (
	"crypto/ed25519"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/xtls/xray-core/core"

	"github.com/Stilltiger45/TigerSB-client/internal/subscription"
	"github.com/Stilltiger45/TigerSB-client/pkg/profile"
)

func vlessKey(name string) string {
	return "vless://5783a3e7-e373-51cd-8642-c83782b807c5@1.2.3.4:443?security=none#" + name
}

func add(t *testing.T, key string) profileInfo {
	t.Helper()
	s, err := AddKey(key)
	if err != nil {
		t.Fatal(err)
	}
	var in profileInfo
	if err := json.Unmarshal([]byte(s), &in); err != nil || in.ID == "" {
		t.Fatalf("info %s: %v", s, err)
	}
	return in
}

// list returns the names of the saved profiles and the active one's.
func list(t *testing.T) (names []string, active string) {
	t.Helper()
	s, err := Profiles()
	if err != nil {
		t.Fatal(err)
	}
	var all []struct {
		profileInfo
	}
	if err := json.Unmarshal([]byte(s), &all); err != nil {
		t.Fatal(err)
	}
	cur, _ := Current()
	var c profileInfo
	json.Unmarshal([]byte(cur), &c)
	for _, p := range all {
		names = append(names, p.Name)
	}
	return names, c.Name
}

// TestProfiles checks several profiles: each key adds one and makes it the
// one in use, the same key again updates its profile, and any can be picked,
// updated or deleted.
func TestProfiles(t *testing.T) {
	initTemp(t)
	if s, err := Profiles(); err != nil || s != "[]" {
		t.Fatalf("no profiles: %q %v", s, err)
	}
	home := add(t, vlessKey("Дом"))
	work := add(t, vlessKey("Работа"))
	if names, active := list(t); len(names) != 2 || names[0] != "Дом" || names[1] != "Работа" || active != "Работа" {
		t.Fatalf("profiles %v, active %q", names, active)
	}
	if again := add(t, vlessKey("Дом")); again.ID != home.ID {
		t.Errorf("the same key made another profile")
	}
	if names, active := list(t); len(names) != 2 || active != "Дом" {
		t.Fatalf("after adding the same key: %v, active %q", names, active)
	}

	if _, err := Select(work.ID); err != nil {
		t.Fatal(err)
	}
	if _, active := list(t); active != "Работа" {
		t.Fatalf("active %q after select", active)
	}
	if _, err := Select("nope"); err == nil {
		t.Error("selected a profile that does not exist")
	}

	// The active one goes: the first left takes over.
	next, err := Delete(work.ID)
	if err != nil {
		t.Fatal(err)
	}
	var in profileInfo
	json.Unmarshal([]byte(next), &in)
	if in.ID != home.ID {
		t.Fatalf("after deleting the active profile: %s", next)
	}
	os.MkdirAll(assetDir(), 0o700)
	if next, err := Delete(home.ID); err != nil || next != "" {
		t.Fatalf("delete the last: %q %v", next, err)
	}
	if _, err := os.Stat(assetDir()); !os.IsNotExist(err) {
		t.Error("the lists stay after the last profile went")
	}
	if s, _ := Current(); s != "" {
		t.Errorf("current %q with no profiles", s)
	}
}

// TestDeleteActiveWhileRunning checks that the profile in use cannot go
// while the VPN runs on it.
func TestDeleteActiveWhileRunning(t *testing.T) {
	initTemp(t)
	in := add(t, vlessKey("Дом"))
	mu.Lock()
	inst = new(core.Instance) // only its presence matters here
	mu.Unlock()
	t.Cleanup(func() { mu.Lock(); inst = nil; mu.Unlock() })
	if _, err := Delete(in.ID); err == nil {
		t.Fatal("deleted the profile the VPN runs on")
	}
}

// TestOldProfileMoves checks that the one profile an older version kept is
// found after the update.
func TestOldProfileMoves(t *testing.T) {
	dir := initTemp(t)
	st, _, err := (&subscription.Client{}).Import(t.Context(), vlessKey("Старый"))
	if err != nil {
		t.Fatal(err)
	}
	if err := subscription.Save(dir, st); err != nil {
		t.Fatal(err)
	}
	if names, active := list(t); len(names) != 1 || active != "Старый" {
		t.Fatalf("old profile: %v, active %q", names, active)
	}
	if _, err := os.Stat(filepath.Join(dir, "state.json")); !os.IsNotExist(err) {
		t.Error("the old file stayed")
	}
}

// TestKeyFromOtherPanel checks that a tigersb:// key signed by a panel no
// saved profile comes from is told apart, and that AddKeyAside leaves the
// profile in use as it is.
func TestKeyFromOtherPanel(t *testing.T) {
	initTemp(t)
	ours, _, _ := profile.GenerateKey()
	theirs, _, _ := profile.GenerateKey()
	key := func(pk ed25519.PublicKey) string {
		return profile.Key{Kind: profile.KindSubscription, URL: "https://panel.example/sub/x", PK: pk}.String()
	}
	if OtherPanel(key(theirs)) {
		t.Error("other panel with no profiles to compare with")
	}
	add(t, vlessKey("Дом"))
	mu.Lock()
	ix, _ := readIndex()
	ix.Order = append(ix.Order, "panel")
	writeIndex(ix)
	subscription.Save(profileDir("panel"), &subscription.State{Key: key(ours), PK: profile.EncodeKey(ours)})
	mu.Unlock()
	if OtherPanel(key(ours)) {
		t.Error("a key of a saved panel called other")
	}
	if !OtherPanel(key(theirs)) {
		t.Error("a key of another panel not told apart")
	}
	if OtherPanel(vlessKey("x")) {
		t.Error("a share link called another panel")
	}

	if _, err := AddKeyAside(vlessKey("Чужой")); err != nil {
		t.Fatal(err)
	}
	if names, active := list(t); len(names) != 2 || names[1] != "Чужой" || active != "Дом" {
		t.Fatalf("after AddKeyAside: %v, active %q", names, active)
	}
}

// TestFirstKeyAside: with no profile in use, the key added aside becomes it.
func TestFirstKeyAside(t *testing.T) {
	initTemp(t)
	if _, err := AddKeyAside(vlessKey("Первый")); err != nil {
		t.Fatal(err)
	}
	if _, active := list(t); active != "Первый" {
		t.Fatalf("active %q", active)
	}
}
