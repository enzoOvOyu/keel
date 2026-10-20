package platform

import "testing"

func TestEngineeringSpeedGbps(t *testing.T) {
	for _, tc := range []struct {
		raw   string
		want  float64
		valid bool
	}{
		{"200G", 200, true}, {"0.8T", 800, true}, {"100000M", 100, true},
		{"0G", 0, false}, {"NaNG", 0, false}, {"InfG", 0, false}, {"200", 0, false}, {"-100G", 0, false},
	} {
		got, ok := engineeringSpeedGbps(tc.raw)
		if got != tc.want || ok != tc.valid {
			t.Fatalf("%q: got %v %v", tc.raw, got, ok)
		}
	}
}
