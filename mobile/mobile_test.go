package mobile

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestImportCurrentForget(t *testing.T) {
	Init(t.TempDir(), "test")
	if s, err := Current(); err != nil || s != "" {
		t.Fatalf("empty state: %q %v", s, err)
	}
	if err := Start(3, "main", 1500); err == nil || !strings.Contains(err.Error(), "ключ") {
		t.Errorf("start without a key: %v", err)
	}
	s, err := AddKey("vless://5783a3e7-e373-51cd-8642-c83782b807c5@1.2.3.4:443?security=none#Дом")
	if err != nil {
		t.Fatal(err)
	}
	var in profileInfo
	if err := json.Unmarshal([]byte(s), &in); err != nil || in.Name != "Дом" || len(in.Exits) != 1 || in.HasRU {
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
