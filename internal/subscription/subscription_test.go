package subscription

import (
	"context"
	"crypto/ed25519"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Stilltiger45/TigerSB-client/pkg/profile"
)

func signedFixture(t *testing.T, priv ed25519.PrivateKey, edit func(string) string) []byte {
	t.Helper()
	b, err := os.ReadFile("../../pkg/profile/testdata/profile.json")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if edit != nil {
		s = edit(s)
	}
	env, err := profile.Sign(priv, []byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return env
}

type server struct {
	*httptest.Server
	body atomic.Value // []byte
	ua   atomic.Value // string
}

func newServer(t *testing.T, body []byte) *server {
	s := &server{}
	s.body.Store(body)
	s.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.ua.Store(r.UserAgent())
		_, _ = w.Write(s.body.Load().([]byte))
	}))
	t.Cleanup(s.Close)
	return s
}

func TestImportPinnedKeyAndRefresh(t *testing.T) {
	pub, priv, _ := profile.GenerateKey()
	srv := newServer(t, signedFixture(t, priv, nil))
	c := &Client{HTTP: srv.Client()}
	key := profile.Key{Kind: profile.KindSubscription, URL: srv.URL + "/sub", PK: pub}

	st, p, err := c.Import(context.Background(), key.String())
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != "home" || st.PK != profile.EncodeKey(pub) {
		t.Fatalf("state %+v", st)
	}
	if !strings.HasPrefix(srv.ua.Load().(string), "TigerSB-client/") {
		t.Errorf("user agent %q", srv.ua.Load())
	}

	dir := t.TempDir()
	if err := Save(dir, st); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(filepath.Join(dir, stateFile)); runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Errorf("state file mode %v", fi.Mode().Perm())
	}
	st, err = Load(dir)
	if err != nil {
		t.Fatal(err)
	}

	// The profile's own update URL points elsewhere; Refresh falls back to the key URL.
	srv.body.Store(signedFixture(t, priv, func(s string) string { return strings.Replace(s, "Домашняя сеть", "Новое имя", 1) }))
	p, err = c.Refresh(context.Background(), st)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "Новое имя" {
		t.Errorf("refresh kept old profile: %q", p.Name)
	}

	// A profile signed by another key is refused even from the same URL.
	_, otherPriv, _ := profile.GenerateKey()
	srv.body.Store(signedFixture(t, otherPriv, nil))
	if _, err := c.Refresh(context.Background(), st); !errors.Is(err, profile.ErrForged) || !strings.Contains(err.Error(), "чужим ключом") {
		t.Errorf("foreign key accepted on refresh: %v", err)
	}
}

func TestImportWrongPinnedKey(t *testing.T) {
	_, priv, _ := profile.GenerateKey()
	otherPub, _, _ := profile.GenerateKey()
	srv := newServer(t, signedFixture(t, priv, nil))
	c := &Client{HTTP: srv.Client()}
	key := profile.Key{Kind: profile.KindSubscription, URL: srv.URL, PK: otherPub}
	if _, _, err := c.Import(context.Background(), key.String()); err == nil {
		t.Error("profile signed by another key imported")
	}
}

func TestImportBareURLPinsFirstKey(t *testing.T) {
	pub, priv, _ := profile.GenerateKey()
	srv := newServer(t, signedFixture(t, priv, nil))
	c := &Client{HTTP: srv.Client()}
	st, _, err := c.Import(context.Background(), srv.URL+"/sub")
	if err != nil {
		t.Fatal(err)
	}
	if st.PK != profile.EncodeKey(pub) {
		t.Error("first key not pinned")
	}
}

func TestImportVLESS(t *testing.T) {
	c := &Client{}
	st, p, err := c.Import(context.Background(), "vless://5783a3e7-e373-51cd-8642-c83782b807c5@1.2.3.4:443?security=none#x")
	if err != nil {
		t.Fatal(err)
	}
	if st.Envelope != nil || st.Profile == nil || p.Name != "x" {
		t.Errorf("state %+v", st)
	}
	again, err := st.Open()
	if err != nil || again.Name != "x" {
		t.Errorf("reopen: %v", err)
	}
	if p2, err := c.Refresh(context.Background(), st); err != nil || p2.Name != "x" {
		t.Errorf("refresh of a vless key: %v", err)
	}
}

func TestHTTPError(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	}))
	defer srv.Close()
	c := &Client{HTTP: srv.Client()}
	if _, _, err := c.Import(context.Background(), srv.URL); err == nil || !strings.Contains(err.Error(), "403") {
		t.Errorf("got %v", err)
	}
}

func TestLoadMissing(t *testing.T) {
	if _, err := Load(t.TempDir()); err != ErrNoState {
		t.Errorf("got %v", err)
	}
}
