package netauto

import (
	"context"
	"net"
	"syscall"
	"time"
)

// ipPMTUDiscProbe sets "don't fragment" and sends even above the path MTU
// the kernel has cached, so the network itself answers (IP_PMTUDISC_PROBE).
const ipPMTUDiscProbe = 3

// echo sends an ICMP echo of size bytes to ip with "don't fragment" set,
// twice in case one is lost, and reports whether a reply came back in time.
// It uses an unprivileged ICMP socket, which Android gives every app.
func echo(ctx context.Context, ip net.IP, size int) bool {
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_DGRAM|syscall.SOCK_CLOEXEC, syscall.IPPROTO_ICMP)
	if err != nil {
		return false
	}
	defer syscall.Close(fd)
	if syscall.SetsockoptInt(fd, syscall.IPPROTO_IP, syscall.IP_MTU_DISCOVER, ipPMTUDiscProbe) != nil {
		return false
	}
	tv := syscall.NsecToTimeval((300 * time.Millisecond).Nanoseconds())
	if syscall.SetsockoptTimeval(fd, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &tv) != nil {
		return false
	}
	to := &syscall.SockaddrInet4{}
	copy(to.Addr[:], ip.To4())
	seq := uint16(size)
	buf := make([]byte, 2048)
	for ctx.Err() == nil {
		// A packet larger than the phone's own link fails right here.
		if err := syscall.Sendto(fd, echoRequest(seq, size), 0, to); err != nil {
			return false
		}
		for end := time.Now().Add(400 * time.Millisecond); time.Now().Before(end) && ctx.Err() == nil; {
			n, _, err := syscall.Recvfrom(fd, buf, 0)
			if err != nil {
				continue // timeout: send again or give up
			}
			// The kernel hands back the ICMP message without the IP header.
			if n >= 8 && buf[0] == 0 && uint16(buf[6])<<8|uint16(buf[7]) == seq {
				return n == size-20
			}
		}
	}
	return false
}
