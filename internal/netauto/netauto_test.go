package netauto

import "testing"

func TestMTU(t *testing.T) {
	for _, c := range []struct {
		t    Transport
		link int
		want int
	}{
		{Wifi, 0, 1500},
		{Cellular, 0, 1400},
		{Cellular, 1358, 1358},
		{Wifi, 9000, 1500},
		{Ethernet, 576, 1500},
		{"", 0, 1500},
	} {
		if got := MTU(c.t, c.link); got != c.want {
			t.Errorf("MTU(%q, %d) = %d, want %d", c.t, c.link, got, c.want)
		}
	}
}
