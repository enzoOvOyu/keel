package platform

import "testing"

func TestSubtypeForPort(t *testing.T) {
	matrix := []byte(`{"ports":{"1":{"sub_type":"ethernet-200g"},"swp1":{"sub_type":"ethernet-800g","child_sub_type":"ethernet-200g"},"swp2s0":{"sub_type":"ethernet-400g"}}}`)
	for _, tc := range []struct{ index, want string }{{"1", "ethernet-200g"}, {"swp1s0", "ethernet-200g"}, {"swp2s0", "ethernet-400g"}, {"swp3s0", ""}} {
		if got := subtypeForPort(tc.index, matrix); got != tc.want {
			t.Fatalf("%s: %q", tc.index, got)
		}
	}
}
