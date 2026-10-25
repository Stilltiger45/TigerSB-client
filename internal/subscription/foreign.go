package subscription

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Stilltiger45/TigerSB-client/pkg/profile"
)

// isEnvelope reports whether a response is a TigerSB signed envelope. Only
// then do signature errors count; anything else is another VPN's subscription.
func isEnvelope(body []byte) bool {
	var env profile.Envelope
	return json.Unmarshal(body, &env) == nil && env.Payload != "" && env.Sig != ""
}

// parseForeign reads another VPN's subscription: a list of share links, plain
// or in base64 (Marzban, 3x-ui, Remnawave and most others), or a JSON array of
// Xray configs (what Remnawave and others send to Xray-based clients). Every
// server becomes a transport of one foreign exit, so the client measures them
// and uses the fastest.
func parseForeign(subURL string, body []byte, hdr http.Header) (*profile.Profile, error) {
	name := headerText(hdr.Get("Profile-Title"))
	if name == "" {
		if u, err := url.Parse(subURL); err == nil {
			name = u.Hostname()
		}
	}

	var p *profile.Profile
	var err error
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) > 0 && (trimmed[0] == '[' || trimmed[0] == '{') {
		p, err = fromXrayJSON(name, trimmed)
	} else {
		p, _, err = profile.FromLinks(name, shareLinks(trimmed))
	}
	if err != nil {
		return nil, err
	}

	p.Update = &profile.Update{URL: subURL}
	if h, err := strconv.Atoi(strings.TrimSpace(hdr.Get("Profile-Update-Interval"))); err == nil && h > 0 {
		p.Update.Every = strconv.Itoa(h) + "h"
	}
	return p, p.Validate()
}

// shareLinks returns the links of a plain or base64 list.
func shareLinks(body []byte) []string {
	text := string(body)
	if !strings.Contains(text, "://") {
		compact := strings.Join(strings.Fields(text), "")
		for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
			if dec, err := enc.DecodeString(compact); err == nil {
				text = string(dec)
				break
			}
		}
	}
	var links []string
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); strings.Contains(line, "://") {
			links = append(links, line)
		}
	}
	return links
}

// Fields of an Xray outbound a profile may carry; see profile.Validate.
var keepOutboundKeys = []string{"protocol", "settings", "streamSettings", "mux"}

// fromXrayJSON takes the proxy outbound of every config in a JSON array (or a
// single config) as one server.
func fromXrayJSON(name string, body []byte) (*profile.Profile, error) {
	var configs []json.RawMessage
	if body[0] == '{' {
		configs = []json.RawMessage{body}
	} else if err := json.Unmarshal(body, &configs); err != nil {
		return nil, fmt.Errorf("подписка не читается: %w", err)
	}
	exit := profile.Exit{ID: "sub", Role: profile.RoleForeign}
	for _, raw := range configs {
		var cfg struct {
			Remarks   string                       `json:"remarks"`
			Outbounds []map[string]json.RawMessage `json:"outbounds"`
		}
		if json.Unmarshal(raw, &cfg) != nil {
			continue
		}
		for _, ob := range cfg.Outbounds {
			var proto string
			_ = json.Unmarshal(ob["protocol"], &proto)
			if proto != "vless" && proto != "vmess" && proto != "trojan" && proto != "shadowsocks" && proto != "hysteria" {
				continue
			}
			out := map[string]json.RawMessage{}
			for _, k := range keepOutboundKeys {
				if v, ok := ob[k]; ok {
					out[k] = v
				}
			}
			if st, ok := out["streamSettings"]; ok {
				out["streamSettings"] = dropDialerProxy(st)
			}
			xray, err := json.Marshal(out)
			if err != nil {
				continue
			}
			exit.Transports = append(exit.Transports, profile.Transport{
				ID:   "s" + strconv.Itoa(len(exit.Transports)+1),
				Prio: 10,
				Name: cfg.Remarks,
				Xray: xray,
			})
			break // the first proxy outbound is the server; the rest are helpers
		}
	}
	if len(exit.Transports) == 0 {
		return nil, fmt.Errorf("в подписке нет серверов, которые клиент умеет открыть")
	}
	return &profile.Profile{V: profile.Version, ID: "sub", Name: name, Updated: time.Now().UTC().Truncate(time.Second), Exits: []profile.Exit{exit}}, nil
}

// dropDialerProxy removes sockopt.dialerProxy: it points at helper outbounds
// (fragment and the like) of the original config that the client does not keep.
func dropDialerProxy(stream json.RawMessage) json.RawMessage {
	if len(stream) == 0 {
		return stream
	}
	var s map[string]any
	if json.Unmarshal(stream, &s) != nil {
		return stream
	}
	if so, ok := s["sockopt"].(map[string]any); ok {
		delete(so, "dialerProxy")
		if len(so) == 0 {
			delete(s, "sockopt")
		}
	}
	b, err := json.Marshal(s)
	if err != nil {
		return stream
	}
	return b
}

// headerText decodes a header value that may be "base64:…".
func headerText(v string) string {
	v = strings.TrimSpace(v)
	if rest, ok := strings.CutPrefix(v, "base64:"); ok {
		if b, err := base64.StdEncoding.DecodeString(rest); err == nil {
			return strings.TrimSpace(string(b))
		}
		return ""
	}
	return v
}
