package profile

import (
	"encoding/json"
	"testing"
)

func TestKeyRoundTrip(t *testing.T) {
	pub, _, _ := GenerateKey()
	k := Key{Kind: KindSubscription, URL: "https://panel.example.org/sub/abc?x=1&y=2", PK: pub, Name: "Дом"}
	got, err := ParseKey(k.String())
	if err != nil {
		t.Fatal(err)
	}
	if got.URL != k.URL || !got.PK.Equal(pub) || got.Name != "Дом" || got.Kind != KindSubscription {
		t.Errorf("round trip lost data: %+v", got)
	}
}

func TestParseKey(t *testing.T) {
	pub, _, _ := GenerateKey()
	good := map[string]KeyKind{
		"  https://panel.example.org/sub/abc \n":                                         KindSubscription,
		"vless://5783a3e7-e373-51cd-8642-c83782b807c5@1.2.3.4:443?security=none#x":       KindVLESS,
		"tigersb://add?url=https%3A%2F%2Fp.example.org%2Fs&pk=" + EncodeKey(pub):         KindSubscription,
		"TigerSB://add?url=https%3A%2F%2Fp.example.org%2Fs&pk=" + EncodeKey(pub) + "&n=": KindSubscription,
	}
	for s, kind := range good {
		k, err := ParseKey(s)
		if err != nil {
			t.Errorf("%q: %v", s, err)
			continue
		}
		if k.Kind != kind {
			t.Errorf("%q: kind %v", s, k.Kind)
		}
	}
	bad := []string{
		"",
		"hello",
		"wireguard://abc@1.2.3.4:51820",
		"tigersb://add?url=http%3A%2F%2Fp.example.org&pk=" + EncodeKey(pub),
		"tigersb://add?url=https%3A%2F%2Fp.example.org",
		"tigersb://other?url=https%3A%2F%2Fp.example.org&pk=" + EncodeKey(pub),
	}
	for _, s := range bad {
		if _, err := ParseKey(s); err == nil {
			t.Errorf("%q accepted", s)
		}
	}
}

func TestFromVLESS(t *testing.T) {
	link := "vless://5783a3e7-e373-51cd-8642-c83782b807c5@203.0.113.10:443?type=tcp&security=reality&pbk=Y4CRAOigwd_rCfP7TmrAzkDtnlNMBXxThbt1Uy8lHwY&sid=6ba8&sni=www.example.com&fp=firefox&flow=xtls-rprx-vision#NL%20Reality"
	p, err := FromVLESS(link)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "NL Reality" || len(p.Exits) != 1 || p.Exits[0].Role != RoleForeign {
		t.Fatalf("unexpected profile %+v", p)
	}
	var ob struct {
		Settings struct {
			Vnext []struct {
				Address string
				Port    int
				Users   []struct{ ID, Flow, Encryption string }
			}
		}
		StreamSettings struct {
			Network         string
			Security        string
			RealitySettings map[string]string
		}
	}
	if err := json.Unmarshal(p.Exits[0].Transports[0].Xray, &ob); err != nil {
		t.Fatal(err)
	}
	v := ob.Settings.Vnext[0]
	if v.Address != "203.0.113.10" || v.Port != 443 || v.Users[0].Flow != "xtls-rprx-vision" || v.Users[0].Encryption != "none" {
		t.Errorf("settings: %+v", v)
	}
	r := ob.StreamSettings.RealitySettings
	if ob.StreamSettings.Network != "raw" || ob.StreamSettings.Security != "reality" || r["fingerprint"] != "firefox" || r["shortId"] != "6ba8" {
		t.Errorf("stream: %+v", ob.StreamSettings)
	}
}

func TestFromVLESSRejects(t *testing.T) {
	bad := []string{
		"vless://@1.2.3.4:443",
		"vless://5783a3e7-e373-51cd-8642-c83782b807c5@1.2.3.4",
		"vless://5783a3e7-e373-51cd-8642-c83782b807c5@1.2.3.4:443?type=kcp",
		"vless://5783a3e7-e373-51cd-8642-c83782b807c5@1.2.3.4:443?security=reality",
		"vless://5783a3e7-e373-51cd-8642-c83782b807c5@1.2.3.4:443?type=xhttp&extra=%7Bbad",
	}
	for _, s := range bad {
		if _, err := FromVLESS(s); err == nil {
			t.Errorf("%q accepted", s)
		}
	}
}
