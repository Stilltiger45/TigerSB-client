package desktop

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"sync"

	"github.com/Stilltiger45/TigerSB-client/mobile"
)

// The app talks to the service over a TCP connection to 127.0.0.1, in lines
// of JSON. It first sends {"token": "..."}, the token the service wrote to
// its control file, which only users of this computer can read. Then each
// request {"id": 1, "m": "current", "a": {...}} gets an answer
// {"id": 1, "r": ...} or {"id": 1, "e": "what failed"}, in any order, and the
// service sends {"ev": "status", "phase": ..., "error": ...} whenever the
// VPN's status changes.

type request struct {
	ID    int64           `json:"id"`
	Token string          `json:"token"`
	M     string          `json:"m"`
	A     json.RawMessage `json:"a"`
}

type args struct {
	Key   string   `json:"key"`
	ID    string   `json:"id"`
	Mode  string   `json:"mode"`
	Names []string `json:"names"`
	Event string   `json:"event"`
}

// Server answers the app.
type Server struct {
	vpn   *VPN
	token string

	mu      sync.Mutex
	clients map[*client]struct{}
}

type client struct {
	mu  sync.Mutex
	enc *json.Encoder
}

func (c *client) send(v any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.enc.Encode(v)
}

// NewServer makes a server for the VPN with a fresh random token.
func NewServer(vpn *VPN) *Server {
	b := make([]byte, 32)
	rand.Read(b)
	return &Server{vpn: vpn, token: hex.EncodeToString(b), clients: map[*client]struct{}{}}
}

// Token is what the app must send first.
func (s *Server) Token() string { return s.token }

// Serve answers the connections l accepts until ctx ends.
func (s *Server) Serve(ctx context.Context, l net.Listener) error {
	go func() {
		<-ctx.Done()
		l.Close()
	}()
	cancel := s.vpn.Subscribe(func(st Status) {
		s.broadcast(map[string]any{"ev": "status", "phase": st.Phase, "error": st.Error})
	})
	defer cancel()
	for {
		c, err := l.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		go s.handle(c)
	}
}

func (s *Server) broadcast(v any) {
	s.mu.Lock()
	cs := make([]*client, 0, len(s.clients))
	for c := range s.clients {
		cs = append(cs, c)
	}
	s.mu.Unlock()
	for _, c := range cs {
		c.send(v)
	}
}

func (s *Server) handle(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReaderSize(conn, 64<<10)
	line, err := r.ReadBytes('\n')
	if err != nil {
		return
	}
	var hello request
	if json.Unmarshal(line, &hello) != nil || subtle.ConstantTimeCompare([]byte(hello.Token), []byte(s.token)) != 1 {
		return
	}
	c := &client{enc: json.NewEncoder(conn)}
	s.mu.Lock()
	s.clients[c] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.clients, c)
		s.mu.Unlock()
	}()
	st := s.vpn.Status()
	c.send(map[string]any{"ev": "status", "phase": st.Phase, "error": st.Error})
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			return
		}
		var req request
		if json.Unmarshal(line, &req) != nil {
			continue
		}
		go func() {
			res, err := s.call(req)
			out := map[string]any{"id": req.ID}
			if err != nil {
				out["e"] = err.Error()
			} else {
				out["r"] = res
			}
			c.send(out)
		}()
	}
}

// call runs one request. Results the core gives as JSON text go to the app
// as that text, as on Android.
func (s *Server) call(req request) (any, error) {
	var a args
	if len(req.A) > 0 {
		if err := json.Unmarshal(req.A, &a); err != nil {
			return nil, err
		}
	}
	v := s.vpn
	switch req.M {
	case "current":
		return mobile.Current()
	case "profiles":
		return mobile.Profiles()
	case "addKey":
		return mobile.AddKey(a.Key)
	case "addKeyAside":
		return mobile.AddKeyAside(a.Key)
	case "otherPanel":
		return mobile.OtherPanel(a.Key), nil
	case "refresh":
		if a.ID == "" {
			return mobile.Refresh()
		}
		return mobile.RefreshProfile(a.ID)
	case "select":
		info, err := mobile.Select(a.ID)
		if err == nil {
			// A running VPN moves to the profile by reconnecting.
			v.Reconnect()
		}
		return info, err
	case "delete":
		return mobile.Delete(a.ID)
	case "stats":
		return mobile.Stats()
	case "setProtocolsOff":
		b, _ := json.Marshal(nonNil(a.Names))
		if err := mobile.SetProtocolsOff(string(b)); err != nil {
			return nil, err
		}
		v.Reconnect()
		return nil, nil
	case "mode":
		return v.Mode(), nil
	case "setMode":
		v.SetMode(a.Mode)
		return nil, nil
	case "connect":
		go v.Connect()
		return true, nil
	case "disconnect":
		v.Disconnect()
		return nil, nil
	case "status":
		return v.Status(), nil
	case "wasOn":
		return v.WasOn(), nil
	case "about":
		return mobile.About(), nil
	case "journal":
		return mobile.Journal(), nil
	case "note":
		mobile.Note(a.Event)
		return nil, nil
	}
	return nil, errors.New("неизвестный запрос " + req.M)
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
