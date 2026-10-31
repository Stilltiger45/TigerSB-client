package xrayconf

import (
	"encoding/json"
	"testing"
)

func TestFewStreams(t *testing.T) {
	cases := []struct{ stream, want string }{
		// The panel's default, in extra as its links carry it.
		{`{"network":"xhttp","xhttpSettings":{"extra":{"xmux":{"maxConcurrency":"16-32","hMaxRequestTimes":"600-900"}}}}`,
			`{"network":"xhttp","xhttpSettings":{"extra":{"xmux":{"hMaxRequestTimes":"600-900","maxConcurrency":"2-4"}}}}`},
		{`{"network":"xhttp","xhttpSettings":{"xmux":{"maxConcurrency":8}}}`,
			`{"network":"xhttp","xhttpSettings":{"xmux":{"maxConcurrency":"2-4"}}}`},
		// Few already, or none (one per connection): left as they are.
		{`{"network":"xhttp","xhttpSettings":{"xmux":{"maxConcurrency":"1-3"}}}`, ""},
		{`{"network":"xhttp","xhttpSettings":{"path":"/x"}}`, ""},
		{`{"network":"grpc","grpcSettings":{"serviceName":"x"}}`, ""},
	}
	for _, c := range cases {
		ob := map[string]json.RawMessage{"streamSettings": json.RawMessage(c.stream)}
		if err := withFewStreams(ob); err != nil {
			t.Fatal(err)
		}
		want := c.want
		if want == "" {
			want = c.stream
		}
		if got := string(ob["streamSettings"]); got != want {
			t.Errorf("%s\n got %s\nwant %s", c.stream, got, want)
		}
	}
}
