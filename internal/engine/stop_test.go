package engine_test

import (
	"encoding/json"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/xtls/xray-core/proxy/tun"

	"github.com/Stilltiger45/TigerSB-client/internal/engine"
)

// gvisorGoroutines counts goroutines running gVisor's packet loops.
func gvisorGoroutines() int {
	buf := make([]byte, 1<<22)
	n := runtime.Stack(buf, true)
	return strings.Count(string(buf[:n]), "gvisor.dev/gvisor/pkg/tcpip/link/fdbased")
}

func TestStopClosesTunStack(t *testing.T) {
	cfg, _ := json.Marshal(map[string]any{
		"log": map[string]any{"loglevel": "none"},
		"inbounds": []any{map[string]any{
			"tag": "tun-in", "protocol": "tun",
			"settings": map[string]any{"name": "tigersbtest0", "MTU": 1500},
		}},
		"outbounds": []any{map[string]any{"protocol": "freedom"}},
	})
	for i := range 3 {
		inst, err := engine.Start(cfg)
		if err != nil {
			t.Skipf("no TUN here: %v", err)
		}
		if gvisorGoroutines() == 0 {
			t.Fatal("no packet loop while running")
		}
		if err := engine.Stop(inst); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(3 * time.Second)
		for gvisorGoroutines() > 0 && time.Now().Before(deadline) {
			time.Sleep(50 * time.Millisecond)
		}
		if n := gvisorGoroutines(); n > 0 {
			t.Fatalf("start %d: %d gVisor goroutines left after Stop", i, n)
		}
	}
}

// Stop reaches into tun.Handler; this fails when an Xray update renames or
// retypes the field, so the workaround is not lost silently.
func TestTunHandlerKeepsStackField(t *testing.T) {
	f, ok := reflect.TypeOf(tun.Handler{}).FieldByName("stack")
	if !ok || f.Type.Kind() != reflect.Interface || !f.Type.Implements(reflect.TypeOf((*interface{ Close() error })(nil)).Elem()) {
		t.Fatalf("tun.Handler.stack is gone or changed: %+v", f)
	}
}
