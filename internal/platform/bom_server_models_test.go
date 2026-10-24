package platform

import (
	"encoding/json"
	"testing"
)

func TestServerModelHardwareDifferences(t *testing.T) {
	server := BOMPackageRow{File: "server/servers.csv", Line: 1, Values: map[string]string{"model_ref": "server-v1"}}
	nic := func(line int, slot, ref string) BOMPackageRow {
		return BOMPackageRow{File: "server/server_components.csv", Line: line, Values: map[string]string{"slot": slot, "model_ref": ref}}
	}
	slots := []ModelSlot{{SlotNo: 1, PcieSlot: "PCIe-1", ExpectNicModel: "CX8"}, {SlotNo: 2, PcieSlot: "onboard", ExpectNicModel: "X710"}}
	names := map[string]string{"nic-v1": "CX8", "onboard-v1": "X710"}
	valid := []BOMPackageRow{nic(2, "1", "nic-v1"), nic(3, "onboard", "onboard-v1")}
	for _, tc := range []struct {
		name, expect string
		nics         []BOMPackageRow
		slots        []ModelSlot
		codes        []string
	}{
		{"exact", "{\"nic_count\":2}", valid, slots, nil},
		{"PCIe alias", "{\"nic_count\":2}", []BOMPackageRow{nic(2, "PCIe-1", "nic-v1"), valid[1]}, slots, nil},
		{"missing", "{\"nic_count\":2}", valid[:1], slots, []string{"model_nic_count_mismatch", "model_nic_slot_missing"}},
		{"duplicate", "{\"nic_count\":2}", []BOMPackageRow{valid[0], nic(3, "PCIe-1", "nic-v1")}, slots, []string{"model_nic_slot_duplicate", "model_nic_slot_missing"}},
		{"unknown", "{\"nic_count\":2}", []BOMPackageRow{nic(2, "99", "nic-v1"), valid[1]}, slots, []string{"model_nic_slot_invalid", "model_nic_slot_missing"}},
		{"wrong model", "{\"nic_count\":2}", []BOMPackageRow{nic(2, "1", "onboard-v1"), valid[1]}, slots, []string{"model_nic_type_mismatch"}},
		{"zero", "{\"nic_count\":0}", nil, nil, []string{"model_nic_slots_unverified"}},
		{"unknown facts", "{}", valid, nil, []string{"model_nic_count_unverified", "model_nic_slots_unverified"}},
		{"malformed", "{\"nic_count\":1.5}", valid, slots, []string{"model_nic_expectation_invalid"}},
		{"negative", "{\"nic_count\":-1}", valid, slots, []string{"model_nic_expectation_invalid"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			issues := compareServerModelHardware(server, tc.nics, json.RawMessage(tc.expect), tc.slots, names)
			if len(issues) != len(tc.codes) {
				t.Fatalf("unexpected differences %+v", issues)
			}
			for _, code := range tc.codes {
				found := false
				for _, issue := range issues {
					if issue.Code == code {
						found = true
						if issue.Severity == "BLOCK" && (issue.ModelVersionRef != "server-v1" || issue.Expected == "" || issue.Actual == "" || issue.BOMLineNo == 0) {
							t.Fatalf("missing difference details %+v", issue)
						}
					}
				}
				if !found {
					t.Fatalf("missing %s: %+v", code, issues)
				}
			}
		})
	}
}

func TestModelNICCountValidation(t *testing.T) {
	for _, value := range []any{-1, 1.5, "2", nil, true, []any{2}} {
		if err := validateModelPayload(PublishModelVersionInput{BomExpect: map[string]any{"nic_count": value}}); err == nil {
			t.Fatalf("invalid count accepted: %#v", value)
		}
	}
	for _, value := range []any{0, 2, float64(2)} {
		if err := validateModelPayload(PublishModelVersionInput{BomExpect: map[string]any{"nic_count": value}}); err != nil {
			t.Fatalf("valid count rejected: %#v %v", value, err)
		}
	}
}
