// Package engine runs Xray-core in-process from a generated config.
package engine

import (
	"bytes"
	"fmt"
	"os"

	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/infra/conf/serial"

	// Register every Xray feature the config can refer to.
	_ "github.com/xtls/xray-core/main/distro/all"
)

// Load parses and builds an Xray JSON config without starting it.
func Load(config []byte) (*core.Config, error) {
	cfg, err := serial.LoadJSONConfig(bytes.NewReader(config))
	if err != nil {
		return nil, fmt.Errorf("Xray не принял конфиг: %w", err)
	}
	return cfg, nil
}

// SetAssetDir points Xray at the directory with geosite.dat and geoip.dat.
func SetAssetDir(dir string) error {
	if dir == "" {
		return nil
	}
	return os.Setenv("XRAY_LOCATION_ASSET", dir)
}

// Start builds the config and starts Xray. Close the instance to stop it.
func Start(config []byte) (*core.Instance, error) {
	cfg, err := Load(config)
	if err != nil {
		return nil, err
	}
	inst, err := core.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("не удалось создать ядро Xray: %w", err)
	}
	if err := inst.Start(); err != nil {
		return nil, fmt.Errorf("не удалось запустить ядро Xray: %w", err)
	}
	return inst, nil
}
