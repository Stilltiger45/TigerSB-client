package subscription

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Stilltiger45/TigerSB-client/internal/engine"
	"github.com/Stilltiger45/TigerSB-client/internal/xrayconf"
)

const foreignLinks = `vless://5783a3e7-e373-51cd-8642-c83782b807c5@nl.example.org:443?security=reality&pbk=Y4CRAOigwd_rCfP7TmrAzkDtnlNMBXxThbt1Uy8lHwY&sni=www.example.com&sid=6ba8&flow=xtls-rprx-vision#🇳🇱 NL
trojan://secret@de.example.org:443?type=ws&path=%2Fws#🇩🇪 DE
hysteria2://pw@fi.example.org:443#FI
`

func TestForeignSubscriptionBase64WithHWID(t *testing.T) {
	ThisDevice = Device{HWID: "abc123", OS: "Android", OSVersion: "14", Model: "Pixel 8"}
	defer func() { ThisDevice = Device{} }()
	var hwid, model atomic.Value
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hwid.Store(r.Header.Get("x-hwid"))
		model.Store(r.Header.Get("x-device-model"))
		w.Header().Set("Profile-Title", "base64:"+base64.StdEncoding.EncodeToString([]byte("Чужой VPN")))
		w.Header().Set("Profile-Update-Interval", "12")
		_, _ = w.Write([]byte(base64.StdEncoding.EncodeToString([]byte(foreignLinks))))
	}))
	defer srv.Close()

	c := &Client{HTTP: srv.Client()}
	st, p, err := c.Import(context.Background(), srv.URL+"/sub/xyz")
	if err != nil {
		t.Fatal(err)
	}
	if hwid.Load() != "abc123" || model.Load() != "Pixel 8" {
		t.Errorf("device headers: %v %v", hwid.Load(), model.Load())
	}
	if !st.Foreign || st.PK != "" || p.Name != "Чужой VPN" || p.Update.Every != "12h" {
		t.Errorf("state %+v profile %+v", st, p)
	}
	ts := p.Exits[0].Transports
	if len(ts) != 2 || ts[0].Name != "🇳🇱 NL" || ts[1].Name != "🇩🇪 DE" {
		t.Fatalf("transports %+v", ts)
	}

	// All servers go into one balancer that real Xray accepts.
	cfg, err := xrayconf.Build(p, xrayconf.Options{Inbounds: []xrayconf.Inbound{{Tag: "tun-in", Protocol: "tun"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Load(cfg); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cfg), `"leastPing"`) {
		t.Error("no balancer for several servers")
	}

	again, err := c.Refresh(context.Background(), st)
	if err != nil || len(again.Exits[0].Transports) != 2 {
		t.Errorf("refresh: %v", err)
	}
	if _, err := st.Open(); err != nil {
		t.Error(err)
	}
}

func TestForeignSubscriptionRetriesWithKnownClient(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.UserAgent(), "v2rayNG/") {
			_, _ = w.Write([]byte("<html>Скачайте приложение</html>"))
			return
		}
		_, _ = w.Write([]byte(foreignLinks))
	}))
	defer srv.Close()
	_, p, err := (&Client{HTTP: srv.Client()}).Import(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Exits[0].Transports) != 2 {
		t.Errorf("transports %+v", p.Exits[0].Transports)
	}
}

func TestForeignSubscriptionXrayJSON(t *testing.T) {
	configs := []map[string]any{
		{"remarks": "NL json", "outbounds": []any{
			map[string]any{"tag": "proxy", "protocol": "vless",
				"settings":       map[string]any{"vnext": []any{map[string]any{"address": "nl.example.org", "port": 443, "users": []any{map[string]any{"id": "5783a3e7-e373-51cd-8642-c83782b807c5", "encryption": "none"}}}}},
				"streamSettings": map[string]any{"network": "raw", "security": "tls", "tlsSettings": map[string]any{"serverName": "nl.example.org"}, "sockopt": map[string]any{"dialerProxy": "fragment"}}},
			map[string]any{"tag": "fragment", "protocol": "freedom"},
			map[string]any{"tag": "direct", "protocol": "freedom"},
		}},
		{"remarks": "broken", "outbounds": []any{map[string]any{"protocol": "freedom"}}},
	}
	body, _ := json.Marshal(configs)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(body) }))
	defer srv.Close()
	_, p, err := (&Client{HTTP: srv.Client()}).Import(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	ts := p.Exits[0].Transports
	if len(ts) != 1 || ts[0].Name != "NL json" || strings.Contains(string(ts[0].Xray), "dialerProxy") || strings.Contains(string(ts[0].Xray), `"tag"`) {
		t.Fatalf("transports %+v %s", ts, ts[0].Xray)
	}
	cfg, err := xrayconf.Build(p, xrayconf.Options{Inbounds: []xrayconf.Inbound{{Tag: "s", Protocol: "socks", Listen: "127.0.0.1", Port: 10808}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Load(cfg); err != nil {
		t.Fatal(err)
	}
}

func TestForeignSubscriptionEmpty(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("nothing here")) }))
	defer srv.Close()
	if _, _, err := (&Client{HTTP: srv.Client()}).Import(context.Background(), srv.URL); err == nil || !strings.Contains(err.Error(), "сервер") {
		t.Errorf("got %v", err)
	}
}
