// Command soak runs the test servers for the long emulator test
// (scripts/soak-android.sh): the app on the emulator connects to them at
// 10.0.2.2 and its traffic leaves through the runner's network.
//
// There are three servers, as a subscription of the soak profile "mix" lists
// them (mix.txt): plain VLESS over TCP, Hysteria 2 and TUIC. Each sits behind
// a stand-in for DPI (dpi.go) whose level -dpi sets and the control port
// changes: GET /dpi?level=off|weak|strong. Connections to BulkIP go to a bulk
// server for downloads, uploads and a stream both ways (bulk.go).
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"flag"
	"fmt"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/xtls/xray-core/common/uuid"
	"github.com/xtls/xray-core/core"

	"github.com/Stilltiger45/TigerSB-client/internal/engine"
)

// Config are the servers' ports and the user's credentials.
type Config struct {
	VLESS, Hysteria, TUIC int // public ports, behind the DPI
	Bulk, Control         int // on 127.0.0.1; 0 means any free port
	ID, Password          string
	Level                 Level
}

// Servers are the running servers.
type Servers struct {
	DPI  *DPI
	Bulk *bulkServer
	// BulkAddr is where the bulk server listens.
	BulkAddr string
	xray     *core.Instance
}

func main() {
	var c Config
	flag.IntVar(&c.VLESS, "port", 20443, "TCP port of VLESS")
	flag.IntVar(&c.Hysteria, "hy", 20444, "UDP port of Hysteria 2")
	flag.IntVar(&c.TUIC, "tuic", 20445, "UDP port of TUIC")
	flag.IntVar(&c.Bulk, "bulk", 20480, "port of the bulk server on 127.0.0.1")
	flag.IntVar(&c.Control, "control", 20490, "port of the control on 127.0.0.1")
	flag.StringVar(&c.ID, "id", "5783a3e7-e373-51cd-8642-c83782b807c5", "user id")
	flag.StringVar(&c.Password, "pw", "soak-password", "password of Hysteria 2 and TUIC")
	dpi := flag.String("dpi", "off", "level of the DPI: off, weak (1) or strong")
	flag.Parse()
	level, ok := ParseLevel(*dpi)
	if !ok {
		log.Fatalf("unknown -dpi %q", *dpi)
	}
	c.Level = level
	s, err := Start(c)
	if err != nil {
		log.Fatal(err)
	}
	defer s.xray.Close()
	go func() {
		for range time.Tick(time.Minute) {
			s.DPI.Report("minute")
			log.Printf("bulk: down %d, up %d", s.Bulk.down.Load(), s.Bulk.up.Load())
		}
	}()
	mux := http.NewServeMux()
	mux.HandleFunc("/dpi", func(w http.ResponseWriter, r *http.Request) {
		l, ok := ParseLevel(r.URL.Query().Get("level"))
		if !ok {
			http.Error(w, "unknown level", http.StatusBadRequest)
			return
		}
		s.DPI.SetLevel(l)
		fmt.Fprintln(w, l)
	})
	go http.ListenAndServe("127.0.0.1:"+strconv.Itoa(c.Control), mux)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	s.DPI.Report("end")
	log.Printf("bulk: down %d, up %d", s.Bulk.down.Load(), s.Bulk.up.Load())
}

// Start runs the servers.
func Start(c Config) (*Servers, error) {
	id, err := uuid.ParseString(c.ID)
	if err != nil {
		return nil, err
	}
	s := &Servers{DPI: NewDPI(c.Level, id.Bytes()), Bulk: &bulkServer{}}

	bl, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(c.Bulk))
	if err != nil {
		return nil, err
	}
	s.BulkAddr = bl.Addr().String()
	go s.Bulk.serve(bl)

	cert, certPEM, keyPEM, err := selfSigned("soak.example.org")
	if err != nil {
		return nil, err
	}
	// Each server listens on a free port of its own behind its public one.
	inner := func(network string) (string, error) {
		if network == "tcp" {
			l, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				return "", err
			}
			defer l.Close()
			return l.Addr().String(), nil
		}
		pc, err := net.ListenPacket("udp", "127.0.0.1:0")
		if err != nil {
			return "", err
		}
		defer pc.Close()
		return pc.LocalAddr().String(), nil
	}
	vlessAddr, err := inner("tcp")
	if err != nil {
		return nil, err
	}
	hyAddr, err := inner("udp")
	if err != nil {
		return nil, err
	}
	port := func(addr string) int {
		_, p, _ := net.SplitHostPort(addr)
		n, _ := strconv.Atoi(p)
		return n
	}
	tlsSettings := map[string]any{
		"alpn":         []string{"h3"},
		"certificates": []any{map[string]any{"certificate": lines(certPEM), "key": lines(keyPEM)}},
	}
	cfg, _ := json.Marshal(map[string]any{
		"log": map[string]any{"loglevel": "warning"},
		"inbounds": []any{
			map[string]any{
				"listen": "127.0.0.1", "port": port(vlessAddr), "protocol": "vless",
				"settings": map[string]any{"clients": []any{map[string]any{"id": c.ID}}, "decryption": "none"},
			},
			map[string]any{
				"listen": "127.0.0.1", "port": port(hyAddr), "protocol": "hysteria",
				"settings": map[string]any{"version": 2, "clients": []any{map[string]any{"auth": c.Password}}},
				"streamSettings": map[string]any{
					"network": "hysteria", "security": "tls", "tlsSettings": tlsSettings,
					"hysteriaSettings": map[string]any{"version": 2},
				},
			},
		},
		"outbounds": []any{
			map[string]any{"tag": "out", "protocol": "freedom"},
			map[string]any{"tag": "bulk", "protocol": "freedom", "settings": map[string]any{"redirect": s.BulkAddr}},
		},
		"routing": map[string]any{"rules": []any{
			map[string]any{"type": "field", "ip": []string{BulkIP}, "port": BulkPort, "outboundTag": "bulk"},
		}},
	})
	s.xray, err = engine.Start(cfg)
	if err != nil {
		return nil, err
	}

	tuicPC, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	if err := runTUIC(tuicPC, cert, c.ID, c.Password, s.BulkAddr); err != nil {
		return nil, err
	}

	l, err := net.Listen("tcp", ":"+strconv.Itoa(c.VLESS))
	if err != nil {
		return nil, err
	}
	go s.DPI.ServeTCP(l, vlessAddr)
	for _, u := range []struct {
		port   int
		target string
	}{{c.Hysteria, hyAddr}, {c.TUIC, tuicPC.LocalAddr().String()}} {
		pc, err := net.ListenPacket("udp", ":"+strconv.Itoa(u.port))
		if err != nil {
			return nil, err
		}
		go s.DPI.ServeUDP(pc, u.target)
	}
	log.Printf("VLESS :%d, Hysteria 2 :%d, TUIC :%d, bulk %s, DPI %s", c.VLESS, c.Hysteria, c.TUIC, s.BulkAddr, c.Level)
	return s, nil
}

// selfSigned makes a certificate for name, as a server without a real
// domain has; the app pins it when it adds the key.
func selfSigned(name string) (tls.Certificate, []byte, []byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, nil, nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: name},
		DNSNames:     []string{name},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, nil, nil, err
	}
	kd, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return tls.Certificate{}, nil, nil, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kd})
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, certPEM, keyPEM, nil
}

// lines splits PEM as Xray takes it inline.
func lines(b []byte) []string {
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}
