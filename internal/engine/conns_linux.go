package engine

import "golang.org/x/sys/unix"

// linkOptions sets the link's TCP options on a socket before it connects.
func linkOptions(fd uintptr, l Link) {
	if l.Mobile {
		unix.SetsockoptInt(int(fd), unix.IPPROTO_TCP, unix.TCP_USER_TIMEOUT, 30000)
	}
	if l.MSS > 0 {
		unix.SetsockoptInt(int(fd), unix.IPPROTO_TCP, unix.TCP_MAXSEG, l.MSS)
	}
}
