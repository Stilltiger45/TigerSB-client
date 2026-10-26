package mobile

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
	"testing"
)

func TestImportCurrentForget(t *testing.T) {
	initTemp(t)
	if s, err := Current(); err != nil || s != "" {
		t.Fatalf("empty state: %q %v", s, err)
	}
	if err := Start(3, "main", 1500); err == nil || !strings.Contains(err.Error(), "ключ") {
		t.Errorf("start without a key: %v", err)
	}
	key := "vless://5783a3e7-e373-51cd-8642-c83782b807c5@1.2.3.4:443?security=none#Дом"
	s, err := AddKey(key)
	if err != nil {
		t.Fatal(err)
	}
	var in profileInfo
	if err := json.Unmarshal([]byte(s), &in); err != nil || in.Name != "Дом" || len(in.Exits) != 1 || in.HasRU || in.Key != key || in.Signed {
		t.Fatalf("info %s: %v", s, err)
	}
	if again, _ := Current(); again != s {
		t.Errorf("current %s, want %s", again, s)
	}
	if err := Start(3, "smart", 1500); err == nil {
		t.Error("unknown mode accepted")
	}
	if err := Forget(); err != nil {
		t.Fatal(err)
	}
	if s, _ := Current(); s != "" {
		t.Errorf("forgotten state still there: %s", s)
	}
}

func TestAppsOutsideVPN(t *testing.T) {
	initTemp(t)
	if _, err := AddKey("vless://5783a3e7-e373-51cd-8642-c83782b807c5@1.2.3.4:443?security=none#Дом"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { Forget() })
	if err := SetUserApps(`["org.example.game","ru.oneme.app"]`); err != nil {
		t.Fatal(err)
	}
	s, _ := Current()
	var in profileInfo
	json.Unmarshal([]byte(s), &in)
	if !slices.Contains(in.DirectApps, "ru.oneme.app") || !slices.Contains(in.DirectApps, "org.example.game") ||
		!slices.Equal(in.UserApps, []string{"org.example.game", "ru.oneme.app"}) || len(in.ShieldApps) == 0 {
		t.Fatalf("info %s", s)
	}
	seen := map[string]bool{}
	for _, a := range in.DirectApps {
		if seen[a] {
			t.Errorf("%s twice", a)
		}
		seen[a] = true
	}
	if err := SetUserApps("nonsense"); err == nil {
		t.Error("bad list accepted")
	}
}

func TestStateKeyEncryptsSavedKey(t *testing.T) {
	dir := initTemp(t)
	t.Cleanup(func() { SetStateKey(nil); Forget() })
	key := "vless://5783a3e7-e373-51cd-8642-c83782b807c5@1.2.3.4:443?security=none#secret-name"
	if _, err := AddKey(key); err != nil {
		t.Fatal(err)
	}
	if err := SetStateKey(bytes.Repeat([]byte{1}, 32)); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "state.json"))
	if bytes.Contains(raw, []byte("secret-name")) {
		t.Fatal("key saved in the clear")
	}
	if s, err := Current(); err != nil || !strings.Contains(s, "secret-name") {
		t.Fatalf("current %q %v", s, err)
	}
}

func TestJournal(t *testing.T) {
	dir := initTemp(t)
	if j := Journal(); j != "Журнал пуст" {
		t.Errorf("empty journal: %q", j)
	}
	Note("сторож: переподключаю")
	os.WriteFile(filepath.Join(dir, xrayLog), []byte("warning one\nwarning two\n"), 0o600)
	j := Journal()
	if !strings.Contains(j, "сторож: переподключаю") || !strings.Contains(j, "warning two") || strings.Contains(j, "Сбой ядра") {
		t.Errorf("journal %q", j)
	}

	// A log over the limit keeps its newer half, from a line start.
	big := strings.Repeat("old line\n", maxLog/9+10) + "newest\n"
	os.WriteFile(filepath.Join(dir, eventsFile), []byte(big), 0o600)
	Note("after")
	b, _ := os.ReadFile(filepath.Join(dir, eventsFile))
	if len(b) > maxLog/2+100 || !strings.HasPrefix(string(b), "old line\n") || !strings.Contains(string(b), "newest\n") {
		t.Errorf("trimmed log: %d bytes, starts %q", len(b), b[:20])
	}
	if Healthy() {
		t.Error("healthy without a running VPN")
	}
}

// initTemp starts the core in a temporary folder. The crash output keeps
// crash.log open, and Windows cannot delete an open file, so it is released
// before the folder is removed.
func initTemp(t *testing.T) string {
	dir := t.TempDir()
	Init(dir, "test")
	inBackground = func(func()) {}
	t.Cleanup(func() { debug.SetCrashOutput(nil, debug.CrashOptions{}) })
	return dir
}

func TestAbout(t *testing.T) {
	initTemp(t)
	var a map[string]string
	if err := json.Unmarshal([]byte(About()), &a); err != nil {
		t.Fatal(err)
	}
	if a["version"] != "test" || a["xray"] == "" || !strings.HasPrefix(a["go"], "go") {
		t.Fatalf("about: %v", a)
	}
}
