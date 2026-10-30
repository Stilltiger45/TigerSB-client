package main

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/amnezia-vpn/amneziawg-go/v3/conn"
	"github.com/amnezia-vpn/amneziawg-go/v3/device"
	"github.com/amnezia-vpn/amneziawg-go/v3/tun"
)

// The AmneziaWG 3 server of the soak profile "awg" (awg.txt) and its one
// client. The keys are for this test only.
const (
	awgServerKey = "iI1ZiLtkHvwM/C46ZmMm8IgBc9tRyXVI+bUHApd8plY="
	awgClientPub = "NUhkBWpf0G7U5X86P8d+Uw+2Rs5w5HtTbtYNVRTOEQM="
	awgHeaderKey = "AT6vwtnuRKNvYlbI0b58T6Lm7zMoRXv+EJWCqAdM8hc="
	// AWGTun is the server's tunnel. soak-android.sh gives it 10.66.0.1/24
	// and sends the client's traffic (10.66.0.2) to the internet.
	AWGTun = "awg0"
)

// runAWG runs the AmneziaWG server on the UDP port. Its tunnel is a real TUN
// device, so the server needs CAP_NET_ADMIN.
func runAWG(port int) (*device.Device, error) {
	t, err := tun.CreateTUN(AWGTun, 1420)
	if err != nil {
		return nil, fmt.Errorf("TUN для AmneziaWG: %w", err)
	}
	dev := device.NewDevice(t, conn.NewStdNetBind(), device.NewLogger(device.LogLevelError, "awg: "))
	hexKey := func(k string) string {
		b, _ := base64.StdEncoding.DecodeString(k)
		return hex.EncodeToString(b)
	}
	uapi := strings.Join([]string{
		"private_key=" + hexKey(awgServerKey),
		fmt.Sprintf("listen_port=%d", port),
		"s1=24", "s2=32", "s3=16", "s4=12",
		"h1=100000-199999", "h2=200000-299999", "h3=300000-399999", "h4=400000-499999",
		"header_protection_key=" + hexKey(awgHeaderKey),
		"public_key=" + hexKey(awgClientPub),
		"allowed_ip=10.66.0.2/32",
	}, "\n") + "\n"
	if err := dev.IpcSet(uapi); err != nil {
		dev.Close()
		return nil, err
	}
	if err := dev.Up(); err != nil {
		dev.Close()
		return nil, err
	}
	return dev, nil
}
