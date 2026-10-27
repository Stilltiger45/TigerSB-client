package mobile

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/Stilltiger45/TigerSB-client/internal/subscription"
)

// The app keeps several profiles, each a subscription in its own folder under
// profiles/, and uses one of them, the active one, for the VPN. The order and
// the active profile are in profiles.json. A phone that kept its one profile
// in state.json before moves it there on first use.

const (
	profilesDir  = "profiles"
	profilesFile = "profiles.json"
)

type profileIndex struct {
	Active string   `json:"active"`
	Order  []string `json:"order"`
}

func profileDir(id string) string { return filepath.Join(dataDir, profilesDir, id) }

// readIndex reads the list of profiles, moving a single profile saved by an
// older version into it. Call with mu held.
func readIndex() (profileIndex, error) {
	var ix profileIndex
	b, err := os.ReadFile(filepath.Join(dataDir, profilesFile))
	if err == nil {
		if err := json.Unmarshal(b, &ix); err != nil {
			return ix, errors.New("список профилей повреждён")
		}
		return ix, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return ix, err
	}
	old := filepath.Join(dataDir, "state.json")
	if _, err := os.Stat(old); err != nil {
		return ix, nil // no profiles yet
	}
	id := newID()
	if err := os.MkdirAll(profileDir(id), 0o700); err != nil {
		return ix, err
	}
	if err := os.Rename(old, filepath.Join(profileDir(id), "state.json")); err != nil {
		return ix, err
	}
	ix = profileIndex{Active: id, Order: []string{id}}
	return ix, writeIndex(ix)
}

func writeIndex(ix profileIndex) error {
	if len(ix.Order) == 0 {
		err := os.Remove(filepath.Join(dataDir, profilesFile))
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	b, err := json.Marshal(ix)
	if err != nil {
		return err
	}
	tmp := filepath.Join(dataDir, profilesFile+".tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dataDir, profilesFile))
}

func newID() string {
	b := make([]byte, 6)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// loadActive reads the active profile's subscription. Call with mu held.
func loadActive() (*subscription.State, string, error) {
	ix, err := readIndex()
	if err != nil {
		return nil, "", err
	}
	if ix.Active == "" {
		return nil, "", subscription.ErrNoState
	}
	st, err := subscription.Load(profileDir(ix.Active))
	return st, ix.Active, err
}

// AddKey adds a key (tigersb://, https:// or a share link) as a new profile,
// or updates the profile with the same key, downloads the profile and the
// lists it needs, makes it the active one and returns its info as JSON.
func AddKey(key string) (string, error) {
	netMu.Lock()
	defer netMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	// The download runs without mu: the app's screen asks the core for the
	// profile meanwhile and must not wait for a slow network.
	st, p, err := (&subscription.Client{}).Import(ctx, key)
	if err != nil {
		return "", err
	}
	mu.Lock()
	defer mu.Unlock()
	ix, err := readIndex()
	if err != nil {
		return "", err
	}
	id := ""
	for _, other := range ix.Order {
		if old, err := subscription.Load(profileDir(other)); err == nil && old.Key == st.Key {
			id = other
			break
		}
	}
	if id == "" {
		id = newID()
		ix.Order = append(ix.Order, id)
	}
	if err := subscription.Save(profileDir(id), st); err != nil {
		return "", err
	}
	ix.Active = id
	if err := writeIndex(ix); err != nil {
		return "", err
	}
	i := inst
	inBackground(func() { fetchLists(i, p, geodataMaxAge, nil) })
	return info(id, st, p)
}

// Refresh downloads the active profile and the lists again. On failure the
// old profile stays usable.
func Refresh() (string, error) {
	mu.Lock()
	ix, err := readIndex()
	mu.Unlock()
	if err != nil {
		return "", err
	}
	if ix.Active == "" {
		return "", noKey(subscription.ErrNoState)
	}
	return RefreshProfile(ix.Active)
}

// RefreshProfile downloads the profile id and the lists again and returns
// its info as JSON. On failure the old profile stays usable. A running VPN
// takes the new profile when it reconnects.
func RefreshProfile(id string) (string, error) {
	netMu.Lock()
	defer netMu.Unlock()
	mu.Lock()
	st, err := subscription.Load(profileDir(id))
	mu.Unlock()
	if err != nil {
		return "", noKey(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	p, err := (&subscription.Client{}).Refresh(ctx, st)
	if err != nil {
		return "", err
	}
	mu.Lock()
	defer mu.Unlock()
	// Deleted meanwhile: do not bring it back.
	if _, err := os.Stat(profileDir(id)); err != nil {
		return "", noKey(subscription.ErrNoState)
	}
	if err := subscription.Save(profileDir(id), st); err != nil {
		return "", err
	}
	i := inst
	inBackground(func() { fetchLists(i, p, geodataMaxAge, nil) })
	return info(id, st, p)
}

// Current returns the active profile's info as JSON, or "" when no key has
// been added.
func Current() (string, error) {
	mu.Lock()
	defer mu.Unlock()
	st, id, err := loadActive()
	if errors.Is(err, subscription.ErrNoState) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	p, err := st.Open()
	if err != nil {
		return "", err
	}
	return info(id, st, p)
}

// Profiles returns the info of every profile, in the order they were added,
// as a JSON array; the active one has "active": true.
func Profiles() (string, error) {
	mu.Lock()
	defer mu.Unlock()
	ix, err := readIndex()
	if err != nil {
		return "", err
	}
	list := []json.RawMessage{}
	for _, id := range ix.Order {
		st, err := subscription.Load(profileDir(id))
		if err != nil {
			continue
		}
		p, err := st.Open()
		if err != nil {
			continue
		}
		s, err := info(id, st, p)
		if err != nil {
			continue
		}
		list = append(list, json.RawMessage(s))
	}
	b, err := json.Marshal(list)
	return string(b), err
}

// Select makes the profile id the active one and returns its info as JSON.
// A running VPN takes it when it reconnects.
func Select(id string) (string, error) {
	mu.Lock()
	defer mu.Unlock()
	ix, err := readIndex()
	if err != nil {
		return "", err
	}
	if !slices.Contains(ix.Order, id) {
		return "", errors.New("такого профиля нет")
	}
	st, err := subscription.Load(profileDir(id))
	if err != nil {
		return "", err
	}
	p, err := st.Open()
	if err != nil {
		return "", err
	}
	ix.Active = id
	if err := writeIndex(ix); err != nil {
		return "", err
	}
	return info(id, st, p)
}

// Delete removes the profile id. When it was the active one, the next
// profile becomes active; the VPN must be off then. It returns the active
// profile's info as JSON, or "" when no profile is left, and then the lists
// go too.
func Delete(id string) (string, error) {
	mu.Lock()
	ix, err := readIndex()
	if err == nil && id == ix.Active && inst != nil {
		err = errors.New("сначала отключите VPN")
	}
	if err == nil && !slices.Contains(ix.Order, id) {
		err = errors.New("такого профиля нет")
	}
	if err != nil {
		mu.Unlock()
		return "", err
	}
	if err := os.RemoveAll(profileDir(id)); err != nil {
		mu.Unlock()
		return "", err
	}
	ix.Order = slices.DeleteFunc(ix.Order, func(o string) bool { return o == id })
	if ix.Active == id {
		ix.Active = ""
		if len(ix.Order) > 0 {
			ix.Active = ix.Order[0]
		}
	}
	err = writeIndex(ix)
	if err == nil && len(ix.Order) == 0 {
		err = os.RemoveAll(assetDir())
	}
	mu.Unlock()
	if err != nil {
		return "", err
	}
	return Current()
}

// SetStateKey sets the 32-byte key that encrypts the saved subscriptions (see
// subscription.StateKey) and encrypts the ones saved without it. Call it
// right after Init.
func SetStateKey(key []byte) error {
	mu.Lock()
	defer mu.Unlock()
	subscription.StateKey = slices.Clone(key)
	ix, err := readIndex()
	if err != nil {
		return err
	}
	for _, id := range ix.Order {
		st, err := subscription.Load(profileDir(id))
		if err != nil {
			return err
		}
		if err := subscription.Save(profileDir(id), st); err != nil {
			return err
		}
	}
	return nil
}
