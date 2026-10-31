package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"

	"github.com/Stilltiger45/TigerSB-client/internal/subscription"
)

// thisDevice describes the computer to subscription servers, as the Android
// app describes the phone: panels with a device limit count devices by the
// hwid and list them by system and name. The hwid comes from the system's
// machine id, hashed so that it tells nothing else about the computer, or,
// where there is none, from a random id kept in the state directory dir.
func thisDevice(dir string) subscription.Device {
	host, _ := os.Hostname()
	name, version := osName()
	return subscription.Device{HWID: hwid(dir), OS: name, OSVersion: version, Model: host}
}

func hwid(dir string) string {
	id := machineID()
	if id == "" {
		id = savedID(dir)
	}
	if id == "" {
		return ""
	}
	sum := sha256.Sum256([]byte("TigerSB device " + id))
	return hex.EncodeToString(sum[:8])
}

// savedID returns the id kept in dir, making one the first time.
func savedID(dir string) string {
	path := filepath.Join(dir, "device-id")
	if b, err := os.ReadFile(path); err == nil {
		if id := strings.TrimSpace(string(b)); len(id) >= 16 {
			return id
		}
	}
	b := make([]byte, 16)
	rand.Read(b)
	id := hex.EncodeToString(b)
	if os.MkdirAll(dir, 0o700) != nil || os.WriteFile(path, []byte(id+"\n"), 0o600) != nil {
		return ""
	}
	return id
}
