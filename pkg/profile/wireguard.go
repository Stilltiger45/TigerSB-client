package profile

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

// WireGuard and AmneziaWG come as a .conf file (what wg-quick, the AmneziaWG
// apps and the panels hand out), as a wireguard:// link (v2rayN's form, with
// AmneziaWG's keys in the query), or inside an Amnezia vpn:// key. All of
// them become one transport of protocol "wireguard", in Xray's form plus
// "dns" and the AmneziaWG keys under "amnezia"; package awg carries it.

// awgKeys are AmneziaWG's keys in the spelling of its .conf: junk packets
// (1.0), paddings and headers (1.0; S3, S4 and ranges of H in 2.0),
// signature packets (2.0), and header protection, content padding, timings,
// random trailers and cookies (3.x).
var awgKeys = []string{
	"Jc", "Jmin", "Jmax", "S1", "S2", "S3", "S4", "H1", "H2", "H3", "H4",
	"I1", "I2", "I3", "I4", "I5",
	"HeaderProtectionKey", "ContentPaddingAddition",
	"RekeyAfterTime", "RekeyTimeout", "RejectAfterTime", "KeepaliveTimeout", "MaxHandshakeAttempts",
	"RandomTrailers", "DisableCookies",
}

// awgKey returns the .conf spelling of an AmneziaWG key written in any case,
// with or without _ and - (jc, header_protection_key, headerprotectionkey).
func awgKey(k string) (string, bool) {
	k = strings.NewReplacer("_", "", "-", "").Replace(k)
	for _, name := range awgKeys {
		if strings.EqualFold(k, name) {
			return name, true
		}
	}
	return "", false
}

// WireGuard is one WireGuard or AmneziaWG server as a client config has it.
type WireGuard struct {
	Name         string
	PrivateKey   string   // base64
	Address      []string // in the tunnel, each with or without a prefix
	DNS          []string
	MTU          int
	PublicKey    string // the server's
	PresharedKey string
	Endpoint     string // host:port
	AllowedIPs   []string
	Keepalive    string // seconds, or a range in AmneziaWG 3
	// AWG holds the AmneziaWG keys in the .conf spelling; none for plain
	// WireGuard.
	AWG map[string]string
}

// ParseWireGuardConf reads a WireGuard or AmneziaWG .conf. Only the first
// [Peer] is used: client configs have one.
func ParseWireGuardConf(text string) (WireGuard, error) {
	w := WireGuard{AWG: map[string]string{}}
	section, peers := "", 0
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}
		if line[0] == '[' {
			section = strings.ToLower(strings.Trim(line, "[] \t"))
			if section == "peer" {
				peers++
			}
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok || section == "peer" && peers > 1 {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if name, ok := awgKey(k); ok {
			if v != "" {
				w.AWG[name] = v
			}
			continue
		}
		switch section + "." + strings.ToLower(k) {
		case "interface.privatekey":
			w.PrivateKey = v
		case "interface.address":
			w.Address = append(w.Address, list(v)...)
		case "interface.dns":
			w.DNS = append(w.DNS, list(v)...)
		case "interface.mtu":
			w.MTU, _ = strconv.Atoi(v)
		case "peer.publickey":
			w.PublicKey = v
		case "peer.presharedkey":
			w.PresharedKey = v
		case "peer.endpoint":
			w.Endpoint = v
		case "peer.allowedips":
			w.AllowedIPs = append(w.AllowedIPs, list(v)...)
		case "peer.persistentkeepalive":
			w.Keepalive = v
		}
	}
	if peers == 0 {
		return WireGuard{}, fmt.Errorf("в конфиге WireGuard нет раздела [Peer]")
	}
	if h, _, err := net.SplitHostPort(w.Endpoint); err == nil {
		w.Name = h
	}
	return w, w.check()
}

// IsWireGuardConf reports whether text looks like a WireGuard .conf.
func IsWireGuardConf(text string) bool {
	return strings.Contains(strings.ToLower(text), "[interface]")
}

// list splits a comma-separated value.
func list(v string) []string {
	var out []string
	for _, s := range strings.Split(v, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// check makes sure the server can be dialled and the keys are keys.
func (w *WireGuard) check() error {
	if !isWGKey(w.PrivateKey) {
		return fmt.Errorf("в конфиге WireGuard нет закрытого ключа PrivateKey или он неверный")
	}
	if !isWGKey(w.PublicKey) {
		return fmt.Errorf("в конфиге WireGuard нет открытого ключа сервера PublicKey или он неверный")
	}
	if w.PresharedKey != "" && !isWGKey(w.PresharedKey) {
		return fmt.Errorf("в конфиге WireGuard неверный PresharedKey")
	}
	if k := w.AWG["HeaderProtectionKey"]; k != "" && !isWGKey(k) {
		return fmt.Errorf("в конфиге AmneziaWG неверный HeaderProtectionKey")
	}
	host, port, err := net.SplitHostPort(w.Endpoint)
	if n, _ := strconv.Atoi(port); err != nil || host == "" || n < 1 || n > 65535 {
		return fmt.Errorf("в конфиге WireGuard неверный адрес сервера Endpoint")
	}
	if len(w.Address) == 0 {
		return fmt.Errorf("в конфиге WireGuard нет адреса в туннеле Address")
	}
	for _, a := range w.Address {
		if _, err := netip.ParsePrefix(a); err != nil {
			if _, err := netip.ParseAddr(a); err != nil {
				return fmt.Errorf("в конфиге WireGuard неверный адрес %q", a)
			}
		}
	}
	return nil
}

func isWGKey(s string) bool {
	b, err := base64.StdEncoding.DecodeString(s)
	return err == nil && len(b) == 32
}

// Link writes the server as a wireguard:// link, the form a profile keeps.
func (w WireGuard) Link() string {
	q := url.Values{}
	q.Set("publickey", w.PublicKey)
	set := func(k, v string) {
		if v != "" {
			q.Set(k, v)
		}
	}
	set("presharedkey", w.PresharedKey)
	set("address", strings.Join(w.Address, ","))
	set("dns", strings.Join(w.DNS, ","))
	if w.MTU > 0 {
		q.Set("mtu", strconv.Itoa(w.MTU))
	}
	set("allowedips", strings.Join(w.AllowedIPs, ","))
	set("keepalive", w.Keepalive)
	for _, k := range awgKeys {
		set(strings.ToLower(k), w.AWG[k])
	}
	u := url.URL{Scheme: "wireguard", User: url.User(w.PrivateKey), Host: w.Endpoint, Path: "/", RawQuery: q.Encode(), Fragment: w.Name}
	return u.String()
}

// wireguardOutbound reads a wireguard://, wg:// or awg:// link: the private
// key as the user, the server as the host, the rest in the query.
func wireguardOutbound(link string) (json.RawMessage, string, error) {
	u, err := url.Parse(link)
	if err != nil {
		return nil, "", fmt.Errorf("неверная ссылка wireguard://")
	}
	w := WireGuard{Name: u.Fragment, PrivateKey: u.User.Username(), Endpoint: u.Host, AWG: map[string]string{}}
	for k, vs := range u.Query() {
		v := strings.TrimSpace(vs[0])
		if name, ok := awgKey(k); ok {
			if v != "" {
				w.AWG[name] = v
			}
			continue
		}
		switch strings.ToLower(strings.ReplaceAll(k, "_", "")) {
		case "publickey", "peerpublickey", "pbk":
			w.PublicKey = v
		case "presharedkey", "psk":
			w.PresharedKey = v
		case "address", "ip", "localaddress":
			w.Address = list(v)
		case "dns":
			w.DNS = list(v)
		case "mtu":
			w.MTU, _ = strconv.Atoi(v)
		case "allowedips":
			w.AllowedIPs = list(v)
		case "keepalive", "persistentkeepalive":
			w.Keepalive = v
		}
	}
	// A key written into a link unescaped loses its + to a space.
	w.PrivateKey = strings.ReplaceAll(w.PrivateKey, " ", "+")
	w.PublicKey = strings.ReplaceAll(w.PublicKey, " ", "+")
	w.PresharedKey = strings.ReplaceAll(w.PresharedKey, " ", "+")
	if k := w.AWG["HeaderProtectionKey"]; k != "" {
		w.AWG["HeaderProtectionKey"] = strings.ReplaceAll(k, " ", "+")
	}
	if err := w.check(); err != nil {
		return nil, "", err
	}
	out, err := w.outbound()
	return out, w.Name, err
}

// outbound is the transport's Xray-style outbound. Package awg reads it.
func (w WireGuard) outbound() (json.RawMessage, error) {
	allowed := w.AllowedIPs
	if len(allowed) == 0 {
		allowed = []string{"0.0.0.0/0", "::/0"}
	}
	peer := map[string]any{"publicKey": w.PublicKey, "endpoint": w.Endpoint, "allowedIPs": allowed}
	if w.PresharedKey != "" {
		peer["preSharedKey"] = w.PresharedKey
	}
	// Phones sit behind NATs that forget a quiet UDP flow in under a
	// minute: keep it alive unless the config says otherwise.
	switch n, err := strconv.Atoi(w.Keepalive); {
	case w.Keepalive == "":
		peer["keepAlive"] = 25
	case err == nil:
		peer["keepAlive"] = n
	default:
		peer["keepAlive"] = w.Keepalive
	}
	settings := map[string]any{"secretKey": w.PrivateKey, "address": w.Address, "peers": []any{peer}}
	if w.MTU > 0 {
		settings["mtu"] = w.MTU
	}
	if len(w.DNS) > 0 {
		settings["dns"] = w.DNS
	}
	if len(w.AWG) > 0 {
		settings["amnezia"] = w.AWG
	}
	return outbound("wireguard", settings, nil)
}

// amneziaOutbound reads an Amnezia vpn:// key: base64 of Qt's qCompress (a
// big-endian length, then zlib) of a JSON config whose containers hold the
// WireGuard or AmneziaWG client config as last_config.
func amneziaOutbound(link string) (json.RawMessage, string, error) {
	w, err := AmneziaKey(link)
	if err != nil {
		return nil, "", err
	}
	out, err := w.outbound()
	return out, w.Name, err
}

// AmneziaKey reads the WireGuard or AmneziaWG server of an Amnezia vpn:// key.
func AmneziaKey(link string) (WireGuard, error) {
	_, body, _ := strings.Cut(strings.TrimSpace(link), "://")
	raw, err := decodeB64(body)
	if err != nil {
		return WireGuard{}, fmt.Errorf("неверный ключ vpn://")
	}
	if len(raw) > 4 && raw[0] != '{' {
		size := binary.BigEndian.Uint32(raw)
		zr, err := zlib.NewReader(bytes.NewReader(raw[4:]))
		if err != nil {
			return WireGuard{}, fmt.Errorf("неверный ключ vpn://")
		}
		raw, err = io.ReadAll(io.LimitReader(zr, int64(min(size, 4<<20))))
		if err != nil {
			return WireGuard{}, fmt.Errorf("неверный ключ vpn://")
		}
	}
	var key struct {
		Description string `json:"description"`
		DNS1        string `json:"dns1"`
		DNS2        string `json:"dns2"`
		Default     string `json:"defaultContainer"`
		Containers  []map[string]json.RawMessage
	}
	if err := json.Unmarshal(raw, &key); err != nil {
		return WireGuard{}, fmt.Errorf("неверный ключ vpn://")
	}
	// The default container first, then the others.
	slices.SortStableFunc(key.Containers, func(a, b map[string]json.RawMessage) int {
		isDefault := func(c map[string]json.RawMessage) bool {
			var name string
			json.Unmarshal(c["container"], &name)
			return name == key.Default
		}
		switch {
		case isDefault(a) && !isDefault(b):
			return -1
		case isDefault(b) && !isDefault(a):
			return 1
		}
		return 0
	})
	dns := strings.NewReplacer("$PRIMARY_DNS", or(key.DNS1, "1.1.1.1"), "$SECONDARY_DNS", or(key.DNS2, "1.0.0.1"))
	firstErr := fmt.Errorf("в ключе vpn:// нет сервера WireGuard или AmneziaWG: ключи Amnezia Premium открывает только приложение Amnezia")
	for _, c := range key.Containers {
		for _, v := range c {
			var proto struct {
				LastConfig string `json:"last_config"`
			}
			if json.Unmarshal(v, &proto) != nil || proto.LastConfig == "" {
				continue
			}
			var last map[string]any
			if json.Unmarshal([]byte(proto.LastConfig), &last) != nil {
				continue
			}
			text, _ := last["config"].(string)
			w, err := ParseWireGuardConf(dns.Replace(text))
			if err != nil {
				firstErr = err
				continue
			}
			// Amnezia keeps the AmneziaWG keys beside the config too.
			for k, v := range last {
				if name, ok := awgKey(k); ok && w.AWG[name] == "" {
					switch x := v.(type) {
					case string:
						if x != "" {
							w.AWG[name] = x
						}
					case float64:
						w.AWG[name] = strconv.FormatFloat(x, 'f', -1, 64)
					}
				}
			}
			if key.Description != "" {
				w.Name = key.Description
			}
			return w, nil
		}
	}
	return WireGuard{}, firstErr
}
