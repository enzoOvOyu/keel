package platform

import (
	"encoding/json"
	"testing"
)

func TestModelPanelLayout(t *testing.T) {
	slots := []ModelSlot{{SlotNo: 1}}
	for _, tc := range []struct {
		name, raw string
		valid     bool
	}{
		{"legacy metadata", `{"ports":["p1"],"source":"legacy"}`, true},
		{"missing", `{}`, true},
		{"valid", `{"layout_v1":{"rows":1,"columns":2,"ports":[{"id":"p1","label":"NIC-1","row":1,"column":1,"slot_no":1},{"id":"oob","label":"OOB","row":1,"column":2}]}}`, true},
		{"null", `{"layout_v1":null}`, false},
		{"fractional", `{"layout_v1":{"rows":1.5,"columns":1,"ports":[]}}`, false},
		{"empty ports", `{"layout_v1":{"rows":1,"columns":1,"ports":[]}}`, false},
		{"out of bounds", `{"layout_v1":{"rows":1,"columns":1,"ports":[{"id":"p1","label":"NIC","row":2,"column":1}]}}`, false},
		{"duplicate IDs", `{"layout_v1":{"rows":1,"columns":2,"ports":[{"id":"p1","label":"NIC","row":1,"column":1},{"id":"p1","label":"NIC","row":1,"column":2}]}}`, false},
		{"duplicate positions", `{"layout_v1":{"rows":1,"columns":1,"ports":[{"id":"p1","label":"NIC","row":1,"column":1},{"id":"p2","label":"NIC","row":1,"column":1}]}}`, false},
		{"missing slot", `{"layout_v1":{"rows":1,"columns":1,"ports":[{"id":"p1","label":"NIC","row":1,"column":1,"slot_no":2}]}}`, false},
		{"null slot", `{"layout_v1":{"rows":1,"columns":1,"ports":[{"id":"p1","label":"NIC","row":1,"column":1,"slot_no":null}]}}`, false},
		{"empty label", `{"layout_v1":{"rows":1,"columns":1,"ports":[{"id":"p1","label":" ","row":1,"column":1}]}}`, false},
		{"whitespace ID", `{"layout_v1":{"rows":1,"columns":1,"ports":[{"id":" p1","label":"NIC","row":1,"column":1}]}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var raw map[string]any
			if err := json.Unmarshal([]byte(tc.raw), &raw); err != nil {
				t.Fatal(err)
			}
			err := validateModelPanelLayout(raw, slots)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v: %v", tc.valid, err)
			}
		})
	}
}
