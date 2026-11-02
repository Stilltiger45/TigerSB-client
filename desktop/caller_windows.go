package desktop

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The token in the control file is readable by every user of the computer,
// so it alone does not tell the app from any other program. The service
// answers only programs that lie next to it, in its own directory under
// Program Files, which only administrators may change: TigerSB.exe, as
// the installer put it there.

var procGetExtendedTCPTable = windows.NewLazySystemDLL("iphlpapi.dll").NewProc("GetExtendedTcpTable")

// TCP_TABLE_OWNER_PID_CONNECTIONS: the open TCP connections with the
// process of each.
const tcpTableOwnerPIDConnections = 4

// checkCaller lets in a connection to the service only from a program in the
// service's own directory.
func checkCaller(c net.Conn) error {
	local, ok1 := c.LocalAddr().(*net.TCPAddr)
	remote, ok2 := c.RemoteAddr().(*net.TCPAddr)
	if !ok1 || !ok2 {
		return errors.New("не TCP")
	}
	pid, err := connectionOwner(local, remote)
	if err != nil {
		return err
	}
	path := imagePath(pid)
	if path == "" {
		return fmt.Errorf("программа процесса %d неизвестна", pid)
	}
	own, err := os.Executable()
	if err != nil {
		return err
	}
	if !sameDir(filepath.Dir(path), filepath.Dir(own)) {
		return fmt.Errorf("чужая программа %s", path)
	}
	return nil
}

func sameDir(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

// connectionOwner finds the process whose end of a connection to this
// computer is remote, the other end being local.
func connectionOwner(local, remote *net.TCPAddr) (uint32, error) {
	var buf []byte
	size := uint32(0)
	for {
		var p unsafe.Pointer
		if len(buf) > 0 {
			p = unsafe.Pointer(&buf[0])
		}
		r, _, _ := procGetExtendedTCPTable.Call(uintptr(p), uintptr(unsafe.Pointer(&size)), 0,
			windows.AF_INET, tcpTableOwnerPIDConnections, 0)
		if r == 0 && len(buf) >= 4 {
			break
		}
		if r != 0 && r != uintptr(windows.ERROR_INSUFFICIENT_BUFFER) {
			return 0, syscall.Errno(r)
		}
		buf = make([]byte, size)
	}
	return findOwner(buf, local, remote)
}

// findOwner reads a MIB_TCPTABLE_OWNER_PID: a count, then rows of state,
// local address and port, remote address and port and process, 4 bytes
// each, the addresses and ports in network order.
func findOwner(table []byte, local, remote *net.TCPAddr) (uint32, error) {
	const row = 24
	n := int(binary.LittleEndian.Uint32(table))
	for i := 0; i < n && 4+(i+1)*row <= len(table); i++ {
		r := table[4+i*row : 4+(i+1)*row]
		port := func(b []byte) int { return int(b[0])<<8 | int(b[1]) }
		if port(r[8:]) == remote.Port && port(r[16:]) == local.Port &&
			net.IP(r[4:8]).Equal(remote.IP) && net.IP(r[12:16]).Equal(local.IP) {
			return binary.LittleEndian.Uint32(r[20:]), nil
		}
	}
	return 0, errors.New("соединение не найдено")
}
