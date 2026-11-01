package desktop

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"time"

	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wireguard/windows/tunnel/winipcfg"
)

// Windows is the service's platform on Windows: the network through the IP
// Helper API, the TUN a Wintun adapter that Xray creates.
type Windows struct{}

// tunMetric puts the TUN's routes and DNS server before every other
// interface's.
const tunMetric = 1

// tunLUID finds the TUN adapter by name, 0 when there is none.
func tunLUID(name string) winipcfg.LUID {
	ifc, err := net.InterfaceByName(name)
	if err != nil {
		return 0
	}
	luid, err := winipcfg.LUIDFromIndex(uint32(ifc.Index))
	if err != nil {
		return 0
	}
	return luid
}

// Uplink picks, of the IPv4 default routes outside the TUN, the one Windows
// itself would use: the smallest sum of the route's and the interface's
// metrics, on an interface that is up.
func (Windows) Uplink() (Uplink, bool, error) {
	tun := tunLUID(TUNName)
	rows, err := winipcfg.GetIPForwardTable2(windows.AF_INET)
	if err != nil {
		return Uplink{}, false, err
	}
	var (
		best       *winipcfg.MibIPforwardRow2
		bestIf     *winipcfg.MibIPInterfaceRow
		bestMetric uint64
	)
	for i := range rows {
		r := &rows[i]
		if r.DestinationPrefix.PrefixLength != 0 || r.InterfaceLUID == tun || r.Loopback {
			continue
		}
		ipif, err := r.InterfaceLUID.IPInterface(windows.AF_INET)
		if err != nil || !ipif.Connected {
			continue
		}
		if row, err := r.InterfaceLUID.Interface(); err != nil || row.OperStatus != winipcfg.IfOperStatusUp {
			continue
		}
		m := uint64(r.Metric) + uint64(ipif.Metric)
		if best == nil || m < bestMetric {
			best, bestIf, bestMetric = r, ipif, m
		}
	}
	if best == nil {
		return Uplink{}, false, nil
	}
	luid := best.InterfaceLUID
	u := Uplink{Index: int(best.InterfaceIndex), MTU: int(bestIf.NLMTU)}
	if row, err := luid.Interface(); err == nil {
		switch row.Type {
		case winipcfg.IfTypeIEEE80211:
			u.Kind = "wifi"
		case winipcfg.IfTypeWwanpp, winipcfg.IfTypeWwanpp2:
			u.Kind = "cellular"
		case winipcfg.IfTypeEthernetCSMACD:
			u.Kind = "ethernet"
		}
	}
	if dns, err := luid.DNS(); err == nil {
		// IPv4 servers first: the VPN is IPv4 inside.
		slices.SortStableFunc(dns, func(a, b netip.Addr) int {
			switch {
			case a.Is4() == b.Is4():
				return 0
			case a.Is4():
				return -1
			}
			return 1
		})
		u.DNS = dns
	}
	if addrs, err := winipcfg.GetUnicastIPAddressTable(windows.AF_UNSPEC); err == nil {
		for i := range addrs {
			if addrs[i].InterfaceLUID == luid {
				u.Addrs = append(u.Addrs, addrs[i].Address.Addr())
			}
		}
	}
	return u, true, nil
}

// WatchNetwork listens to changes of routes, interfaces and addresses.
func (Windows) WatchNetwork(changed func()) (func(), error) {
	routes, err := winipcfg.RegisterRouteChangeCallback(func(winipcfg.MibNotificationType, *winipcfg.MibIPforwardRow2) { changed() })
	if err != nil {
		return nil, err
	}
	ifaces, err := winipcfg.RegisterInterfaceChangeCallback(func(winipcfg.MibNotificationType, *winipcfg.MibIPInterfaceRow) { changed() })
	if err != nil {
		routes.Unregister()
		return nil, err
	}
	addrs, err := winipcfg.RegisterUnicastAddressChangeCallback(func(winipcfg.MibNotificationType, *winipcfg.MibUnicastIPAddressRow) { changed() })
	if err != nil {
		routes.Unregister()
		ifaces.Unregister()
		return nil, err
	}
	return func() {
		routes.Unregister()
		ifaces.Unregister()
		addrs.Unregister()
	}, nil
}

// ConfigureTUN sends all traffic into the TUN: its addresses, two halves
// of each address space (more specific than any default route, so the
// system's own stays for the core's sockets, which are bound to the
// uplink), the DNS server the core answers, the lowest metric and the MTU.
func (Windows) ConfigureTUN(name string, mtu int) error {
	var luid winipcfg.LUID
	// Xray creates the adapter as it starts; Windows may show it a moment
	// later.
	for range 50 {
		if luid = tunLUID(name); luid != 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if luid == 0 {
		return errors.New("адаптер " + name + " не появился")
	}
	if err := luid.SetIPAddressesForFamily(windows.AF_INET, []netip.Prefix{tunAddr}); err != nil {
		return fmt.Errorf("адрес: %w", err)
	}
	// IPv6 goes into the TUN too, where the core refuses it, so that apps
	// fall back to IPv4 at once instead of going around the VPN.
	ipv6 := luid.SetIPAddressesForFamily(windows.AF_INET6, []netip.Prefix{tunAddr6}) == nil
	var routes []*winipcfg.RouteData
	for _, p := range []string{"0.0.0.0/1", "128.0.0.0/1"} {
		routes = append(routes, &winipcfg.RouteData{Destination: netip.MustParsePrefix(p), NextHop: netip.IPv4Unspecified(), Metric: 0})
	}
	if ipv6 {
		for _, p := range []string{"::/1", "8000::/1"} {
			routes = append(routes, &winipcfg.RouteData{Destination: netip.MustParsePrefix(p), NextHop: netip.IPv6Unspecified(), Metric: 0})
		}
	}
	if err := luid.SetRoutes(routes); err != nil {
		return fmt.Errorf("маршруты: %w", err)
	}
	families := []winipcfg.AddressFamily{windows.AF_INET}
	if ipv6 {
		families = append(families, windows.AF_INET6)
	}
	for _, f := range families {
		ipif, err := luid.IPInterface(f)
		if err != nil {
			return fmt.Errorf("интерфейс: %w", err)
		}
		ipif.UseAutomaticMetric = false
		ipif.Metric = tunMetric
		if f == windows.AF_INET || mtu >= 1280 {
			ipif.NLMTU = uint32(mtu)
		}
		ipif.DadTransmits = 0
		ipif.RouterDiscoveryBehavior = winipcfg.RouterDiscoveryDisabled
		if err := ipif.Set(); err != nil {
			return fmt.Errorf("метрика и MTU: %w", err)
		}
	}
	if err := luid.SetDNS(windows.AF_INET, []netip.Addr{tunDNS}, nil); err != nil {
		return fmt.Errorf("DNS: %w", err)
	}
	flushDNS()
	return nil
}

// ReleaseTUN forgets the TUN's DNS server, which Windows keeps by the
// adapter, and the names resolved through it.
func (Windows) ReleaseTUN(name string) {
	if luid := tunLUID(name); luid != 0 {
		luid.FlushDNS(windows.AF_INET)
		luid.FlushRoutes(windows.AF_INET)
		luid.FlushRoutes(windows.AF_INET6)
	}
	flushDNS()
}

var dnsFlush = windows.NewLazySystemDLL("dnsapi.dll").NewProc("DnsFlushResolverCache")

// flushDNS empties Windows' DNS cache, so names resolved before the VPN
// changed are asked again.
func flushDNS() {
	if dnsFlush.Find() == nil {
		dnsFlush.Call()
	}
}
