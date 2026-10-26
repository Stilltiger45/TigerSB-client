//go:build !linux

package engine

// linkOptions does nothing off Linux and Android: the LTE mode is for phones.
func linkOptions(uintptr, Link) {}
