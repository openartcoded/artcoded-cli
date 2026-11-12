package main

import "testing"

func TestNewerVersion(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"v2027.0.4", "v2027.0.3", true},
		{"v2027.1.0", "v2027.0.9", true},
		{"v2028.0.0", "v2027.9.9", true},
		{"v2027.0.3", "v2027.0.3", false},
		{"v2027.0.2", "v2027.0.3", false},
		{"v2027.0.10", "v2027.0.9", true},
		// dev / edge builds are never updated
		{"v2027.0.4", "dev", false},
		{"v2027.0.4", "edge-abc1234", false},
		{"v2027.0.4-rc1", "v2027.0.3", false},
	}
	for _, c := range cases {
		if got := newer(c.latest, c.current); got != c.want {
			t.Errorf("newer(%s, %s) = %v", c.latest, c.current, got)
		}
	}
}
