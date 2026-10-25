package subscription

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
)

// StateKey, when set to 32 bytes, encrypts the state file (it holds the
// subscription key) with AES-256-GCM. The Android app keeps this key wrapped
// by the Android Keystore, so a copy of the app's files, from a backup or a
// rooted phone, does not reveal the subscription. A state file written
// without a key is still read and is encrypted on the next save.
var StateKey []byte

// sealMagic starts an encrypted state file; a plain one starts with "{".
var sealMagic = []byte("TSB1")

func aead() (cipher.AEAD, error) {
	block, err := aes.NewCipher(StateKey)
	if err != nil {
		return nil, fmt.Errorf("ключ шифрования: %w", err)
	}
	return cipher.NewGCM(block)
}

func seal(plain []byte) ([]byte, error) {
	if StateKey == nil {
		return plain, nil
	}
	g, err := aead()
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, g.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	out := append(bytes.Clone(sealMagic), nonce...)
	return g.Seal(out, nonce, plain, sealMagic), nil
}

// errSealed means the state file is encrypted with a key this run does not
// have, such as after the Keystore was reset: the key has to be added again.
var errSealed = fmt.Errorf("%w: сохранённый ключ зашифрован и не читается, добавьте его заново", ErrNoState)

func unseal(b []byte) ([]byte, error) {
	if !bytes.HasPrefix(b, sealMagic) {
		return b, nil
	}
	if StateKey == nil {
		return nil, errSealed
	}
	g, err := aead()
	if err != nil {
		return nil, err
	}
	b = b[len(sealMagic):]
	if len(b) < g.NonceSize() {
		return nil, errSealed
	}
	plain, err := g.Open(nil, b[:g.NonceSize()], b[g.NonceSize():], sealMagic)
	if err != nil {
		return nil, errors.Join(errSealed, err)
	}
	return plain, nil
}
