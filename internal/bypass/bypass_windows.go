package bypass

import (
	"encoding/binary"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	ipUnicastIf   = 31 // IP_UNICAST_IF
	ipv6UnicastIf = 31 // IPV6_UNICAST_IF
)

// bind sets IP_UNICAST_IF and, on IPv6 sockets, which Go also opens for
// IPv4 when no address family is named, IPV6_UNICAST_IF. IPv4 takes the
// index in network byte order.
func bind(fd uintptr, network string, index int) error {
	var be [4]byte
	binary.BigEndian.PutUint32(be[:], uint32(index))
	v4 := int(*(*uint32)(unsafe.Pointer(&be[0])))
	h := windows.Handle(fd)
	err4 := windows.SetsockoptInt(h, windows.IPPROTO_IP, ipUnicastIf, v4)
	if strings.HasSuffix(network, "4") {
		return err4
	}
	return windows.SetsockoptInt(h, windows.IPPROTO_IPV6, ipv6UnicastIf, index)
}
