// Command soakload runs on the emulator during the soak test
// (scripts/soak-android.sh) and loads the VPN like an app: a download, an
// upload or a stream both ways to the bulk server of scripts/soak, reached by
// its address through the tunnel. Each result is one line; a failure starts
// with FAIL.
package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/Stilltiger45/TigerSB-client/scripts/soak/load"
)

func main() {
	addr := flag.String("addr", "198.51.100.7:9000", "bulk server")
	mode := flag.String("mode", "dl", "dl, ul or stream")
	size := flag.Int64("size", 4<<20, "bytes to download or upload")
	timeout := flag.Duration("timeout", 30*time.Second, "time a download or upload may take")
	dur := flag.Duration("for", 10*time.Minute, "how long the stream runs")
	rate := flag.Int64("rate", 256<<10, "bytes a second the stream sends each way")
	stall := flag.Duration("stall", 15*time.Second, "silence after which the stream is opened again")
	flag.Parse()
	dial := func(ctx context.Context) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "tcp", *addr)
	}
	ctx := context.Background()
	switch *mode {
	case "dl", "ul":
		ctx, cancel := context.WithTimeout(ctx, *timeout)
		defer cancel()
		f := load.Download
		if *mode == "ul" {
			f = load.Upload
		}
		d, err := f(ctx, dial, *size)
		if err != nil {
			fmt.Printf("FAIL %s %d Б: %v\n", *mode, *size, err)
			os.Exit(1)
		}
		fmt.Printf("%s %.1f МБ за %.1f с, %.1f МБ/с\n", *mode, float64(*size)/(1<<20), d.Seconds(), float64(*size)/(1<<20)/d.Seconds())
	case "stream":
		start := time.Now()
		show := func(r load.StreamResult) string {
			return fmt.Sprintf("отправлено %.1f МБ, получено %.1f МБ, самая долгая пауза %.0f с, переподключений %d",
				float64(r.Up)/(1<<20), float64(r.Down)/(1<<20), r.MaxGap.Seconds(), r.Reconnects)
		}
		r := load.Stream(ctx, dial, *dur, *rate, *stall, func(r load.StreamResult) {
			fmt.Printf("%3.0f с: %s\n", time.Since(start).Seconds(), show(r))
		})
		verdict := "ok"
		if r.Down == 0 || r.MaxGap > 2**stall {
			verdict = "FAIL"
		}
		fmt.Printf("%s поток: %s\n", verdict, show(r))
	default:
		fmt.Println("FAIL неизвестный режим", *mode)
		os.Exit(2)
	}
}
