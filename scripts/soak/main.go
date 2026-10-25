// Command soak runs a plain VLESS server for the long emulator test
// (scripts/soak-android.sh): the app on the emulator connects to it at
// 10.0.2.2 and its traffic leaves through the runner's network.
package main

import (
	"encoding/json"
	"flag"
	"log"
	"os"
	"os/signal"

	"github.com/Stilltiger45/TigerSB-client/internal/engine"
)

func main() {
	port := flag.Int("port", 20443, "TCP port")
	id := flag.String("id", "5783a3e7-e373-51cd-8642-c83782b807c5", "VLESS user id")
	flag.Parse()
	cfg, _ := json.Marshal(map[string]any{
		"log": map[string]any{"loglevel": "warning"},
		"inbounds": []any{map[string]any{
			"listen": "0.0.0.0", "port": *port, "protocol": "vless",
			"settings": map[string]any{"clients": []any{map[string]any{"id": *id}}, "decryption": "none"},
		}},
		"outbounds": []any{map[string]any{"protocol": "freedom"}},
	})
	inst, err := engine.Start(cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer inst.Close()
	log.Printf("VLESS on :%d", *port)
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	<-stop
}
