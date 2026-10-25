package profile

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// Envelope is how a signed profile travels: the profile JSON as the panel
// wrote it, the Ed25519 signature over exactly those bytes and the public key.
// Signing raw bytes avoids any JSON canonicalisation.
type Envelope struct {
	V       int    `json:"v"`
	PK      string `json:"pk"`
	Payload string `json:"payload"`
	Sig     string `json:"sig"`
}

var b64 = base64.RawURLEncoding

// GenerateKey returns a new signing key pair.
func GenerateKey() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	return ed25519.GenerateKey(rand.Reader)
}

// EncodeKey returns the base64url (no padding) form used in keys and envelopes.
func EncodeKey(b []byte) string { return b64.EncodeToString(b) }

// DecodePublicKey reads a base64url Ed25519 public key.
func DecodePublicKey(s string) (ed25519.PublicKey, error) {
	b, err := b64.DecodeString(s)
	if err != nil || len(b) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("неверный публичный ключ подписи")
	}
	return ed25519.PublicKey(b), nil
}

// DecodePrivateKey reads a base64url Ed25519 seed (32 bytes) or full private key (64 bytes).
func DecodePrivateKey(s string) (ed25519.PrivateKey, error) {
	b, err := b64.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("неверный закрытый ключ подписи")
	}
	switch len(b) {
	case ed25519.SeedSize:
		return ed25519.NewKeyFromSeed(b), nil
	case ed25519.PrivateKeySize:
		return ed25519.PrivateKey(b), nil
	}
	return nil, fmt.Errorf("неверный закрытый ключ подписи")
}

// Sign validates the profile JSON and wraps it in a signed envelope.
func Sign(priv ed25519.PrivateKey, profileJSON []byte) ([]byte, error) {
	var p Profile
	if err := json.Unmarshal(profileJSON, &p); err != nil {
		return nil, fmt.Errorf("профиль не читается: %w", err)
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	env := Envelope{
		V:       Version,
		PK:      EncodeKey(priv.Public().(ed25519.PublicKey)),
		Payload: b64.EncodeToString(profileJSON),
		Sig:     b64.EncodeToString(ed25519.Sign(priv, profileJSON)),
	}
	return json.MarshalIndent(env, "", "  ")
}

// Open checks the envelope signature and returns the profile and the key that
// signed it. If pinned is set, the envelope must be signed by that key.
func Open(data []byte, pinned ed25519.PublicKey) (*Profile, ed25519.PublicKey, error) {
	var env Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, nil, fmt.Errorf("ответ подписки не похож на профиль TigerSB: %w", err)
	}
	if env.V != Version {
		return nil, nil, fmt.Errorf("версия конверта %d не поддерживается", env.V)
	}
	pk, err := DecodePublicKey(env.PK)
	if err != nil {
		return nil, nil, err
	}
	if pinned != nil && !bytes.Equal(pinned, pk) {
		return nil, nil, fmt.Errorf("профиль подписан чужим ключом: возможно, подписку подменили")
	}
	payload, err := b64.DecodeString(env.Payload)
	if err != nil {
		return nil, nil, fmt.Errorf("повреждённое содержимое профиля")
	}
	sig, err := b64.DecodeString(env.Sig)
	if err != nil || !ed25519.Verify(pk, payload, sig) {
		return nil, nil, fmt.Errorf("подпись профиля не сходится")
	}
	var p Profile
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil, nil, fmt.Errorf("профиль не читается: %w", err)
	}
	if err := p.Validate(); err != nil {
		return nil, nil, err
	}
	return &p, pk, nil
}
