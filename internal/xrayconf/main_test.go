package xrayconf_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xtls/xray-core/app/router"
	"google.golang.org/protobuf/proto"

	"github.com/Stilltiger45/TigerSB-client/internal/engine"
)

// TestMain gives Xray a geoip.dat with the Russian list, which the basic mode
// refers to; the client downloads the real one (package geodata).
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "xrayconf-geo")
	if err != nil {
		panic(err)
	}
	l := router.GeoIPList{Entry: []*router.GeoIP{
		{CountryCode: "RU", Cidr: []*router.CIDR{{Ip: []byte{5, 0, 0, 0}, Prefix: 8}}},
	}}
	b, err := proto.Marshal(&l)
	if err != nil {
		panic(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "geoip.dat"), b, 0o600); err != nil {
		panic(err)
	}
	if err := engine.SetAssetDir(dir); err != nil {
		panic(err)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
