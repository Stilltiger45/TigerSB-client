// Package naive carries NaïveProxy transports. Xray has no NaïveProxy, so each
// such transport of a profile becomes an outbound handler added to Xray under
// the transport's tag.
//
// NaïveProxy is an HTTP/2 CONNECT proxy (Caddy with klzgrad's forwardproxy,
// or sing-box) that looks like Chrome: the handler dials TLS through Xray with
// Chrome's fingerprint, keeps one HTTP/2 connection to the server and opens a
// CONNECT stream in it for each connection. When the server agrees, the first
// frames each way carry random padding, as NaïveProxy's own client does, so
// that the sizes of the first packets do not show the TLS of the site inside.
// NaïveProxy carries TCP only.
package naive

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"io"
	mrand "math/rand/v2"
	gonet "net"
	"net/http"
	"net/url"
	"slices"
	"sync"
	"time"

	"github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/infra/conf"
	"github.com/xtls/xray-core/transport"
	"github.com/xtls/xray-core/transport/internet"
	"github.com/xtls/xray-core/transport/internet/tls"
	"golang.org/x/net/http2"

	"github.com/Stilltiger45/TigerSB-client/internal/carry"
	"github.com/Stilltiger45/TigerSB-client/internal/xrayconf"
	"github.com/Stilltiger45/TigerSB-client/pkg/profile"
)

// Hooks returns the engine.Start hooks that add the profile's NaïveProxy
// transports to Xray.
func Hooks(p *profile.Profile) []func(*core.Instance) error {
	var hooks []func(*core.Instance) error
	for _, ob := range xrayconf.Carried(p, "naive") {
		hooks = append(hooks, func(inst *core.Instance) error { return Attach(inst, ob.Tag, ob.Xray) })
	}
	return hooks
}

// config is a NaïveProxy transport as pkg/profile writes it, in Xray's style.
type config struct {
	Settings struct {
		Address  string            `json:"address"`
		Port     uint16            `json:"port"`
		Username string            `json:"username"`
		Password string            `json:"password"`
		Padding  bool              `json:"padding"`
		Headers  map[string]string `json:"headers"`
	} `json:"settings"`
	Stream conf.StreamConfig `json:"streamSettings"`
}

// Attach adds the NaïveProxy outbound to a created, not yet started Xray
// instance.
func Attach(inst *core.Instance, tag string, xray json.RawMessage) error {
	var c config
	if err := json.Unmarshal(xray, &c); err != nil {
		return fmt.Errorf("транспорт %s: %w", tag, err)
	}
	s := c.Settings
	if s.Address == "" || s.Port == 0 {
		return fmt.Errorf("транспорт %s: нет адреса сервера", tag)
	}
	if t := c.Stream.TLSSettings; t == nil || c.Stream.Security != "tls" {
		return fmt.Errorf("транспорт %s: NaïveProxy работает только поверх TLS", tag)
	} else if t.AllowInsecure && t.PinnedPeerCertSha256 == "" {
		// Like Xray, refuse to connect without checking the certificate.
		return fmt.Errorf("транспорт %s: сертификат сервера не закреплён", tag)
	}
	built, err := c.Stream.Build()
	if err != nil {
		return fmt.Errorf("транспорт %s: %w", tag, err)
	}
	stream, err := internet.ToMemoryStreamConfig(built)
	if err != nil {
		return fmt.Errorf("транспорт %s: %w", tag, err)
	}
	header := http.Header{}
	for k, v := range s.Headers {
		header.Set(k, v)
	}
	if s.Username != "" || s.Password != "" {
		header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(s.Username+":"+s.Password)))
	}
	// Go's HTTP/2 client names itself unless told otherwise; NaïveProxy
	// sends no User-Agent with CONNECT.
	if header.Get("User-Agent") == "" {
		header["User-Agent"] = []string{""}
	}
	base, err := carry.NewBase(inst, tag)
	if err != nil {
		return err
	}
	return carry.Add(inst, &Handler{
		Base:    base,
		server:  net.TCPDestination(net.ParseAddress(s.Address), net.Port(s.Port)),
		stream:  stream,
		header:  header,
		padding: s.Padding,
		h2: &http2.Transport{
			DisableCompression: true,
			// Chrome's limits, which it announces in its SETTINGS.
			MaxDecoderHeaderTableSize: 65536,
			MaxHeaderListSize:         262144,
			// A connection the network dropped silently is found by a
			// ping and replaced, instead of new streams hanging in it.
			ReadIdleTimeout: 30 * time.Second,
			PingTimeout:     10 * time.Second,
			IdleConnTimeout: 5 * time.Minute,
		},
	})
}

// Handler is a NaïveProxy outbound. It implements Xray's outbound.Handler.
type Handler struct {
	*carry.Base
	server  net.Destination
	stream  *internet.MemoryStreamConfig
	header  http.Header
	padding bool
	h2      *http2.Transport

	mu    sync.Mutex
	conns []*http2.ClientConn // to the server, the newest last
}

func (h *Handler) Start() error                         { return nil }
func (h *Handler) SenderSettings() *serial.TypedMessage { return nil }
func (h *Handler) ProxySettings() *serial.TypedMessage  { return nil }

func (h *Handler) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, cc := range h.conns {
		cc.Close()
	}
	h.conns = nil
	return nil
}

// Dispatch carries one connection through the NaïveProxy server.
func (h *Handler) Dispatch(ctx context.Context, link *transport.Link) {
	h.Base.Dispatch(ctx, link, "naive", h.connect, h.packets)
}

var errUDP = stderrors.New("NaïveProxy не передаёт UDP")

func (h *Handler) packets(context.Context, *transport.Link, net.Destination) error { return errUDP }

func (h *Handler) connect(ctx context.Context, link *transport.Link, target net.Destination) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	t, err := h.open(ctx, target.NetAddr())
	if err != nil {
		return err
	}
	defer t.Close()
	return h.Stream(ctx, cancel, link, t)
}

// open opens a CONNECT stream to addr (host:port), over a connection already
// open to the server when one takes it.
func (h *Handler) open(ctx context.Context, addr string) (*tunnel, error) {
	cc, err := h.conn(ctx)
	if err != nil {
		return nil, err
	}
	pr, pw := io.Pipe()
	req := &http.Request{
		Method:        http.MethodConnect,
		URL:           &url.URL{Host: addr},
		Host:          addr,
		Header:        h.header.Clone(),
		Body:          pr,
		ContentLength: -1,
	}
	if h.padding {
		req.Header.Set("Padding", paddingHeader(16+mrand.IntN(17)))
	}
	// The request's context is the stream's: ending it resets the stream.
	// A server that does not answer in time is given up on.
	rctx, stop := context.WithCancel(ctx)
	late := time.AfterFunc(timeout, stop)
	resp, err := cc.RoundTrip(req.WithContext(rctx))
	if !late.Stop() && err == nil {
		resp.Body.Close()
		err = stderrors.New("сервер NaïveProxy не ответил")
	}
	if err == nil && (resp.StatusCode != http.StatusOK || resp.ContentLength >= 0) {
		resp.Body.Close()
		// A tunnel has no length. With a wrong password, a server that hides
		// from probes answers as the site it pretends to be, with one.
		if resp.StatusCode == http.StatusProxyAuthRequired || resp.StatusCode == http.StatusUnauthorized || resp.ContentLength >= 0 {
			err = stderrors.New("сервер NaïveProxy не принял имя или пароль")
		} else {
			err = fmt.Errorf("сервер NaïveProxy ответил %s", resp.Status)
		}
	}
	if err != nil {
		stop()
		pw.Close()
		return nil, err
	}
	t := &tunnel{w: pw, r: resp.Body, stop: stop}
	if h.padding && resp.Header.Get("Padding") != "" {
		t.writes, t.reads = paddedFrames, paddedFrames
	}
	return t, nil
}

// conn returns a connection to the server that takes another stream,
// dialling one when none does.
func (h *Handler) conn(ctx context.Context) (*http2.ClientConn, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.conns = slices.DeleteFunc(h.conns, func(cc *http2.ClientConn) bool { return cc.State().Closed })
	for i := len(h.conns) - 1; i >= 0; i-- {
		if cc := h.conns[i]; cc.CanTakeNewRequest() {
			return cc, nil
		}
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	c, err := internet.Dial(ctx, h.server, h.stream)
	if err != nil {
		return nil, err
	}
	if tc, ok := c.(tls.Interface); !ok || tc.NegotiatedProtocol() != http2.NextProtoTLS {
		c.Close()
		return nil, stderrors.New("сервер NaïveProxy не говорит HTTP/2")
	}
	cc, err := h.h2.NewClientConn(c)
	if err != nil {
		c.Close()
		return nil, err
	}
	h.conns = append(h.conns, cc)
	return cc, nil
}

// timeout bounds the dial and the server's answer to CONNECT.
const timeout = 15 * time.Second

// paddedFrames is how many frames each way carry padding, as in NaïveProxy.
const paddedFrames = 8

// tunnel is one CONNECT stream as a connection. While padding is on, the
// first paddedFrames writes and reads each way are frames: the size of the
// payload in two bytes, the size of the padding in one, the payload and as
// many zero bytes.
type tunnel struct {
	w    *io.PipeWriter
	r    io.ReadCloser
	stop context.CancelFunc // resets the stream

	writes, reads int // padded frames still to write and read
	left, pad     int // what remains of the frame being read
}

func (t *tunnel) Write(b []byte) (int, error) {
	if t.writes == 0 {
		return t.w.Write(b)
	}
	n := 0
	for len(b) > 0 && t.writes > 0 {
		chunk := b[:min(len(b), 65535)]
		pad := mrand.IntN(256)
		frame := make([]byte, 3+len(chunk)+pad)
		binary.BigEndian.PutUint16(frame, uint16(len(chunk)))
		frame[2] = byte(pad)
		copy(frame[3:], chunk)
		if _, err := t.w.Write(frame); err != nil {
			return n, err
		}
		t.writes--
		n += len(chunk)
		b = b[len(chunk):]
	}
	if len(b) > 0 {
		m, err := t.w.Write(b)
		return n + m, err
	}
	return n, nil
}

func (t *tunnel) Read(b []byte) (int, error) {
	for t.left == 0 {
		if t.pad > 0 {
			if _, err := io.CopyN(io.Discard, t.r, int64(t.pad)); err != nil {
				return 0, err
			}
			t.pad = 0
		}
		if t.reads == 0 {
			return t.r.Read(b)
		}
		var head [3]byte
		if _, err := io.ReadFull(t.r, head[:]); err != nil {
			return 0, err
		}
		t.reads--
		t.left, t.pad = int(binary.BigEndian.Uint16(head[:])), int(head[2])
	}
	n, err := t.r.Read(b[:min(len(b), t.left)])
	t.left -= n
	return n, err
}

func (t *tunnel) Close() error {
	t.w.Close()
	err := t.r.Close()
	t.stop()
	return err
}

func (t *tunnel) LocalAddr() gonet.Addr            { return &gonet.TCPAddr{} }
func (t *tunnel) RemoteAddr() gonet.Addr           { return &gonet.TCPAddr{} }
func (t *tunnel) SetDeadline(time.Time) error      { return nil }
func (t *tunnel) SetReadDeadline(time.Time) error  { return nil }
func (t *tunnel) SetWriteDeadline(time.Time) error { return nil }

// paddingHeader returns n characters for the Padding header, written as
// NaïveProxy writes them: up to 16 random ones that HPACK leaves unencoded,
// the rest '~'.
func paddingHeader(n int) string {
	var bits [8]byte
	rand.Read(bits[:])
	r := binary.LittleEndian.Uint64(bits[:])
	b := make([]byte, n)
	for i := range b {
		if i < 16 {
			b[i] = "!#$()+<>?@[]^`{}"[r&15]
			r >>= 4
		} else {
			b[i] = '~'
		}
	}
	return string(b)
}
