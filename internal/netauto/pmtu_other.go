//go:build !linux

package netauto

import (
	"context"
	"net"
)

// echo measures nothing outside Linux and Android: the path stays unknown.
func echo(context.Context, net.IP, int) bool { return false }
