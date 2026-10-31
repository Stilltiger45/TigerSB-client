//go:build !windows

package main

import (
	"bufio"
	"os"
	"runtime"
	"strconv"
	"strings"
)

func machineID() string {
	for _, path := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
		if b, err := os.ReadFile(path); err == nil {
			if id := strings.TrimSpace(string(b)); id != "" {
				return id
			}
		}
	}
	return ""
}

// osName returns the system and its version: on Linux the distribution's
// name from os-release, such as "Ubuntu 24.04.1 LTS".
func osName() (string, string) {
	switch runtime.GOOS {
	case "linux":
		return "Linux", osRelease("/etc/os-release")
	case "darwin":
		return "macOS", ""
	}
	return runtime.GOOS, ""
}

func osRelease(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		if v, ok := strings.CutPrefix(s.Text(), "PRETTY_NAME="); ok {
			if u, err := strconv.Unquote(v); err == nil {
				return u
			}
			return strings.Trim(v, `"'`)
		}
	}
	return ""
}
