package profile

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// FromVLESS turns a single vless:// link into a profile with one foreign exit.
// There is no RU exit, so in the main mode everything that is not blocked
// goes direct.
func FromVLESS(link string) (*Profile, error) {
	xray, name, err := vlessOutbound(link)
	if err != nil {
		return nil, err
	}
	p := &Profile{
		V:       Version,
		ID:      "vless",
		Name:    name,
		Updated: time.Now().UTC().Truncate(time.Second),
		Exits: []Exit{{
			ID:         "link",
			Role:       RoleForeign,
			Transports: []Transport{{ID: "vless", Prio: 10, Xray: xray}},
		}},
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return p, nil
}

func vlessOutbound(link string) (json.RawMessage, string, error) {
	u, err := url.Parse(strings.TrimSpace(link))
	if err != nil || !strings.EqualFold(u.Scheme, "vless") {
		return nil, "", fmt.Errorf("неверная ссылка vless://")
	}
	id := u.User.Username()
	if id == "" {
		return nil, "", fmt.Errorf("в ссылке vless:// нет id пользователя")
	}
	host := u.Hostname()
	port, err := strconv.Atoi(u.Port())
	if host == "" || err != nil || port < 1 || port > 65535 {
		return nil, "", fmt.Errorf("в ссылке vless:// неверный адрес сервера")
	}
	q := u.Query()

	user := map[string]any{"id": id, "encryption": or(q.Get("encryption"), "none")}
	if flow := q.Get("flow"); flow != "" {
		user["flow"] = flow
	}
	settings := map[string]any{
		"vnext": []any{map[string]any{"address": host, "port": port, "users": []any{user}}},
	}

	stream, err := vlessStream(q, host)
	if err != nil {
		return nil, "", err
	}
	out, err := json.Marshal(map[string]any{
		"protocol":       "vless",
		"settings":       settings,
		"streamSettings": stream,
	})
	if err != nil {
		return nil, "", err
	}
	return out, u.Fragment, nil
}

func vlessStream(q url.Values, host string) (map[string]any, error) {
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
				return nil, fmt.Errorf("в ссылке vless:// неверный параметр extra: %w", err)
			}
			xs["extra"] = e
		}
		stream["xhttpSettings"] = xs
	default:
		return nil, fmt.Errorf("транспорт %q из ссылки vless:// не поддерживается", network)
	}
	stream["network"] = network

	sni := or(q.Get("sni"), q.Get("peer"))
	switch security := or(q.Get("security"), "none"); security {
	case "none":
	case "tls":
		tls := dropEmpty(map[string]any{
			"serverName":  or(sni, hostHeader, hostIfName(host)),
			"fingerprint": q.Get("fp"),
		})
		if alpn := q.Get("alpn"); alpn != "" {
			tls["alpn"] = strings.Split(alpn, ",")
		}
		stream["security"] = "tls"
		stream["tlsSettings"] = tls
	case "reality":
		if q.Get("pbk") == "" {
			return nil, fmt.Errorf("в ссылке vless:// с reality нет параметра pbk")
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
		return nil, fmt.Errorf("защита %q из ссылки vless:// не поддерживается", security)
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
