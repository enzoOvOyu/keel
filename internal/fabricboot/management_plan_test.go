package fabricboot

import (
	"reflect"
	"strings"
	"testing"
)

func TestManagementPlanStableAllocation(t *testing.T) {
	r := ManagementRules{CIDR: "192.0.2.0/29", FirstAddress: "192.0.2.1", Gateway: "192.0.2.1", BatchPrefix: "approved", BatchBy: "rack"}
	d := []ManagementDevice{{ID: "b", Serial: "SN-B", DC: "DC1", Pod: "P1", Role: "leaf", Rack: "R1", Index: "2"}, {ID: "c", Serial: "SN-C", DC: "dc1", Pod: "p1", Role: "leaf", Rack: "r1", Index: "3"}}
	first, e := BuildManagementPlan(r, d, nil, 1)
	if e != nil {
		t.Fatal(e)
	}
	if first[0].Address != "192.0.2.2" || first[1].Address != "192.0.2.3" || first[0].Batch == first[1].Batch {
		t.Fatalf("wrong allocation: %+v", first)
	}
	added := append([]ManagementDevice{{ID: "a", Serial: "SN-A", DC: "dc1", Pod: "p1", Role: "leaf", Rack: "r1", Index: "1"}}, d...)
	next, e := BuildManagementPlan(r, added, first, 1)
	if e != nil {
		t.Fatal(e)
	}
	if next[0].Address != "192.0.2.4" || !reflect.DeepEqual(next[1:], first) {
		t.Fatalf("existing devices renumbered: %+v", next)
	}
	again, e := BuildManagementPlan(r, added, next, 1)
	if e != nil || !reflect.DeepEqual(next, again) {
		t.Fatal("allocation is not idempotent", e)
	}
}
func TestManagementPlanRejectsUnsafeRulesAndIdentities(t *testing.T) {
	rules := ManagementRules{CIDR: "192.0.2.0/30", FirstAddress: "192.0.2.1", Gateway: "192.0.2.1", BatchPrefix: "approved", BatchBy: "rack"}
	device := ManagementDevice{ID: "a", Serial: "SN-A", DC: "dc1", Pod: "p1", Role: "leaf", Rack: "r1", Index: "1"}
	tests := []struct {
		name   string
		change func(*ManagementRules, *ManagementDevice)
		count  int
	}{
		{"exhaustion", func(*ManagementRules, *ManagementDevice) {}, 2},
		{"underscore", func(_ *ManagementRules, d *ManagementDevice) { d.Rack = "rack_1" }, 1},
		{"long hostname", func(_ *ManagementRules, d *ManagementDevice) { d.DC = strings.Repeat("a", 64) }, 1},
		{"broadcast first", func(r *ManagementRules, _ *ManagementDevice) { r.FirstAddress = "192.0.2.3" }, 1},
		{"gateway outside", func(r *ManagementRules, _ *ManagementDevice) { r.Gateway = "198.51.100.1" }, 1},
		{"noncanonical", func(r *ManagementRules, _ *ManagementDevice) { r.CIDR = "192.0.2.1/30" }, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, d := rules, device
			tc.change(&r, &d)
			devices := []ManagementDevice{d}
			if tc.count == 2 {
				other := d
				other.ID = "b"
				other.Serial = "SN-B"
				other.Index = "2"
				devices = append(devices, other)
			}
			if _, e := BuildManagementPlan(r, devices, nil, 500); e == nil {
				t.Fatal("unsafe plan accepted")
			}
		})
	}
}
