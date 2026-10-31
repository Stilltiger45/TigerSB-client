package xrayconf

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestInboundLogin(t *testing.T) {
	in := inbounds([]Inbound{
		{Tag: "socks-in", Protocol: "socks", Port: 1, User: "tigersb", Pass: "secret"},
		{Tag: "http-in", Protocol: "http", Port: 2, User: "tigersb", Pass: "secret"},
		{Tag: "open", Protocol: "socks", Port: 3},
	})
	b, _ := json.Marshal(in)
	want := `[{"port":1,"protocol":"socks","settings":{"accounts":[{"pass":"secret","user":"tigersb"}],"auth":"password","udp":true}`
	if got := string(b); len(got) < len(want) || got[:len(want)] != want {
		t.Errorf("socks: %s", got)
	}
	s := string(b)
	for _, part := range []string{`"protocol":"http","settings":{"accounts":[{"pass":"secret","user":"tigersb"}]}`, `"port":3,"protocol":"socks","settings":{"auth":"noauth","udp":true}`} {
		if !strings.Contains(s, part) {
			t.Errorf("missing %s in %s", part, s)
		}
	}
}
