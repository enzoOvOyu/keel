package platform

import "testing"

func TestSwitchPortCapabilityChecks(t *testing.T) {
	matrix := []byte(`{"ports":{"swp1":{"speeds":["200G","400G"],"breakout_modes":["4x200G"]},"2":{"speed":"100G"}}}`)
	for _, tc := range []struct{ port, mode, speed, wantMode, wantSpeed string }{
		{"swp1s0", "4x200G", "200G", "", ""},
		{"swp1s0", "8x100G", "200G", "breakout_mode_not_supported", "breakout_speed_mismatch"},
		{"swp1s0", "4x200G", "800G", "", "breakout_speed_mismatch"},
		{"swp3", "", "", "switch_port_not_in_model", ""},
		{"swp1s4", "4x200G", "200G", "breakout_child_out_of_range", ""},
	} {
		if got := switchPortCapabilityIssue(tc.port, tc.mode, matrix); got != tc.wantMode {
			t.Fatalf("%s mode: %s", tc.port, got)
		}
		if got := switchPortSpeedIssue(tc.port, tc.speed, tc.mode, matrix); got != tc.wantSpeed {
			t.Fatalf("%s speed: %s", tc.port, got)
		}
	}
}
