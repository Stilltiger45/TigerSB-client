// Package subscription imports a key, downloads the profile (a signed TigerSB
// profile or another VPN's list of servers) and keeps it on disk.
package subscription

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/Stilltiger45/TigerSB-client/internal/certpin"
	"github.com/Stilltiger45/TigerSB-client/pkg/profile"
)

// UserAgent is sent with every subscription request.
var UserAgent = "TigerSB-client/dev"

// compatUserAgent is tried once when a third-party panel answers our own
// user agent with something that holds no servers (some return a web page or
// refuse unknown clients).
const compatUserAgent = "v2rayNG/1.10.16"

// Device describes this device to subscription servers. Panels with a device
// limit (Remnawave HWID and similar) count devices by these headers.
type Device struct {
	HWID      string
	OS        string
	OSVersion string
	Model     string
}

// ThisDevice is sent with every subscription request; empty fields are left out.
var ThisDevice Device

// maxBody caps the size of a subscription response.
const maxBody = 4 << 20

// State is what the client keeps between runs.
type State struct {
	// Key is the string the user imported.
	Key string `json:"key"`
	// PK is the pinned signing key (base64url); empty for a share link or a
	// third-party subscription.
	PK string `json:"pk,omitempty"`
	// Envelope is the last signed profile the panel sent.
	Envelope json.RawMessage `json:"envelope,omitempty"`
	// Profile is set instead of Envelope for a share link or a third-party
	// subscription, which are not signed.
	Profile json.RawMessage `json:"profile,omitempty"`
	// Foreign marks a third-party subscription: refreshing it reads a list of
	// servers instead of a signed profile.
	Foreign bool `json:"foreign,omitempty"`
	// Fetched is when the profile was last downloaded.
	Fetched time.Time `json:"fetched"`
}

// Client downloads profiles.
type Client struct {
	HTTP *http.Client
}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 20 * time.Second}
}

// Import reads a key and returns a fresh state with its profile.
//
// A bare https:// address may point at a TigerSB panel or at any other VPN's
// subscription. A signed envelope is pinned on first use; anything else is
// read as a list of servers, and the client picks the fastest of them.
func (c *Client) Import(ctx context.Context, keyText string) (*State, *profile.Profile, error) {
	key, err := profile.ParseKey(keyText)
	if err != nil {
		return nil, nil, err
	}
	if key.Kind == profile.KindVLESS {
		p, err := profile.FromVLESS(key.Raw)
		if err != nil {
			return nil, nil, err
		}
		if _, err := certpin.Profile(ctx, p); err != nil {
			return nil, nil, err
		}
		raw, err := json.Marshal(p)
		if err != nil {
			return nil, nil, err
		}
		return &State{Key: key.String(), Profile: raw, Fetched: time.Now().UTC()}, p, nil
	}
	if key.PK == nil {
		body, hdr, err := c.get(ctx, key.URL, UserAgent)
		if err != nil {
			return nil, nil, fmt.Errorf("не удалось получить подписку: %w", err)
		}
		if !isEnvelope(body) {
			return c.importForeign(ctx, key, body, hdr)
		}
		// A TigerSB panel: trust the key of this first envelope from now on.
		p, pk, err := profile.Open(body, nil)
		if err != nil {
			return nil, nil, err
		}
		return &State{Key: key.String(), PK: profile.EncodeKey(pk), Envelope: body, Fetched: time.Now().UTC()}, p, nil
	}
	env, p, pk, err := c.fetch(ctx, []string{key.URL}, key.PK)
	if err != nil {
		return nil, nil, err
	}
	return &State{Key: key.String(), PK: profile.EncodeKey(pk), Envelope: env, Fetched: time.Now().UTC()}, p, nil
}

func (c *Client) importForeign(ctx context.Context, key profile.Key, body []byte, hdr http.Header) (*State, *profile.Profile, error) {
	p, err := c.readForeign(ctx, key.URL, body, hdr)
	if err != nil {
		return nil, nil, err
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, nil, err
	}
	return &State{Key: key.String(), Profile: raw, Foreign: true, Fetched: time.Now().UTC()}, p, nil
}

// readForeign turns a third-party subscription response into a profile,
// asking again with a well-known client's user agent if ours got no servers.
// Servers with self-signed certificates get them pinned; those that do not
// answer are left out.
func (c *Client) readForeign(ctx context.Context, u string, body []byte, hdr http.Header) (*profile.Profile, error) {
	p, err := parseForeign(u, body, hdr)
	if err != nil {
		body2, hdr2, err2 := c.get(ctx, u, compatUserAgent)
		if err2 != nil {
			return nil, err
		}
		if p, err2 = parseForeign(u, body2, hdr2); err2 != nil {
			return nil, err
		}
	}
	if _, err := certpin.Profile(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

// Refresh downloads the profile again, trying the update URL and mirrors from
// the current profile before the URL in the key. The signing key stays pinned.
func (c *Client) Refresh(ctx context.Context, st *State) (*profile.Profile, error) {
	cur, err := st.Open()
	if err != nil {
		return nil, err
	}
	if st.Foreign {
		key, err := profile.ParseKey(st.Key)
		if err != nil {
			return nil, err
		}
		body, hdr, err := c.get(ctx, key.URL, UserAgent)
		if err != nil {
			return nil, fmt.Errorf("не удалось получить подписку: %w", err)
		}
		p, err := c.readForeign(ctx, key.URL, body, hdr)
		if err != nil {
			return nil, err
		}
		raw, err := json.Marshal(p)
		if err != nil {
			return nil, err
		}
		st.Profile, st.Fetched = raw, time.Now().UTC()
		return p, nil
	}
	if st.Envelope == nil {
		return cur, nil
	}
	key, err := profile.ParseKey(st.Key)
	if err != nil {
		return nil, err
	}
	var urls []string
	if cur.Update != nil {
		urls = append(urls, cur.Update.URL)
		urls = append(urls, cur.Update.Mirrors...)
	}
	urls = append(urls, key.URL)
	pinned, err := profile.DecodePublicKey(st.PK)
	if err != nil {
		return nil, err
	}
	env, p, _, err := c.fetch(ctx, urls, pinned)
	if err != nil {
		return nil, err
	}
	st.Envelope, st.Fetched = env, time.Now().UTC()
	return p, nil
}

func (c *Client) fetch(ctx context.Context, urls []string, pinned ed25519.PublicKey) ([]byte, *profile.Profile, ed25519.PublicKey, error) {
	var errs []error
	seen := map[string]bool{}
	for _, u := range urls {
		if seen[u] {
			continue
		}
		seen[u] = true
		body, _, err := c.get(ctx, u, UserAgent)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		p, pk, err := profile.Open(body, pinned)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", u, err))
			continue
		}
		return body, p, pk, nil
	}
	return nil, nil, nil, fmt.Errorf("не удалось получить профиль: %w", errors.Join(errs...))
}

func (c *Client) get(ctx context.Context, u, userAgent string) ([]byte, http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	for k, v := range map[string]string{
		"X-Hwid":         ThisDevice.HWID,
		"X-Device-Os":    ThisDevice.OS,
		"X-Ver-Os":       ThisDevice.OSVersion,
		"X-Device-Model": ThisDevice.Model,
	} {
		if v != "" {
			req.Header.Set(k, v)
		}
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("%s: сервер ответил %s", u, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, nil, err
	}
	if len(body) > maxBody {
		return nil, nil, fmt.Errorf("%s: ответ больше %d МБ", u, maxBody>>20)
	}
	return body, resp.Header, nil
}

// Open returns the stored profile, checking the signature again.
func (st *State) Open() (*profile.Profile, error) {
	if st.Envelope == nil {
		var p profile.Profile
		if err := json.Unmarshal(st.Profile, &p); err != nil {
			return nil, fmt.Errorf("сохранённый профиль повреждён: %w", err)
		}
		if err := p.Validate(); err != nil {
			return nil, err
		}
		return &p, nil
	}
	pinned, err := profile.DecodePublicKey(st.PK)
	if err != nil {
		return nil, err
	}
	p, _, err := profile.Open(st.Envelope, pinned)
	return p, err
}

// DefaultDir is where the client keeps its state.
func DefaultDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "TigerSB"), nil
}

const stateFile = "state.json"

// ErrNoState means no key has been imported yet.
var ErrNoState = errors.New("ключ ещё не добавлен: выполните tigersb-core import <ключ>")

// Load reads the state from dir.
func Load(dir string) (*State, error) {
	b, err := os.ReadFile(filepath.Join(dir, stateFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNoState
	}
	if err != nil {
		return nil, err
	}
	if b, err = unseal(b); err != nil {
		return nil, err
	}
	var st State
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, fmt.Errorf("файл состояния повреждён: %w", err)
	}
	return &st, nil
}

// Save writes the state to dir atomically, readable only by the user, and
// encrypted when StateKey is set.
func Save(dir string, st *State) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	if b, err = seal(b); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, stateFile+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, stateFile))
}
