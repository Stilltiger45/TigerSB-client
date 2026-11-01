//go:build !windows

package engine

// closeDevice does nothing: there the app hands the core a TUN descriptor and
// closes it itself; closing it here too could close a reused descriptor.
func closeDevice(any) {}
