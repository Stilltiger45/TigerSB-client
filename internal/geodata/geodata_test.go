package geodata_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xtls/xray-core/app/router"
	"google.golang.org/protobuf/proto"

	"github.com/Stilltiger45/TigerSB-client/internal/engine"
	"github.com/Stilltiger45/TigerSB-client/internal/geodata"
	"github.com/Stilltiger45/TigerSB-client/internal/xrayconf"
	"github.com/Stilltiger45/TigerSB-client/pkg/profile"
)

func siteList(t *testing.T) []byte {
	t.Helper()
	var l router.GeoSiteList
	for _, code := range []string{"CATEGORY-ADS", "RU-BLOCKED", "RU-BLOCKED-ALL", "YOUTUBE"} {
		l.Entry = append(l.Entry, &router.GeoSite{CountryCode: code, Domain: []*router.Domain{
			{Type: router.Domain_Domain, Value: strings.ToLower(code) + ".example"},
		}})
	}
	b, err := proto.Marshal(&l)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func ipList(t *testing.T) []byte {
	t.Helper()
	l := router.GeoIPList{Entry: []*router.GeoIP{
		{CountryCode: "RU", Cidr: []*router.CIDR{{Ip: []byte{5, 0, 0, 0}, Prefix: 8}}},
		{CountryCode: "RU-BLOCKED", Cidr: []*router.CIDR{{Ip: []byte{8, 8, 8, 0}, Prefix: 24}}},
	}}
	b, err := proto.Marshal(&l)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func fixture(t *testing.T, siteURL, ipURL string) *profile.Profile {
	t.Helper()
	b, err := os.ReadFile("../../pkg/profile/testdata/profile.json")
	if err != nil {
		t.Fatal(err)
	}
	var p profile.Profile
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	p.Geodata = &profile.Geodata{GeoSite: siteURL, GeoIP: ipURL}
	p.Routing.Blocked.Domains = append(p.Routing.Blocked.Domains, "geosite:ru-blocked")
	p.Routing.Blocked.IPs = append(p.Routing.Blocked.IPs, "geoip:ru-blocked")
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	return &p
}

func TestEnsureTrimsAndXrayLoads(t *testing.T) {
	var hits atomic.Int32
	site, ip := siteList(t), ipList(t)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if strings.HasSuffix(r.URL.Path, "geosite.dat") {
			_, _ = w.Write(site)
		} else {
			_, _ = w.Write(ip)
		}
	}))
	defer srv.Close()

	p := fixture(t, srv.URL+"/geosite.dat", srv.URL+"/geoip.dat")
	sites, ips := geodata.Codes(p)
	if strings.Join(sites, ",") != "RU-BLOCKED" || strings.Join(ips, ",") != "RU-BLOCKED" {
		t.Fatalf("codes %v %v", sites, ips)
	}

	dir := t.TempDir()
	if geodata.Ready(dir, p) {
		t.Error("ready before the download")
	}
	if err := geodata.Ensure(context.Background(), srv.Client(), dir, p, 0); err != nil {
		t.Fatal(err)
	}
	if !geodata.Ready(dir, p) || geodata.Ready(dir, p, "geoip:ru") {
		t.Error("ready must hold exactly for the downloaded lists")
	}
	fi, _ := os.Stat(filepath.Join(dir, geodata.GeoSiteFile))
	if fi.Size() >= int64(len(site)) {
		t.Errorf("geosite.dat not trimmed: %d of %d bytes", fi.Size(), len(site))
	}
	var l router.GeoSiteList
	b, _ := os.ReadFile(filepath.Join(dir, geodata.GeoSiteFile))
	if err := proto.Unmarshal(b, &l); err != nil || len(l.Entry) != 1 || l.Entry[0].CountryCode != "RU-BLOCKED" {
		t.Fatalf("trimmed file: %v %v", err, l.Entry)
	}

	// Real Xray finds the lists in the trimmed files.
	if err := engine.SetAssetDir(dir); err != nil {
		t.Fatal(err)
	}
	cfg, err := xrayconf.Build(p, xrayconf.Options{Inbounds: []xrayconf.Inbound{{Tag: "socks-in", Protocol: "socks", Listen: "127.0.0.1", Port: 10808}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Load(cfg); err != nil {
		t.Fatal(err)
	}

	// Complete and fresh files are not downloaded again.
	before := hits.Load()
	if err := geodata.Ensure(context.Background(), srv.Client(), dir, p, time.Hour); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != before {
		t.Error("fresh files downloaded again")
	}
}

func TestEnsureKeepsOldFileWhenRefreshFails(t *testing.T) {
	site, ip := siteList(t), ipList(t)
	var down atomic.Bool
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if down.Load() {
			http.Error(w, "down", http.StatusBadGateway)
			return
		}
		if strings.HasSuffix(r.URL.Path, "geosite.dat") {
			_, _ = w.Write(site)
		} else {
			_, _ = w.Write(ip)
		}
	}))
	defer srv.Close()
	p := fixture(t, srv.URL+"/geosite.dat", srv.URL+"/geoip.dat")
	dir := t.TempDir()
	if err := geodata.Ensure(context.Background(), srv.Client(), dir, p, 0); err != nil {
		t.Fatal(err)
	}
	down.Store(true)
	// maxAge of 1ns makes every file stale.
	if err := geodata.Ensure(context.Background(), srv.Client(), dir, p, time.Nanosecond); err != nil {
		t.Errorf("stale but complete files must survive a failed refresh: %v", err)
	}
	// A list the files lack cannot be served from the old copy.
	p.Routing.Blocked.Domains = append(p.Routing.Blocked.Domains, "geosite:youtube")
	if err := geodata.Ensure(context.Background(), srv.Client(), dir, p, 0); err == nil {
		t.Error("missing list reported as present")
	}
}

func TestEnsureMissingCode(t *testing.T) {
	site := siteList(t)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(site) }))
	defer srv.Close()
	p := fixture(t, srv.URL+"/geosite.dat", srv.URL+"/geoip.dat")
	p.Routing.Blocked.IPs = nil
	p.Routing.Blocked.Domains = []string{"geosite:netflix"}
	err := geodata.Ensure(context.Background(), srv.Client(), t.TempDir(), p, 0)
	if err == nil || !strings.Contains(err.Error(), "netflix") {
		t.Errorf("got %v", err)
	}
}

func TestEnsureBasicIPs(t *testing.T) {
	ip := ipList(t)
	var sites atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "geosite.dat") {
			sites.Add(1)
		}
		_, _ = w.Write(ip)
	}))
	defer srv.Close()
	p, err := profile.FromVLESS("vless://5783a3e7-e373-51cd-8642-c83782b807c5@1.2.3.4:443?security=none")
	if err != nil {
		t.Fatal(err)
	}
	p.Geodata = &profile.Geodata{GeoSite: srv.URL + "/geosite.dat", GeoIP: srv.URL + "/geoip.dat"}
	dir := t.TempDir()
	// The basic mode needs Russian IPs even when the profile has no lists;
	// geosite.dat is not needed at all.
	if err := geodata.Ensure(context.Background(), srv.Client(), dir, p, 0, "geoip:ru"); err != nil {
		t.Fatal(err)
	}
	var l router.GeoIPList
	b, _ := os.ReadFile(filepath.Join(dir, geodata.GeoIPFile))
	if err := proto.Unmarshal(b, &l); err != nil || len(l.Entry) != 1 || l.Entry[0].CountryCode != "RU" {
		t.Fatalf("geoip.dat: %v %v", err, l.Entry)
	}
	if sites.Load() != 0 {
		t.Error("geosite.dat downloaded without lists")
	}
}

func TestEnsureNothingNeeded(t *testing.T) {
	p, err := profile.FromVLESS("vless://5783a3e7-e373-51cd-8642-c83782b807c5@1.2.3.4:443?security=none")
	if err != nil {
		t.Fatal(err)
	}
	// No lists, no download: a nil client and no server must be fine.
	if err := geodata.Ensure(context.Background(), nil, t.TempDir(), p, 0); err != nil {
		t.Error(err)
	}
}
