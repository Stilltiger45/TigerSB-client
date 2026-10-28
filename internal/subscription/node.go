package subscription

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// node is one server of a Clash or sing-box subscription, written as the
// share link it would be. The links go through profile.FromLinks, so these
// formats get exactly the checks and defaults that links get.
type node struct {
	scheme     string // vless, vmess, trojan, ss, hysteria2, tuic, mierus, naive+https, naive+quic
	name       string
	host       string
	port       int // 0 for mierus, whose ports are in the query
	user, pass string
	q          url.Values
}

// link renders the node as a share link.
func (n node) link() string {
	hostPort := n.host
	if strings.Contains(hostPort, ":") {
		hostPort = "[" + hostPort + "]"
	}
	if n.port != 0 {
		hostPort = net.JoinHostPort(n.host, strconv.Itoa(n.port))
	}
	switch n.scheme {
	case "vmess":
		return vmessLink(n)
	case "hysteria2":
		// Written by hand: the parser splits at the last @ and unescapes.
		return "hysteria2://" + url.PathEscape(n.pass) + "@" + hostPort + "/?" + n.q.Encode() + "#" + url.PathEscape(n.name)
	}
	u := url.URL{Scheme: n.scheme, Host: hostPort, RawQuery: n.q.Encode(), Fragment: n.name}
	if n.pass != "" {
		u.User = url.UserPassword(n.user, n.pass)
	} else if n.user != "" {
		u.User = url.User(n.user)
	}
	return u.String()
}

// vmessLink writes the v2rayN form vmess://base64(json).
func vmessLink(n node) string {
	path := n.q.Get("path")
	if n.q.Get("type") == "grpc" {
		path = n.q.Get("serviceName")
	}
	v := map[string]string{
		"v": "2", "ps": n.name, "add": n.host, "port": strconv.Itoa(n.port), "id": n.user,
		"aid": or(n.q.Get("aid"), "0"), "scy": or(n.q.Get("scy"), "auto"),
		"net": or(n.q.Get("type"), "tcp"), "host": n.q.Get("host"), "path": path,
		"sni": n.q.Get("sni"), "alpn": n.q.Get("alpn"), "fp": n.q.Get("fp"),
	}
	if n.q.Get("security") == "tls" {
		v["tls"] = "tls"
	}
	b, _ := json.Marshal(v)
	return "vmess://" + base64.StdEncoding.EncodeToString(b)
}

// tlsQuery is the TLS of a server in share-link terms.
type tlsQuery struct {
	on       bool
	sni      string
	insecure bool
	alpn     []string
	fp       string // uTLS fingerprint
	pin      string // certificate SHA-256
	pbk, sid string // REALITY
}

// put writes the TLS into a link's query. Protocols over plain links (vless,
// vmess, trojan) say "security"; hysteria2 and naive name the pin pinSHA256.
func (t tlsQuery) put(scheme string, q url.Values) {
	set(q, "sni", t.sni)
	set(q, "alpn", strings.Join(t.alpn, ","))
	set(q, "fp", t.fp)
	if t.insecure {
		q.Set("insecure", "1")
	}
	if scheme == "hysteria2" || scheme == "naive+https" || scheme == "naive+quic" {
		set(q, "pinSHA256", t.pin)
		return
	}
	set(q, "pcs", t.pin)
	switch {
	case t.pbk != "":
		q.Set("security", "reality")
		q.Set("pbk", t.pbk)
		set(q, "sid", t.sid)
	case t.on:
		q.Set("security", "tls")
	case scheme != "tuic":
		q.Set("security", "none")
	}
}

// transportQuery is the transport of a server in share-link terms.
type transportQuery struct {
	network     string // tcp, ws, httpupgrade, grpc, xhttp; anything else is refused by the parser
	path, host  string
	serviceName string
	mode        string
}

func (t transportQuery) put(q url.Values) {
	q.Set("type", or(t.network, "tcp"))
	set(q, "path", t.path)
	set(q, "host", t.host)
	set(q, "serviceName", t.serviceName)
	set(q, "mode", t.mode)
}

func set(q url.Values, k, v string) {
	if v != "" {
		q.Set(k, v)
	}
}

func or(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// linksOf renders the nodes, or says why none of the servers can be opened.
func linksOf(nodes []node, errs []error) ([]string, error) {
	var out []string
	for _, n := range nodes {
		out = append(out, n.link())
	}
	if len(out) == 0 {
		if len(errs) > 0 {
			return nil, fmt.Errorf("в подписке нет серверов, которые клиент умеет открыть: %w", errs[0])
		}
		return nil, fmt.Errorf("в подписке нет ни одного сервера")
	}
	return out, nil
}

// obj is a JSON object of a subscription (YAML is read as JSON too), with
// lenient getters: panels write numbers as strings and the other way round.
type obj map[string]any

func decodeObj(b []byte) (obj, bool) {
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var o obj
	return o, d.Decode(&o) == nil && o != nil
}

func (o obj) str(k string) string {
	switch v := o[k].(type) {
	case string:
		return strings.TrimSpace(v)
	case json.Number:
		return v.String()
	case bool:
		return strconv.FormatBool(v)
	}
	return ""
}

func (o obj) num(k string) int {
	n, _ := strconv.Atoi(o.str(k))
	return n
}

func (o obj) flag(k string) bool {
	return o.str(k) == "true"
}

func (o obj) obj(k string) obj {
	m, _ := o[k].(map[string]any)
	return m
}

func (o obj) list(k string) []any {
	l, _ := o[k].([]any)
	return l
}

// strs reads a list of strings, or a single string with commas.
func (o obj) strs(k string) []string {
	var out []string
	if s := o.str(k); s != "" {
		for _, p := range strings.Split(s, ",") {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
		return out
	}
	for _, v := range o.list(k) {
		if s := (obj{"v": v}).str("v"); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// header reads a header from a map with any case of its name.
func (o obj) header(name string) string {
	for k := range o {
		if strings.EqualFold(k, name) {
			if s := o.str(k); s != "" {
				return s
			}
			if l := o.strs(k); len(l) > 0 {
				return l[0]
			}
		}
	}
	return ""
}
