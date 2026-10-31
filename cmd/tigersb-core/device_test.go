package main

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// TestThisDevice checks that the computer sends a steady hwid of 16 hex
// digits, its system and its name, and that without a machine id the hwid
// comes from an id kept in the state directory.
func TestThisDevice(t *testing.T) {
	dir := t.TempDir()
	d := thisDevice(dir)
	if !regexp.MustCompile(`^[0-9a-f]{16}$`).MatchString(d.HWID) || d.OS == "" {
		t.Fatalf("device %+v", d)
	}
	if again := thisDevice(dir); again.HWID != d.HWID {
		t.Errorf("hwid changed: %s, %s", d.HWID, again.HWID)
	}
	id := savedID(dir)
	if id == "" || savedID(dir) != id {
		t.Fatalf("saved id %q is not kept", id)
	}
	if _, err := os.Stat(filepath.Join(dir, "device-id")); err != nil {
		t.Error(err)
	}
	if other := savedID(t.TempDir()); other == id {
		t.Error("two state directories got the same id")
	}
}
