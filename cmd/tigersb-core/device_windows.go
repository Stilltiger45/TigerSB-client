package main

import (
	"strconv"
	"strings"

	"golang.org/x/sys/windows/registry"
)

func machineID() string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Cryptography`, registry.QUERY_VALUE|registry.WOW64_64KEY)
	if err != nil {
		return ""
	}
	defer k.Close()
	id, _, _ := k.GetStringValue("MachineGuid")
	return strings.TrimSpace(id)
}

// osName returns "Windows" and its version, such as "11 24H2 (26100)". The
// registry calls Windows 11 "Windows 10" in ProductName; the build tells
// them apart.
func osName() (string, string) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows NT\CurrentVersion`, registry.QUERY_VALUE|registry.WOW64_64KEY)
	if err != nil {
		return "Windows", ""
	}
	defer k.Close()
	build, _, _ := k.GetStringValue("CurrentBuildNumber")
	display, _, _ := k.GetStringValue("DisplayVersion")
	major := "10"
	if n, err := strconv.Atoi(build); err == nil && n >= 22000 {
		major = "11"
	}
	v := major
	if display != "" {
		v += " " + display
	}
	if build != "" {
		v += " (" + build + ")"
	}
	return "Windows", v
}
