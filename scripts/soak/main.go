// Command soak runs a plain VLESS server for the long emulator test
// (scripts/soak-android.sh): the app on the emulator connects to it at
// 10.0.2.2 and its traffic leaves through the runner's network.
//
// With -dpi the server sits behind a stand-in for DPI: it reads the first
// segment of each connection and resets the connection when the segment
// carries the whole VLESS user id, so the app reaches the server only with
// its connection start fragmented.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/xtls/xray-core/common/uuid"

	"github.com/Stilltiger45/TigerSB-client/internal/engine"
)

func main() {
	port := flag.Int("port", 20443, "TCP port")
	id := flag.String("id", "5783a3e7-e373-51cd-8642-c83782b807c5", "VLESS user id")
	dpi := flag.Bool("dpi", false, "reset connections whose first segment carries the user id")
	flag.Parse()
	vlessPort := *port
	if *dpi {
		vlessPort = *port + 1
	}
	cfg, _ := json.Marshal(map[string]any{
		"log": map[string]any{"loglevel": "warning"},
		"inbounds": []any{map[string]any{
			"listen": "0.0.0.0", "port": vlessPort, "protocol": "vless",
			"settings": map[string]any{"clients": []any{map[string]any{"id": *id}}, "decryption": "none"},
		}},
		"outbounds": []any{map[string]any{"protocol": "freedom"}},
	})
	inst, err := engine.Start(cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer inst.Close()
	log.Printf("VLESS on :%d", vlessPort)

	var box *dpiBox
	if *dpi {
		u, err := uuid.ParseString(*id)
		if err != nil {
			log.Fatal(err)
		}
		box = &dpiBox{target: "127.0.0.1:" + strconv.Itoa(vlessPort), sign: u.Bytes()}
		go box.run(*port)
		go func() {
			for range time.Tick(time.Minute) {
				box.report()
			}
		}()
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	if box != nil {
		box.report()
	}
}

type dpiBox struct {
	target          string
	sign            []byte
	blocked, passed atomic.Int64
}

func (d *dpiBox) report() {
	log.Printf("dpi: blocked %d, passed %d", d.blocked.Load(), d.passed.Load())
}

func (d *dpiBox) run(port int) {
	l, err := net.Listen("tcp", ":"+strconv.Itoa(port))
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("DPI in front on :%d", port)
	for {
		c, err := l.Accept()
		if err != nil {
			log.Fatal(err)
		}
		go d.serve(c)
	}
}

func (d *dpiBox) serve(c net.Conn) {
	defer c.Close()
	first := make([]byte, 16384)
	c.SetReadDeadline(time.Now().Add(10 * time.Second))
	n, err := c.Read(first)
	if err != nil {
		return
	}
	c.SetReadDeadline(time.Time{})
	if bytes.Contains(first[:n], d.sign) {
		d.blocked.Add(1)
		c.(*net.TCPConn).SetLinger(0)
		return
	}
	d.passed.Add(1)
	up, err := net.Dial("tcp", d.target)
	if err != nil {
		return
	}
	defer up.Close()
	if _, err := up.Write(first[:n]); err != nil {
		return
	}
	go func() {
		io.Copy(up, c)
		up.(*net.TCPConn).CloseWrite()
	}()
	io.Copy(c, up)
}
