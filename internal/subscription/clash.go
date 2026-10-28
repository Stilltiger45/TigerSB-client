package subscription

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/ghodss/yaml"
)

// clashLinks reads a Clash (Mihomo, Clash Meta) YAML config: the servers of
// its proxies list, as share links. ok is false when the body is not one.
func clashLinks(body []byte) (links []string, ok bool, err error) {
	j, err := yaml.YAMLToJSON(body)
	if err != nil {
		return nil, false, nil
	}
	cfg, isObj := decodeObj(j)
	if !isObj {
		return nil, false, nil
	}
	if _, has := cfg["proxies"]; !has {
		return nil, false, nil
	}
	var nodes []node
	var errs []error
	for _, v := range cfg.list("proxies") {
		p, _ := v.(map[string]any)
		n, err := clashNode(p)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		nodes = append(nodes, n)
	}
	links, err = linksOf(nodes, errs)
	return links, true, err
}

func clashNode(p obj) (node, error) {
	typ := strings.ToLower(p.str("type"))
	n := node{name: p.str("name"), host: p.str("server"), port: p.num("port"), q: url.Values{}}
	tls := tlsQuery{
		on:       p.flag("tls"),
		sni:      or(p.str("servername"), p.str("sni")),
		insecure: p.flag("skip-cert-verify"),
		alpn:     p.strs("alpn"),
		fp:       p.str("client-fingerprint"),
		pin:      p.str("fingerprint"),
	}
	if r := p.obj("reality-opts"); r != nil {
		tls.pbk, tls.sid = r.str("public-key"), r.str("short-id")
	}
	switch typ {
	case "vless", "vmess", "trojan":
		n.scheme = typ
		tr, err := clashTransport(p)
		if err != nil {
			return node{}, err
		}
		tr.put(n.q)
		switch typ {
		case "vless":
			n.user = p.str("uuid")
			set(n.q, "flow", p.str("flow"))
			set(n.q, "encryption", p.str("encryption"))
		case "vmess":
			n.user = p.str("uuid")
			set(n.q, "aid", p.str("alterId"))
			set(n.q, "scy", p.str("cipher"))
		case "trojan":
			n.user = p.str("password") // trojan://password@host
			tls.on = true              // trojan is always over TLS
		}
		tls.put(typ, n.q)
	case "ss":
		if p.str("plugin") != "" {
			return node{}, fmt.Errorf("серверы ss с плагином %s не поддерживаются", p.str("plugin"))
		}
		n.scheme, n.user, n.pass = "ss", p.str("cipher"), p.str("password")
	case "hysteria2", "hy2":
		n.scheme, n.pass = "hysteria2", or(p.str("password"), p.str("auth"))
		set(n.q, "mport", p.str("ports"))
		set(n.q, "obfs", p.str("obfs"))
		set(n.q, "obfs-password", p.str("obfs-password"))
		tls.put(n.scheme, n.q)
	case "tuic":
		n.scheme, n.user, n.pass = "tuic", p.str("uuid"), p.str("password")
		set(n.q, "congestion_control", p.str("congestion-controller"))
		set(n.q, "udp_relay_mode", p.str("udp-relay-mode"))
		if p.flag("reduce-rtt") {
			n.q.Set("reduce_rtt", "1")
		}
		tls.put(n.scheme, n.q)
	case "mieru":
		n.scheme, n.user, n.pass = "mierus", p.str("username"), p.str("password")
		n.port = 0 // mieru's ports go in the query
		n.q.Set("port", or(p.str("port-range"), p.str("port")))
		n.q.Set("protocol", strings.ToUpper(or(p.str("transport"), "TCP")))
		set(n.q, "multiplexing", p.str("multiplexing"))
		set(n.q, "handshake-mode", p.str("handshake-mode"))
		set(n.q, "traffic-pattern", p.str("traffic-pattern"))
	default:
		return node{}, fmt.Errorf("серверы %s не поддерживаются", or(typ, "без типа"))
	}
	return n, nil
}

// clashTransport reads network and its *-opts.
func clashTransport(p obj) (transportQuery, error) {
	t := transportQuery{network: strings.ToLower(or(p.str("network"), "tcp"))}
	switch t.network {
	case "tcp":
	case "ws":
		o := p.obj("ws-opts")
		t.path, t.host = o.str("path"), o.obj("headers").header("Host")
		if o.flag("v2ray-http-upgrade") {
			t.network = "httpupgrade"
		}
	case "grpc":
		t.serviceName = p.obj("grpc-opts").str("grpc-service-name")
	case "xhttp":
		o := p.obj("xhttp-opts")
		t.path, t.host, t.mode = o.str("path"), o.str("host"), o.str("mode")
	default:
		return t, fmt.Errorf("транспорт %s не поддерживается", t.network)
	}
	return t, nil
}
