package platform

import "testing"

func TestBOMResolvedComponentMACs(t *testing.T) {
	for _, tc := range []struct{ name, parent, mac, code string }{
		{"stable UID wins over raw name", "nic-owner", "AA:BB:CC:DD:EE:01", ""},
		{"wrong UID does not borrow matching name", "another-nic", "aa:bb:cc:dd:ee:01", "component_port_mac_mismatch"},
		{"missing per port MAC", "nic-owner", "", "per_port_mac_missing"},
		{"summary differs", "nic-owner", "aa:bb:cc:dd:ee:02", "component_port_mac_mismatch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			nic := &bomMaterial{category: "nic", uid: "nic-owner", row: BOMPackageRow{File: "server/server_components.csv", Line: 2, Values: map[string]string{"serial": "20", "mac": "aa:bb:cc:dd:ee:01", "model_ref": "nic-v1"}}}
			port := &bomMaterial{category: "port", parentUID: tc.parent, row: BOMPackageRow{File: "server/server_ports.csv", Line: 3, Values: map[string]string{"parent_ref": "20", "mac": tc.mac}}}
			issues := validateResolvedComponentPortMACs([]*bomMaterial{nic, port})
			if tc.code == "" {
				if len(issues) != 0 {
					t.Fatal(issues)
				}
				return
			}
			found := false
			for _, issue := range issues {
				if issue.Code == tc.code && issue.Severity == "BLOCK" && issue.ModelVersionRef == "nic-v1" && issue.BOMLineNo > 0 {
					found = true
				}
			}
			if !found {
				t.Fatalf("missing located MAC difference: %+v", issues)
			}
		})
	}
}
