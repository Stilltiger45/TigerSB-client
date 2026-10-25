package meter_test

import (
	"os"
	"os/exec"
	"testing"
)

// spliceEnv turns off Xray's splice copy for freedom outbounds. The test's
// servers are freedom outbounds, and spliced bytes skip the outbound counters
// (VPN outbounds behind the app's TUN never splice). Xray reads the variable
// at start-up, so the tests run again in a child process that has it.
const spliceEnv = "XRAY_BUF_SPLICE"

func TestMain(m *testing.M) {
	if os.Getenv(spliceEnv) == "" {
		cmd := exec.Command(os.Args[0], os.Args[1:]...)
		cmd.Env = append(os.Environ(), spliceEnv+"=disable")
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			if e, ok := err.(*exec.ExitError); ok {
				os.Exit(e.ExitCode())
			}
			panic(err)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}
