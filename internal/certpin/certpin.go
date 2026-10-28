// Package certpin replaces "allowInsecure" in share links and third-party
// subscriptions with a pin of the server's certificate.
//
// Servers with self-signed certificates come with insecure=1 in their links,
// but Xray no longer accepts allowInsecure. So when a profile is imported the
// client connects to each such server once, takes the certificate it shows
// and pins its SHA-256 (pinnedPeerCertSha256): from then on the server must
// show that same certificate.
package certpin

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/apernet/quic-go"
	"github.com/xtls/xray-core/transport/internet/finalmask/salamander"

	"github.com/Stilltiger45/TigerSB-client/pkg/profile"
)

// Timeout bounds the connection to one server.
var Timeout = 5 * time.Second

// Profile pins every transport of p that asks for allowInsecure, in place.
// Transports whose server does not answer are dropped and counted in dropped;
// an exit left without transports is an error.
func Profile(ctx context.Context, p *profile.Profile) (dropped int, err error) {
	type job struct {
		exit, tr int
		srv      server
	}
	var jobs []job
	for i, e := range p.Exits {
		for j, t := range e.Transports {
			if srv, ok := insecure(t.Xray); ok {
				jobs = append(jobs, job{i, j, srv})
			}
		}
	}
	if len(jobs) == 0 {
		return 0, nil
	}

	pins := make([]string, len(jobs))
	errs := make([]error, len(jobs))
	var wg sync.WaitGroup
	for k, jb := range jobs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pins[k], errs[k] = fetch(ctx, jb.srv)
		}()
	}
	wg.Wait()

	drop := map[[2]int]bool{}
	var firstErr error
	for k, jb := range jobs {
		t := &p.Exits[jb.exit].Transports[jb.tr]
		if errs[k] == nil {
			t.Xray, errs[k] = setPin(t.Xray, pins[k])
		}
		if errs[k] != nil {
			drop[[2]int{jb.exit, jb.tr}] = true
			if firstErr == nil {
				firstErr = fmt.Errorf("%s: %w", or(t.Name, t.ID), errs[k])
			}
		}
	}
	for i := range p.Exits {
		kept := p.Exits[i].Transports[:0]
		for j, t := range p.Exits[i].Transports {
			if drop[[2]int{i, j}] {
				dropped++
				continue
			}
			kept = append(kept, t)
		}
		p.Exits[i].Transports = kept
		if len(kept) == 0 {
			return dropped, fmt.Errorf("не удалось получить сертификат сервера (%w)", firstErr)
		}
	}
	return dropped, nil
}

// server is where to take a certificate from.
type server struct {
	Address    string
	Port       int
	ServerName string
	ALPN       []string
	QUIC       bool   // Hysteria, TUIC and NaïveProxy over HTTP/3: TLS inside QUIC over UDP
	Salamander string // the obfuscation password of a Hysteria server, if any
}

// insecure reads a transport's Xray outbound and reports the server to pin
// when its TLS settings ask for allowInsecure and carry no pin yet.
func insecure(xray json.RawMessage) (server, bool) {
	var ob struct {
		Settings struct {
			Address string `json:"address"`
			Port    int    `json:"port"`
			Vnext   []struct {
				Address string `json:"address"`
				Port    int    `json:"port"`
			} `json:"vnext"`
			Servers []struct {
				Address string `json:"address"`
				Port    int    `json:"port"`
			} `json:"servers"`
			QUIC bool `json:"quic"` // NaïveProxy over HTTP/3
		} `json:"settings"`
		Stream struct {
			Network  string `json:"network"`
			Security string `json:"security"`
			TLS      struct {
				ServerName    string   `json:"serverName"`
				ALPN          []string `json:"alpn"`
				AllowInsecure bool     `json:"allowInsecure"`
				Pin           string   `json:"pinnedPeerCertSha256"`
			} `json:"tlsSettings"`
			Mask struct {
				UDP []struct {
					Type     string `json:"type"`
					Settings struct {
						Password string `json:"password"`
					} `json:"settings"`
				} `json:"udp"`
			} `json:"finalmask"`
		} `json:"streamSettings"`
	}
	if json.Unmarshal(xray, &ob) != nil {
		return server{}, false
	}
	st := ob.Stream
	if st.Security != "tls" || !st.TLS.AllowInsecure || st.TLS.Pin != "" {
		return server{}, false
	}
	srv := server{Address: ob.Settings.Address, Port: ob.Settings.Port, ServerName: st.TLS.ServerName, ALPN: st.TLS.ALPN}
	if len(ob.Settings.Vnext) > 0 {
		srv.Address, srv.Port = ob.Settings.Vnext[0].Address, ob.Settings.Vnext[0].Port
	}
	if len(ob.Settings.Servers) > 0 {
		srv.Address, srv.Port = ob.Settings.Servers[0].Address, ob.Settings.Servers[0].Port
	}
	if st.Network == "hysteria" || st.Network == "tuic" || ob.Settings.QUIC {
		srv.QUIC = true
		for _, m := range st.Mask.UDP {
			if m.Type == "salamander" {
				srv.Salamander = m.Settings.Password
			}
		}
	}
	if srv.ServerName == "" && net.ParseIP(srv.Address) == nil {
		srv.ServerName = srv.Address
	}
	return srv, srv.Address != "" && srv.Port > 0
}

// fetch connects to the server and returns the hex SHA-256 of the
// certificate it shows, as pinnedPeerCertSha256 takes it.
func fetch(ctx context.Context, srv server) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	conf := &tls.Config{
		ServerName: srv.ServerName,
		NextProtos: srv.ALPN,
		// The certificate is unknown yet: that is what is being fetched.
		InsecureSkipVerify: true,
	}
	addr := net.JoinHostPort(srv.Address, strconv.Itoa(srv.Port))
	var state tls.ConnectionState
	if srv.QUIC {
		s, err := fetchQUIC(ctx, addr, srv.Salamander, conf)
		if err != nil {
			return "", err
		}
		state = s
	} else {
		c, err := (&tls.Dialer{Config: conf}).DialContext(ctx, "tcp", addr)
		if err != nil {
			return "", err
		}
		state = c.(*tls.Conn).ConnectionState()
		c.Close()
	}
	if len(state.PeerCertificates) == 0 {
		return "", errors.New("сервер не прислал сертификат")
	}
	sum := sha256.Sum256(state.PeerCertificates[0].Raw)
	return hex.EncodeToString(sum[:]), nil
}

func fetchQUIC(ctx context.Context, addr, obfs string, conf *tls.Config) (tls.ConnectionState, error) {
	raddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return tls.ConnectionState{}, err
	}
	udp, err := net.ListenUDP("udp", nil)
	if err != nil {
		return tls.ConnectionState{}, err
	}
	defer udp.Close()
	var pc net.PacketConn = udp
	if obfs != "" {
		o, err := salamander.NewSalamanderObfuscator([]byte(obfs))
		if err != nil {
			return tls.ConnectionState{}, err
		}
		pc = &salamanderConn{udp, o}
	}
	if len(conf.NextProtos) == 0 {
		conf.NextProtos = []string{"h3"}
	}
	conn, err := quic.Dial(ctx, pc, raddr, conf, &quic.Config{})
	if err != nil {
		return tls.ConnectionState{}, err
	}
	defer conn.CloseWithError(0, "")
	return conn.ConnectionState().TLS, nil
}

// salamanderConn obfuscates the packets of a Hysteria handshake. Xray's own
// wrapper only transforms buffers for its transport stack and does no I/O.
// The UDP socket is held as a plain PacketConn so that quic-go cannot reach
// past ReadFrom and WriteTo to its batch reads.
type salamanderConn struct {
	net.PacketConn
	obfs *salamander.SalamanderObfuscator
}

const saltLen = 8

func (c *salamanderConn) ReadFrom(p []byte) (int, net.Addr, error) {
	buf := make([]byte, len(p)+saltLen)
	for {
		n, addr, err := c.PacketConn.ReadFrom(buf)
		if err != nil {
			return 0, addr, err
		}
		if m := c.obfs.Deobfuscate(buf[:n], p); m > 0 {
			return m, addr, nil
		}
	}
}

func (c *salamanderConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	buf := make([]byte, len(p)+saltLen)
	if _, err := c.PacketConn.WriteTo(buf[:c.obfs.Obfuscate(p, buf)], addr); err != nil {
		return 0, err
	}
	return len(p), nil
}

// setPin swaps allowInsecure for the pin in a transport's outbound.
func setPin(xray json.RawMessage, pin string) (json.RawMessage, error) {
	var ob map[string]any
	if err := json.Unmarshal(xray, &ob); err != nil {
		return nil, err
	}
	st, _ := ob["streamSettings"].(map[string]any)
	ts, _ := st["tlsSettings"].(map[string]any)
	if ts == nil {
		return nil, errors.New("нет настроек TLS")
	}
	delete(ts, "allowInsecure")
	ts["pinnedPeerCertSha256"] = pin
	return json.Marshal(ob)
}

func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
