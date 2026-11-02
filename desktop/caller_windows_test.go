package desktop

import (
	"net"
	"os"
	"testing"
)

// The service finds the program at the other end of a connection: here the
// test itself, which lies next to itself, so it is let in.
func TestCheckCallerFindsOwnProcess(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	c, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	s, err := l.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	pid, err := connectionOwner(s.LocalAddr().(*net.TCPAddr), s.RemoteAddr().(*net.TCPAddr))
	if err != nil || pid != uint32(os.Getpid()) {
		t.Fatalf("owner %d, %v; want %d", pid, err, os.Getpid())
	}
	if err := checkCaller(s); err != nil {
		t.Errorf("own process refused: %v", err)
	}
}

func TestSameDir(t *testing.T) {
	if !sameDir(`C:\Program Files\TigerSB`, `c:\program files\tigersb\`) {
		t.Error("the same folder in other case is not the same")
	}
	if sameDir(`C:\Program Files\TigerSB`, `C:\Users\u\Downloads`) {
		t.Error("another folder passes")
	}
}
