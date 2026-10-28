package subscription

import (
	"fmt"
	"net/url"
	"strings"
)

// singBoxLinks reads a sing-box JSON config: the servers among its outbounds,
// as share links. Selectors, urltest, direct, block and dns outbounds are
// helpers, not servers. ok is false when the body is not a sing-box config
// (Xray configs say "protocol" where sing-box says "type").
func singBoxLinks(body []byte) (links []string, ok bool, err error) {
	cfg, isObj := decodeObj(body)
	if !isObj {
		return nil, false, nil
	}
	var nodes []node
	var errs []error
	for _, v := range cfg.list("outbounds") {
		ob, _ := v.(map[string]any)
		typ := obj(ob).str("type")
		if typ == "" {
			continue
		}
		ok = true
		switch typ {
		case "selector", "urltest", "direct", "block", "dns":
			continue
		}
		n, err := singBoxNode(ob)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		nodes = append(nodes, n)
	}
	if !ok {
		return nil, false, nil
	}
	links, err = linksOf(nodes, errs)
	return links, true, err
}

func singBoxNode(o obj) (node, error) {
	typ := o.str("type")
	n := node{name: o.str("tag"), host: o.str("server"), port: o.num("server_port"), q: url.Values{}}
	t := o.obj("tls")
	tls := tlsQuery{
		on:       t.flag("enabled"),
		sni:      t.str("server_name"),
		insecure: t.flag("insecure"),
		alpn:     t.strs("alpn"),
	}
	if u := t.obj("utls"); u.flag("enabled") {
		tls.fp = u.str("fingerprint")
	}
	if r := t.obj("reality"); r.flag("enabled") {
		tls.pbk, tls.sid = r.str("public_key"), r.str("short_id")
	}
	switch typ {
	case "vless", "vmess", "trojan":
		n.scheme = typ
		tr, err := singBoxTransport(o.obj("transport"))
		if err != nil {
			return node{}, err
		}
		tr.put(n.q)
		switch typ {
		case "vless":
			n.user = o.str("uuid")
			set(n.q, "flow", o.str("flow"))
		case "vmess":
			n.user = o.str("uuid")
			set(n.q, "aid", o.str("alter_id"))
			set(n.q, "scy", o.str("security"))
		case "trojan":
			n.user = o.str("password") // trojan://password@host
		}
		tls.put(typ, n.q)
	case "shadowsocks":
		if o.str("plugin") != "" {
			return node{}, fmt.Errorf("серверы shadowsocks с плагином %s не поддерживаются", o.str("plugin"))
		}
		n.scheme, n.user, n.pass = "ss", o.str("method"), o.str("password")
	case "hysteria2":
		n.scheme, n.pass = "hysteria2", o.str("password")
		// sing-box writes port ranges as 2080:3000, links as 2080-3000.
		set(n.q, "mport", strings.ReplaceAll(strings.Join(o.strs("server_ports"), ","), ":", "-"))
		if obfs := o.obj("obfs"); obfs != nil {
			set(n.q, "obfs", obfs.str("type"))
			set(n.q, "obfs-password", obfs.str("password"))
		}
		tls.put(n.scheme, n.q)
	case "tuic":
		n.scheme, n.user, n.pass = "tuic", o.str("uuid"), o.str("password")
		set(n.q, "congestion_control", o.str("congestion_control"))
		set(n.q, "udp_relay_mode", o.str("udp_relay_mode"))
		if o.flag("zero_rtt_handshake") {
			n.q.Set("reduce_rtt", "1")
		}
		tls.put(n.scheme, n.q)
	case "naive":
		if o.flag("quic") {
			return node{}, fmt.Errorf("NaïveProxy поверх QUIC не поддерживается")
		}
		n.scheme, n.user, n.pass = "naive+https", o.str("username"), o.str("password")
		var headers []string
		for k := range o.obj("extra_headers") {
			headers = append(headers, k+": "+o.obj("extra_headers").header(k))
		}
		set(n.q, "extra-headers", strings.Join(headers, "\r\n"))
		tls.put(n.scheme, n.q)
	default:
		return node{}, fmt.Errorf("серверы %s не поддерживаются", typ)
	}
	return n, nil
}

func singBoxTransport(t obj) (transportQuery, error) {
	tr := transportQuery{network: or(t.str("type"), "tcp"), path: t.str("path")}
	switch tr.network {
	case "tcp":
	case "ws":
		tr.host = t.obj("headers").header("Host")
	case "httpupgrade":
		tr.host = t.str("host")
	case "grpc":
		tr.serviceName = t.str("service_name")
	default:
		return tr, fmt.Errorf("транспорт %s не поддерживается", tr.network)
	}
	return tr, nil
}
