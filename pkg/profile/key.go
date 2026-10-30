package profile

import (
	"crypto/ed25519"
	"fmt"
	"net/url"
	"strings"
)

// Scheme is the URL scheme of TigerSB keys.
const Scheme = "tigersb"

// KeyKind says what a pasted key points at.
type KeyKind int

const (
	// KindSubscription is a panel subscription that returns a signed envelope.
	KindSubscription KeyKind = iota + 1
	// KindVLESS is a single share link (vless://, vmess://, trojan://, ss://)
	// with no subscription behind it.
	KindVLESS
)

// Key is what the user pastes or scans: the one string the client needs.
type Key struct {
	Kind KeyKind
	// URL is the subscription address (KindSubscription).
	URL string
	// PK pins the signing key. Empty for a bare https:// link: the client then
	// trusts the key of the first envelope it gets and pins it.
	PK ed25519.PublicKey
	// Name is an optional display name.
	Name string
	// Raw is the share link (KindVLESS).
	Raw string
}

// ParseKey reads a tigersb://add?url=…&pk=… key, a bare https:// subscription
// address (a TigerSB panel or any other VPN's subscription) or a share link.
func ParseKey(s string) (Key, error) {
	s = strings.TrimSpace(s)
	// A WireGuard or AmneziaWG .conf is kept as the link it amounts to.
	if IsWireGuardConf(s) {
		w, err := ParseWireGuardConf(s)
		if err != nil {
			return Key{}, err
		}
		return Key{Kind: KindVLESS, Raw: w.Link()}, nil
	}
	scheme, _, ok := strings.Cut(s, "://")
	if !ok {
		return Key{}, fmt.Errorf("это не похоже на ключ: нужна ссылка tigersb://, https://, vless://, vmess://, trojan://, ss://, wireguard://, vpn:// или конфиг WireGuard/AmneziaWG")
	}
	switch strings.ToLower(scheme) {
	case Scheme:
		u, err := url.Parse(s)
		if err != nil || u.Host != "add" {
			return Key{}, fmt.Errorf("неверный ключ tigersb://")
		}
		q := u.Query()
		sub := q.Get("url")
		if !strings.HasPrefix(sub, "https://") {
			return Key{}, fmt.Errorf("в ключе нет адреса подписки https://")
		}
		pk, err := DecodePublicKey(q.Get("pk"))
		if err != nil {
			return Key{}, fmt.Errorf("в ключе %w", err)
		}
		return Key{Kind: KindSubscription, URL: sub, PK: pk, Name: q.Get("name")}, nil
	case "https":
		if _, err := url.Parse(s); err != nil {
			return Key{}, fmt.Errorf("неверный адрес подписки: %w", err)
		}
		return Key{Kind: KindSubscription, URL: s}, nil
	}
	if IsLink(s) {
		return Key{Kind: KindVLESS, Raw: s}, nil
	}
	return Key{}, fmt.Errorf("схема %s:// не поддерживается", scheme)
}

// String formats a subscription key as a tigersb:// link (or the bare URL
// when no key is pinned) and returns a vless key unchanged.
func (k Key) String() string {
	if k.Kind == KindVLESS {
		return k.Raw
	}
	if k.PK == nil {
		return k.URL
	}
	q := url.Values{}
	q.Set("url", k.URL)
	q.Set("pk", EncodeKey(k.PK))
	if k.Name != "" {
		q.Set("name", k.Name)
	}
	return Scheme + "://add?" + q.Encode()
}
