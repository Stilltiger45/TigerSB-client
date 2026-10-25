package subscription

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStateEncrypted(t *testing.T) {
	t.Cleanup(func() { StateKey = nil })
	dir := t.TempDir()
	st := &State{Key: "tigersb://add?url=https://panel.example.org/sub/secret&pk=x", Fetched: time.Unix(1, 0).UTC()}

	// A state saved before encryption is read, then encrypted on save.
	if err := Save(dir, st); err != nil {
		t.Fatal(err)
	}
	StateKey = bytes.Repeat([]byte{7}, 32)
	got, err := Load(dir)
	if err != nil || got.Key != st.Key {
		t.Fatalf("plain state: %+v, %v", got, err)
	}
	if err := Save(dir, got); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, stateFile))
	if bytes.Contains(raw, []byte("secret")) || !bytes.HasPrefix(raw, sealMagic) {
		t.Fatalf("state not encrypted: %q", raw)
	}
	if got, err := Load(dir); err != nil || got.Key != st.Key {
		t.Fatalf("encrypted state: %+v, %v", got, err)
	}

	// Another key, or none, cannot read it: the user adds the key again.
	for _, k := range [][]byte{bytes.Repeat([]byte{8}, 32), nil} {
		StateKey = k
		if _, err := Load(dir); !errors.Is(err, ErrNoState) {
			t.Errorf("key %v: %v", k, err)
		}
	}
}
