package xrayconf

import (
	"encoding/json"
	"strconv"
	"strings"
)

// maxStreams caps how many requests XHTTP puts on one connection to the
// server. All of them share one TCP connection: a big download on it holds
// up every short request beside it, and the panel's default lets 16 to 32
// share one. A few per connection keep pages quick while something
// downloads, at the cost of a few more connections.
const maxStreams = "2-4"

// withFewStreams caps xmux.maxConcurrency of an XHTTP transport, in its
// xhttpSettings and in their extra, at maxStreams. Without xmux Xray takes
// one request per connection, which stays.
func withFewStreams(ob map[string]json.RawMessage) error {
	raw, ok := ob["streamSettings"]
	if !ok {
		return nil
	}
	var stream map[string]any
	if err := json.Unmarshal(raw, &stream); err != nil {
		return err
	}
	xs, _ := stream["xhttpSettings"].(map[string]any)
	if stream["network"] != "xhttp" || xs == nil {
		return nil
	}
	changed := capStreams(xs)
	if extra, ok := xs["extra"].(map[string]any); ok && capStreams(extra) {
		changed = true
	}
	if !changed {
		return nil
	}
	raw, err := json.Marshal(stream)
	ob["streamSettings"] = raw
	return err
}

// capStreams lowers xmux.maxConcurrency in s when it allows more than
// maxStreams; it tells whether it did.
func capStreams(s map[string]any) bool {
	xmux, _ := s["xmux"].(map[string]any)
	if xmux == nil {
		return false
	}
	var top int
	switch v := xmux["maxConcurrency"].(type) {
	case float64:
		top = int(v)
	case string:
		_, hi, found := strings.Cut(v, "-")
		if !found {
			hi = v
		}
		top, _ = strconv.Atoi(strings.TrimSpace(hi))
	}
	_, hi, _ := strings.Cut(maxStreams, "-")
	if limit, _ := strconv.Atoi(hi); top <= limit {
		return false
	}
	xmux["maxConcurrency"] = maxStreams
	return true
}
