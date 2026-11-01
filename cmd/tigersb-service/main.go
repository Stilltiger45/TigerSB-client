// Command tigersb-service is the VPN service of TigerSB for Windows. It holds
// the core and the TUN and runs without anyone logged in; the app controls
// it (package desktop).
//
//	tigersb-service install    registers the service and starts it
//	tigersb-service uninstall  stops the service and removes it
//	tigersb-service console    runs it in this console, for debugging
//
// Windows starts it with no arguments.
package main

import (
	"fmt"
	"os"
)

// version is set by the build: -ldflags "-X main.version=0.1.0".
var version = "dev"

// serviceName is the service's name for Windows.
const serviceName = "TigerSB"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "tigersb-service:", err)
		os.Exit(1)
	}
}
