package desktop

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"

	"github.com/Stilltiger45/TigerSB-client/internal/bypass"
	"github.com/Stilltiger45/TigerSB-client/internal/device"
	"github.com/Stilltiger45/TigerSB-client/internal/subscription"
	"github.com/Stilltiger45/TigerSB-client/mobile"
)

// Control is what the service writes to its control file for the app: the
// port it listens on at 127.0.0.1 and the token to send first.
type Control struct {
	Port  int    `json:"port"`
	Token string `json:"token"`
}

// ControlFile is the control file's name in the service's directory.
const ControlFile = "control.json"

// Run runs the service until ctx ends: the core keeps its state in
// root/data, which only the system and administrators may read, and the
// control file goes to root, where the computer's users can read it.
func Run(ctx context.Context, root, version string, plat Platform) error {
	data := filepath.Join(root, "data")
	if err := secureDir(data); err != nil {
		return fmt.Errorf("каталог данных: %w", err)
	}
	// The core's own connections leave outside the VPN, names included.
	bypass.Install()
	mobile.Init(data, version)
	subscription.UserAgent = "TigerSB-windows/" + version
	d := device.This(data)
	mobile.SetDevice(d.HWID, d.OS, d.OSVersion, d.Model)
	if key, err := stateKey(data); err != nil {
		mobile.Note("ключ шифрования состояния: " + err.Error())
	} else if key != nil {
		if err := mobile.SetStateKey(key); err != nil {
			mobile.Note("ключ шифрования состояния: " + err.Error())
		}
	}

	vpn := NewVPN(Core{}, plat, data)
	defer vpn.Close()
	srv := NewServer(vpn)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	b, _ := json.Marshal(Control{Port: l.Addr().(*net.TCPAddr).Port, Token: srv.Token()})
	path := filepath.Join(root, ControlFile)
	if err := writeForUsers(path, b); err != nil {
		l.Close()
		return fmt.Errorf("файл управления: %w", err)
	}
	defer os.Remove(path)
	mobile.Note("служба запущена, версия " + version)
	return srv.Serve(ctx, l)
}
