package mobile

import (
	"net"
	"testing"
	"time"
)

// TestOnline checks the test of the phone's own internet: any one address
// that answers is enough.
func TestOnline(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	dead, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	deadAddr := dead.Addr().String()
	dead.Close()
	old, oldTimeout := onlineHosts, onlineTimeout
	t.Cleanup(func() { onlineHosts, onlineTimeout = old, oldTimeout })
	onlineTimeout = time.Second

	onlineHosts = []string{deadAddr, ln.Addr().String()}
	if !Online() {
		t.Error("offline although one address answers")
	}
	onlineHosts = []string{deadAddr}
	if Online() {
		t.Error("online although nothing answers")
	}
}
