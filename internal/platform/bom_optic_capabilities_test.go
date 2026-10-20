package platform

import "testing"

func TestOpticCompatibilityIssues(t *testing.T) {
	row := BOMPackageRow{File: "fabric/optic_modules.csv", Line: 6, Values: map[string]string{"speed": "200G", "media_type": "DR4"}}
	port := bomSwitchOpticPortCapability{Speed: "200G", OpticMediaTypes: []string{"DR4"}}
	optic := bomOpticModelCapability{Speed: "200G", MediaTypes: []string{"DR4"}}
	if got := opticCompatibilityIssues(row, "", port, optic); len(got) != 0 {
		t.Fatalf("valid optic blocked: %+v", got)
	}
	row.Values["speed"] = "400G"
	got := opticCompatibilityIssues(row, "", port, optic)
	if len(got) != 2 || got[0].Code != "optic_model_speed_mismatch" || got[1].Code != "optic_port_speed_mismatch" {
		t.Fatalf("speed mismatch not blocked: %+v", got)
	}
	row.Values["speed"] = ""
	got = opticCompatibilityIssues(row, "", bomSwitchOpticPortCapability{}, bomOpticModelCapability{})
	if len(got) != 1 || got[0].Severity != "WARN" || got[0].Code != "optic_speed_unverified" {
		t.Fatalf("unknown speed passed: %+v", got)
	}
	row.Values["speed"] = "200G"
	row.Values["media_type"] = "FR4"
	got = opticCompatibilityIssues(row, "", port, optic)
	if len(got) != 2 || got[0].Code != "optic_model_media_mismatch" || got[1].Code != "optic_port_media_mismatch" {
		t.Fatalf("media mismatch not blocked: %+v", got)
	}
}
