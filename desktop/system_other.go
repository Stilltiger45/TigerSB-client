//go:build !windows

package desktop

import (
	"errors"
	"os"
)

// Only Windows has the service for now; elsewhere the files are plain.

func secureDir(dir string) error { return os.MkdirAll(dir, 0o700) }

func writeForUsers(path string, b []byte) error { return os.WriteFile(path, b, 0o644) }

func stateKey(string) ([]byte, error) { return nil, nil }

// NoPlatform stands for a system the service does not support.
type NoPlatform struct{}

var errNoPlatform = errors.New("VPN-служба есть только для Windows")

func (NoPlatform) Uplink() (Uplink, bool, error)       { return Uplink{}, false, errNoPlatform }
func (NoPlatform) WatchNetwork(func()) (func(), error) { return nil, errNoPlatform }
func (NoPlatform) ConfigureTUN(string, int) error      { return errNoPlatform }
func (NoPlatform) ReleaseTUN(string)                   {}
func (NoPlatform) String() string                      { return "none" }
