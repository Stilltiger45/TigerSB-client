package profile

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Share links other clients and panels hand out. Each becomes one transport.
var linkSchemes = map[string]bool{
	"vless": true, "vmess": true, "trojan": true, "ss": true,
	"hysteria2": true, "hy2": true,
}

// IsLink reports whether s looks like a share link the client can read.
func IsLink(s string) bool {
	scheme, _, ok := strings.Cut(strings.TrimSpace(s), "://")
	return ok && linkSchemes[strings.ToLower(scheme)]
}

// FromVLESS turns a single share link (vless://, vmess://, trojan://, ss://
// or hysteria2://) into a profile with one foreign exit. Without an RU exit and lists,
// the main mode sends everything but the direct list through it.
func FromVLESS(link string) (*Profile, error) {
	xray, name, err := ParseLink(link)
	if err != nil {
		return nil, err
	}
	scheme, _, _ := strings.Cut(strings.TrimSpace(link), "://")
	p := &Profile{
		V:       Version,
		ID:      strings.ToLower(scheme),
		Name:    name,
		Updated: time.Now().UTC().Truncate(time.Second),
		Exits: []Exit{{
			ID:         "link",
			Role:       RoleForeign,
			Transports: []Transport{{ID: strings.ToLower(scheme), Prio: 10, Name: name, Xray: xray}},
		}},
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return p, nil
}

// FromLinks builds a profile from the servers of a third-party subscription:
// one foreign exit whose transports are the servers, all with the same
// priority, so the client measures them and uses the fastest. Links it cannot
// read are skipped; skipped says how many.
func FromLinks(name string, links []string) (p *Profile, skipped int, err error) {
	p = &Profile{
		V:       Version,
		ID:      "sub",
		Name:    name,
		Updated: time.Now().UTC().Truncate(time.Second),
	}
	exit := Exit{ID: "sub", Role: RoleForeign}
	var firstErr error
	for _, l := range links {
		xray, remark, err := ParseLink(l)
		if err != nil {
			skipped++
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		exit.Transports = append(exit.Transports, Transport{
			ID:   "s" + strconv.Itoa(len(exit.Transports)+1),
			Prio: 10,
			Name: remark,
			Xray: xray,
		})
	}
	if len(exit.Transports) == 0 {
		if firstErr != nil {
			return nil, skipped, fmt.Errorf("в подписке нет серверов, которые клиент умеет открыть: %w", firstErr)
		}
		return nil, skipped, fmt.Errorf("в подписке нет ни одного сервера")
	}
	p.Exits = []Exit{exit}
	if err := p.Validate(); err != nil {
		return nil, skipped, err
	}
	return p, skipped, nil
}

// ParseLink turns a share link into an Xray outbound (without a tag) and the
// link's display name.
func ParseLink(link string) (json.RawMessage, string, error) {
	link = strings.TrimSpace(link)
	scheme, _, _ := strings.Cut(link, "://")
	switch strings.ToLower(scheme) {
	case "vless":
		return vlessOutbound(link)
	case "trojan":
		return trojanOutbound(link)
	case "ss":
		return ssOutbound(link)
	case "vmess":
		return vmessOutbound(link)
	case "hysteria2", "hy2":
		return hysteria2Outbound(link)
	}
	return nil, "", fmt.Errorf("ссылки %s:// не поддерживаются", scheme)
}

// hostPort reads the server address of a link.
func hostPort(u *url.URL, scheme string) (string, int, error) {
	host := u.Hostname()
	port, err := strconv.Atoi(u.Port())
	if host == "" || err != nil || port < 1 || port > 65535 {
		return "", 0, fmt.Errorf("в ссылке %s:// неверный адрес сервера", scheme)
	}
	return host, port, nil
}

func outbound(protocol string, settings, stream map[string]any) (json.RawMessage, error) {
	ob := map[string]any{"protocol": protocol, "settings": settings}
	if stream != nil {
		ob["streamSettings"] = stream
	}
	return json.Marshal(ob)
}

func vlessOutbound(link string) (json.RawMessage, string, error) {
	u, err := url.Parse(link)
	if err != nil {
		return nil, "", fmt.Errorf("неверная ссылка vless://")
	}
	id := u.User.Username()
	if id == "" {
		return nil, "", fmt.Errorf("в ссылке vless:// нет id пользователя")
	}
	host, port, err := hostPort(u, "vless")
	if err != nil {
		return nil, "", err
	}
	q := u.Query()
	user := map[string]any{"id": id, "encryption": or(q.Get("encryption"), "none")}
	if flow := q.Get("flow"); flow != "" {
		user["flow"] = flow
	}
	stream, err := streamFromQuery(q, host, "none", "vless")
	if err != nil {
		return nil, "", err
	}
	out, err := outbound("vless", map[string]any{
		"vnext": []any{map[string]any{"address": host, "port": port, "users": []any{user}}},
	}, stream)
	return out, u.Fragment, err
}

func trojanOutbound(link string) (json.RawMessage, string, error) {
	u, err := url.Parse(link)
	if err != nil {
		return nil, "", fmt.Errorf("неверная ссылка trojan://")
	}
	password := u.User.Username()
	if password == "" {
		return nil, "", fmt.Errorf("в ссылке trojan:// нет пароля")
	}
	host, port, err := hostPort(u, "trojan")
	if err != nil {
		return nil, "", err
	}
	stream, err := streamFromQuery(u.Query(), host, "tls", "trojan")
	if err != nil {
		return nil, "", err
	}
	out, err := outbound("trojan", map[string]any{
		"servers": []any{map[string]any{"address": host, "port": port, "password": password}},
	}, stream)
	return out, u.Fragment, err
}

// ssOutbound reads SIP002 links (ss://base64(method:password)@host:port or
// ss://method:password@host:port) and the older ss://base64(method:password@host:port).
func ssOutbound(link string) (json.RawMessage, string, error) {
	body := strings.TrimPrefix(link, link[:len("ss://")])
	body, name, _ := strings.Cut(body, "#")
	if n, err := url.PathUnescape(name); err == nil {
		name = n
	}
	if !strings.Contains(body, "@") {
		// Old form: everything before # is base64.
		dec, err := decodeB64(strings.SplitN(body, "?", 2)[0])
		if err != nil {
			return nil, "", fmt.Errorf("неверная ссылка ss://")
		}
		body = string(dec)
	}
	u, err := url.Parse("ss://" + body)
	if err != nil {
		return nil, "", fmt.Errorf("неверная ссылка ss://")
	}
	if u.Query().Get("plugin") != "" {
		return nil, "", fmt.Errorf("ссылки ss:// с плагинами не поддерживаются")
	}
	method, password := u.User.Username(), ""
	if p, ok := u.User.Password(); ok {
		password = p
	} else if dec, err := decodeB64(method); err == nil {
		method, password, _ = strings.Cut(string(dec), ":")
	}
	if method == "" || password == "" {
		return nil, "", fmt.Errorf("в ссылке ss:// нет метода или пароля")
	}
	host, port, err := hostPort(u, "ss")
	if err != nil {
		return nil, "", err
	}
	out, err := outbound("shadowsocks", map[string]any{
		"servers": []any{map[string]any{"address": host, "port": port, "method": method, "password": password}},
	}, nil)
	return out, name, err
}

// vmessOutbound reads the v2rayN form vmess://base64(json).
func vmessOutbound(link string) (json.RawMessage, string, error) {
	dec, err := decodeB64(strings.TrimSpace(link[len("vmess://"):]))
	if err != nil {
		return nil, "", fmt.Errorf("неверная ссылка vmess://")
	}
	var v map[string]any
	if err := json.Unmarshal(dec, &v); err != nil {
		return nil, "", fmt.Errorf("неверная ссылка vmess://")
	}
	str := func(k string) string {
		switch x := v[k].(type) {
		case string:
			return x
		case float64:
			return strconv.FormatFloat(x, 'f', -1, 64)
		}
		return ""
	}
	host := str("add")
	port, err := strconv.Atoi(str("port"))
	if host == "" || err != nil || port < 1 || port > 65535 || str("id") == "" {
		return nil, "", fmt.Errorf("в ссылке vmess:// неверный адрес сервера или id")
	}
	aid, _ := strconv.Atoi(str("aid"))
	q := url.Values{}
	q.Set("type", or(str("net"), "tcp"))
	q.Set("host", str("host"))
	q.Set("path", str("path"))
	q.Set("sni", str("sni"))
	q.Set("alpn", str("alpn"))
	q.Set("fp", str("fp"))
	if str("net") == "grpc" {
		q.Set("serviceName", str("path"))
		if str("type") == "multi" {
			q.Set("mode", "multi")
		}
	}
	if str("tls") == "tls" {
		q.Set("security", "tls")
	}
	stream, err := streamFromQuery(q, host, "none", "vmess")
	if err != nil {
		return nil, "", err
	}
	out, err := outbound("vmess", map[string]any{
		"vnext": []any{map[string]any{"address": host, "port": port, "users": []any{
			map[string]any{"id": str("id"), "alterId": aid, "security": or(str("scy"), "auto")},
		}}},
	}, stream)
	return out, str("ps"), err
}

// decodeB64 accepts standard and URL-safe base64, with or without padding.
func decodeB64(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(s); err == nil {
			return b, nil
		}
	}
	return nil, fmt.Errorf("не base64")
}

// streamFromQuery builds streamSettings from the query of a share link.
func streamFromQuery(q url.Values, host, defaultSecurity, scheme string) (map[string]any, error) {
	network := or(q.Get("type"), "tcp")
	stream := map[string]any{}
	path := q.Get("path")
	hostHeader := q.Get("host")
	switch network {
	case "tcp", "raw":
		network = "raw"
	case "ws":
		stream["wsSettings"] = dropEmpty(map[string]any{"path": path, "host": hostHeader})
	case "httpupgrade":
		stream["httpupgradeSettings"] = dropEmpty(map[string]any{"path": path, "host": hostHeader})
	case "grpc":
		stream["grpcSettings"] = dropEmpty(map[string]any{
			"serviceName": q.Get("serviceName"),
			"authority":   q.Get("authority"),
			"multiMode":   q.Get("mode") == "multi",
		})
	case "xhttp", "splithttp":
		network = "xhttp"
		xs := dropEmpty(map[string]any{"path": path, "host": hostHeader, "mode": q.Get("mode")})
		if extra := q.Get("extra"); extra != "" {
			var e json.RawMessage
			if err := json.Unmarshal([]byte(extra), &e); err != nil {
				return nil, fmt.Errorf("в ссылке %s:// неверный параметр extra: %w", scheme, err)
			}
			xs["extra"] = e
		}
		stream["xhttpSettings"] = xs
	default:
		return nil, fmt.Errorf("транспорт %q из ссылки %s:// не поддерживается", network, scheme)
	}
	stream["network"] = network

	sni := or(q.Get("sni"), q.Get("peer"))
	switch security := or(q.Get("security"), defaultSecurity); security {
	case "none", "":
	case "tls":
		pin := pinHex(q.Get("pcs"))
		tls := dropEmpty(map[string]any{
			"serverName":           or(sni, hostHeader, hostIfName(host)),
			"fingerprint":          q.Get("fp"),
			"pinnedPeerCertSha256": pin,
			"verifyPeerCertByName": q.Get("vcn"),
			// Xray no longer accepts allowInsecure; the importer replaces it
			// with the pin of the certificate the server has now (see
			// Insecure).
			"allowInsecure": pin == "" && (q.Get("allowInsecure") == "1" || q.Get("insecure") == "1"),
		})
		if alpn := q.Get("alpn"); alpn != "" {
			tls["alpn"] = strings.Split(alpn, ",")
		}
		stream["security"] = "tls"
		stream["tlsSettings"] = tls
	case "reality":
		if q.Get("pbk") == "" {
			return nil, fmt.Errorf("в ссылке %s:// с reality нет параметра pbk", scheme)
		}
		stream["security"] = "reality"
		stream["realitySettings"] = dropEmpty(map[string]any{
			"serverName":    sni,
			"fingerprint":   or(q.Get("fp"), "chrome"),
			"publicKey":     q.Get("pbk"),
			"shortId":       q.Get("sid"),
			"spiderX":       q.Get("spx"),
			"mldsa65Verify": q.Get("pqv"),
		})
	default:
		return nil, fmt.Errorf("защита %q из ссылки %s:// не поддерживается", security, scheme)
	}
	return stream, nil
}

func or(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func hostIfName(host string) string {
	if net.ParseIP(host) != nil {
		return ""
	}
	return host
}

func dropEmpty(m map[string]any) map[string]any {
	for k, v := range m {
		if v == "" || v == false {
			delete(m, k)
		}
	}
	return m
}

// hysteria2Outbound reads hysteria2:// (or hy2://) links:
// hysteria2://auth@host:port/?sni=&insecure=1&pinSHA256=&obfs=salamander&obfs-password=&mport=#name.
// The port may be a list of ports and ranges ("443,20000-30000"), which, like
// mport, turns on port hopping.
func hysteria2Outbound(link string) (json.RawMessage, string, error) {
	_, rest, _ := strings.Cut(strings.TrimSpace(link), "://")
	rest, name, _ := strings.Cut(rest, "#")
	if n, err := url.PathUnescape(name); err == nil {
		name = n
	}
	rest, rawQuery, _ := strings.Cut(rest, "?")
	authority, _, _ := strings.Cut(rest, "/")
	q, err := url.ParseQuery(rawQuery)
	if err != nil {
		return nil, "", fmt.Errorf("неверная ссылка hysteria2://")
	}
	var auth string
	if i := strings.LastIndex(authority, "@"); i >= 0 {
		auth, authority = authority[:i], authority[i+1:]
		if a, err := url.PathUnescape(auth); err == nil {
			auth = a
		}
	}
	if auth == "" {
		return nil, "", fmt.Errorf("в ссылке hysteria2:// нет пароля")
	}

	// The port part may hold a list of ports, which net.SplitHostPort and
	// url.Parse refuse, so split at the last colon outside brackets.
	host, ports := authority, "443"
	if i := strings.LastIndex(authority, ":"); i >= 0 && i > strings.LastIndex(authority, "]") {
		host, ports = authority[:i], authority[i+1:]
	}
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	first, _, _ := strings.Cut(ports, ",")
	first, _, _ = strings.Cut(first, "-")
	port, err := strconv.Atoi(first)
	if host == "" || err != nil || port < 1 || port > 65535 {
		return nil, "", fmt.Errorf("в ссылке hysteria2:// неверный адрес сервера")
	}
	hop := q.Get("mport")
	if hop == "" && ports != first {
		hop = ports
	}

	pin := pinHex(q.Get("pinSHA256"))
	tls := dropEmpty(map[string]any{
		"serverName":           or(q.Get("sni"), q.Get("peer"), hostIfName(host)),
		"pinnedPeerCertSha256": pin,
		"allowInsecure":        pin == "" && q.Get("insecure") == "1",
	})
	tls["alpn"] = []string{"h3"}
	stream := map[string]any{
		"network":          "hysteria",
		"security":         "tls",
		"tlsSettings":      tls,
		"hysteriaSettings": map[string]any{"version": 2, "auth": auth},
	}
	mask := map[string]any{}
	switch obfs := q.Get("obfs"); obfs {
	case "", "none":
	case "salamander":
		password := q.Get("obfs-password")
		if password == "" {
			return nil, "", fmt.Errorf("в ссылке hysteria2:// нет obfs-password")
		}
		mask["udp"] = []any{map[string]any{"type": "salamander", "settings": map[string]any{"password": password}}}
	default:
		return nil, "", fmt.Errorf("обфускация %q из ссылки hysteria2:// не поддерживается", obfs)
	}
	if hop != "" {
		mask["quicParams"] = map[string]any{"udpHop": map[string]any{"ports": hop}}
	}
	if len(mask) > 0 {
		stream["finalmask"] = mask
	}
	out, err := outbound("hysteria", map[string]any{"version": 2, "address": host, "port": port}, stream)
	return out, name, err
}

// pinHex normalizes a certificate SHA-256 from a link: hex, with or without
// colons, as Xray's pinnedPeerCertSha256 takes it. Anything else is dropped.
func pinHex(s string) string {
	s = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(s), ":", ""))
	if len(s) != 64 {
		return ""
	}
	for _, c := range s {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return ""
		}
	}
	return s
}
