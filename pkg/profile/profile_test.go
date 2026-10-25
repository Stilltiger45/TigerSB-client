package profile

import (
	"crypto/ed25519"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func loadFixture(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/profile.json")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestValidateNormalizes(t *testing.T) {
	var p Profile
	if err := json.Unmarshal(loadFixture(t), &p); err != nil {
		t.Fatal(err)
	}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	if got := p.Routing.Blocked.Domains[0]; got != "domain:youtube.com" {
		t.Errorf("bare domain normalised to %q", got)
	}
	if !p.HasRole(RoleRU) || !p.HasRole(RoleForeign) {
		t.Error("roles not found")
	}
}

func TestValidateRejects(t *testing.T) {
	cases := map[string]func(p *Profile){
		"version":        func(p *Profile) { p.V = 2 },
		"no foreign":     func(p *Profile) { p.Exits = p.Exits[1:] },
		"bad role":       func(p *Profile) { p.Exits[0].Role = "moon" },
		"duplicate exit": func(p *Profile) { p.Exits[1].ID = p.Exits[0].ID },
		"freedom":        func(p *Profile) { p.Exits[0].Transports[0].Xray = json.RawMessage(`{"protocol":"freedom"}`) },
		"tag field":      func(p *Profile) { p.Exits[0].Transports[0].Xray = json.RawMessage(`{"protocol":"vless","tag":"x"}`) },
		"proxySettings": func(p *Profile) {
			p.Exits[0].Transports[0].Xray = json.RawMessage(`{"protocol":"vless","proxySettings":{"tag":"direct"}}`)
		},
		"bad cidr":                     func(p *Profile) { p.Routing.Blocked.IPs = []string{"300.1.1.1/8"} },
		"bad regexp":                   func(p *Profile) { p.Routing.Blocked.Domains = []string{"regexp:("} },
		"unknown prefix":               func(p *Profile) { p.Routing.Blocked.Domains = []string{"ext:foo"} },
		"apps blocked":                 func(p *Profile) { p.Routing.Blocked.Apps = []string{"org.telegram.messenger"} },
		"bad app":                      func(p *Profile) { p.Routing.Direct.Apps = []string{"not a package"} },
		"http update":                  func(p *Profile) { p.Update.URL = "http://panel.example.org/sub" },
		"ru exit without blocked list": func(p *Profile) { p.Routing.Blocked = Matchers{} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			var p Profile
			if err := json.Unmarshal(loadFixture(t), &p); err != nil {
				t.Fatal(err)
			}
			mutate(&p)
			if err := p.Validate(); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

func TestSignOpen(t *testing.T) {
	pub, priv, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	env, err := Sign(priv, loadFixture(t))
	if err != nil {
		t.Fatal(err)
	}

	p, pk, err := Open(env, nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != "home" || !pub.Equal(pk) {
		t.Errorf("got profile %q signed by %x", p.ID, pk)
	}
	if _, _, err := Open(env, pub); err != nil {
		t.Errorf("pinned key rejected: %v", err)
	}

	otherPub, _, _ := GenerateKey()
	if _, _, err := Open(env, otherPub); err == nil || !strings.Contains(err.Error(), "чужим ключом") {
		t.Errorf("foreign key accepted: %v", err)
	}

	// Swap the payload but keep the signature.
	var e Envelope
	_ = json.Unmarshal(env, &e)
	tampered := strings.Replace(string(loadFixture(t)), "203.0.113.10", "203.0.113.66", 1)
	e.Payload = b64.EncodeToString([]byte(tampered))
	bad, _ := json.Marshal(e)
	if _, _, err := Open(bad, nil); err == nil || !strings.Contains(err.Error(), "подпись") {
		t.Errorf("tampered payload accepted: %v", err)
	}
}

func TestSignRejectsInvalidProfile(t *testing.T) {
	_, priv, _ := GenerateKey()
	if _, err := Sign(priv, []byte(`{"v":1,"id":"x","exits":[]}`)); err == nil {
		t.Error("signed a profile with no exits")
	}
}

func TestPrivateKeyFromSeed(t *testing.T) {
	pub, priv, _ := GenerateKey()
	got, err := DecodePrivateKey(EncodeKey(priv.Seed()))
	if err != nil {
		t.Fatal(err)
	}
	if !got.Public().(ed25519.PublicKey).Equal(pub) {
		t.Error("seed does not give the same key")
	}
}
