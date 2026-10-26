package netauto

import (
	"context"
	"net"
	"slices"
	"sync"
	"time"
)

// Sizes are the packet sizes PathMTU tries, largest first: a clean Ethernet
// path, then the TUN MTUs the app picks from (see MTU).
var Sizes = []int{1500, 1480, 1460, 1280}

// PathMTU returns the largest of Sizes that reaches host and back in one
// piece: an ICMP echo of that size with "don't fragment" set gets a reply.
// It returns 0 when nothing answered, not even 1280 bytes, or the system
// allows no ICMP socket: the host drops pings, so the path is unknown.
func PathMTU(ctx context.Context, host string) int {
	ip := ipv4(ctx, host)
	if ip == nil {
		return 0
	}
	ctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	ok := make([]bool, len(Sizes))
	var wg sync.WaitGroup
	for i, size := range Sizes {
		wg.Go(func() { ok[i] = echo(ctx, ip, size) })
	}
	wg.Wait()
	if i := slices.Index(ok, true); i >= 0 {
		return Sizes[i]
	}
	return 0
}

// ipv4 resolves host to its first IPv4 address, or nil.
func ipv4(ctx context.Context, host string) net.IP {
	if ip := net.ParseIP(host); ip != nil {
		return ip.To4()
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip4", host)
	if err != nil || len(ips) == 0 {
		return nil
	}
	return ips[0].To4()
}

// checksum is the Internet checksum of b.
func checksum(b []byte) uint16 {
	var sum uint32
	for i := 0; i+1 < len(b); i += 2 {
		sum += uint32(b[i])<<8 | uint32(b[i+1])
	}
	if len(b)%2 == 1 {
		sum += uint32(b[len(b)-1]) << 8
	}
	for sum>>16 != 0 {
		sum = sum&0xffff + sum>>16
	}
	return ^uint16(sum)
}

// echoRequest builds an ICMP echo request that makes an IPv4 packet of size
// bytes: 20 of IP header, 8 of ICMP header and the payload.
func echoRequest(seq uint16, size int) []byte {
	b := make([]byte, size-20)
	b[0] = 8 // echo request
	b[6], b[7] = byte(seq>>8), byte(seq)
	for i := 8; i < len(b); i++ {
		b[i] = byte(i)
	}
	c := checksum(b)
	b[2], b[3] = byte(c>>8), byte(c)
	return b
}
