package mobile

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xtls/xray-core/app/router"
	"google.golang.org/protobuf/proto"

	"github.com/Stilltiger45/TigerSB-client/internal/engine"
	"github.com/Stilltiger45/TigerSB-client/internal/geodata"
	"github.com/Stilltiger45/TigerSB-client/pkg/profile"
)

// TestListsComeThroughVPN starts a core without the lists and checks that
// they are fetched through it, and that the app is then asked to reconnect
// once. The lists' host resolves nowhere: only the core's outbound, which
// redirects to the test server, reaches it.
func TestListsComeThroughVPN(t *testing.T) {
	initTemp(t)
	ips, _ := proto.Marshal(&router.GeoIPList{Entry: []*router.GeoIP{
		{CountryCode: "RU", Cidr: []*router.CIDR{{Ip: []byte{5, 0, 0, 0}, Prefix: 8}}},
	}})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(ips) }))
	defer srv.Close()
	cfg, _ := json.Marshal(map[string]any{
		"log": map[string]any{"loglevel": "none"},
		"outbounds": []any{map[string]any{"protocol": "freedom", "settings": map[string]any{
			"redirect": strings.TrimPrefix(srv.URL, "http://"),
		}}},
	})
	i, err := engine.Start(cfg)
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	inst = i
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		inst = nil
		mu.Unlock()
		engine.Stop(i)
	})

	p := &profile.Profile{Geodata: &profile.Geodata{GeoIP: "http://lists.invalid/geoip.dat"}}
	fetchLists(i, p, 0, []string{"geoip:ru"})
	if !geodata.Ready(assetDir(), p, "geoip:ru") {
		t.Fatal("the lists did not come through the core")
	}
	if !TakeRestart() || TakeRestart() {
		t.Error("the app must be asked to reconnect exactly once")
	}
}
