package netbaseline

import (
	"strings"
	"testing"
)

func TestApprovedAddressRulesApplyIPv4AndIPv6WithoutRenumbering(t *testing.T) {
	a, b := strings.Repeat("a", 32), strings.Repeat("b", 32)
	plan := AddressPlanning{ManagementVRF: "mgmt", Pools: []AddressPool{{ID: "loop-v4", Scope: "loopback", CIDR: "198.51.100.0/24", UnitPrefix: 32, VRF: "default"}, {ID: "p2p-v6", Scope: "p2p", CIDR: "2001:db8:100::/64", UnitPrefix: 127, VRF: "default"}}, Rules: []AddressRule{{ObjectUID: a, PoolRef: "loop-v4", UnitIndex: 10}, {ObjectUID: b, PoolRef: "loop-v4", UnitIndex: 11}, {ObjectUID: a, PoolRef: "p2p-v6", UnitIndex: 2147483647, HostOffset: 0, PeerUID: b}, {ObjectUID: b, PoolRef: "p2p-v6", UnitIndex: 2147483647, HostOffset: 1, PeerUID: a}}}
	assignments, e := BuildAddressPlan(plan, nil)
	if e != nil {
		t.Fatal(e)
	}
	got := map[string]AddressAssignment{}
	for _, v := range assignments {
		got[v.PoolRef+v.ObjectUID] = v
	}
	if got["loop-v4"+a].Address != "198.51.100.10" || got["p2p-v6"+a].Address != "2001:db8:100::ffff:fffe" || got["p2p-v6"+b].PeerAddress != got["p2p-v6"+a].Address {
		t.Fatalf("incorrect indexed allocation: %+v", assignments)
	}
	// Input order and new unrelated entries do not choose different free addresses.
	plan.Rules[0], plan.Rules[1] = plan.Rules[1], plan.Rules[0]
	plan.Rules = append(plan.Rules, AddressRule{ObjectUID: strings.Repeat("c", 32), PoolRef: "loop-v4", UnitIndex: 12})
	updated, e := BuildAddressPlan(plan, nil)
	if e != nil {
		t.Fatal(e)
	}
	for _, v := range updated {
		if old, exists := got[v.PoolRef+v.ObjectUID]; exists && old.Address != v.Address {
			t.Fatal("existing allocation renumbered")
		}
	}
}
func TestAddressAdjustmentsRejectConflictsAndUnpairedLinks(t *testing.T) {
	a, b := strings.Repeat("a", 32), strings.Repeat("b", 32)
	plan := AddressPlanning{ManagementVRF: "mgmt", Pools: []AddressPool{{ID: "loop", Scope: "loopback", CIDR: "198.51.100.0/24", UnitPrefix: 32, VRF: "default", Reserved: []string{"198.51.100.0/28"}}}, Rules: []AddressRule{{ObjectUID: a, PoolRef: "loop", UnitIndex: 20}, {ObjectUID: b, PoolRef: "loop", UnitIndex: 21}}}
	for _, override := range []AddressOverride{{ObjectUID: a, PoolRef: "loop", Address: "198.51.100.21"}, {ObjectUID: a, PoolRef: "loop", Address: "198.51.100.2"}, {ObjectUID: a, PoolRef: "loop", Address: "203.0.113.2"}, {ObjectUID: strings.Repeat("c", 32), PoolRef: "loop", Address: "198.51.100.22"}} {
		if _, e := BuildAddressPlan(plan, []AddressOverride{override}); e == nil {
			t.Fatalf("invalid adjustment accepted: %+v", override)
		}
	}
	adjusted, e := BuildAddressPlan(plan, []AddressOverride{{ObjectUID: a, PoolRef: "loop", Address: "198.51.100.30"}})
	if e != nil || adjusted[0].Address != "198.51.100.30" || !adjusted[0].Adjusted {
		t.Fatal(adjusted, e)
	}
	plan = AddressPlanning{ManagementVRF: "mgmt", Pools: []AddressPool{{ID: "pair", Scope: "p2p", CIDR: "203.0.113.0/24", UnitPrefix: 31, VRF: "default"}}, Rules: []AddressRule{{ObjectUID: a, PoolRef: "pair", UnitIndex: 1, PeerUID: b}, {ObjectUID: b, PoolRef: "pair", UnitIndex: 1, HostOffset: 1, PeerUID: a}}}
	if _, e = BuildAddressPlan(plan, []AddressOverride{{ObjectUID: a, PoolRef: "pair", Address: "203.0.113.8"}}); e == nil {
		t.Fatal("half-moved p2p accepted")
	}
	if _, e = BuildAddressPlan(plan, []AddressOverride{{ObjectUID: a, PoolRef: "pair", Address: "203.0.113.8"}, {ObjectUID: b, PoolRef: "pair", Address: "203.0.113.9"}}); e != nil {
		t.Fatal(e)
	}
	plan.Rules[0].UnitIndex = 128
	if _, e = BuildAddressPlan(plan, nil); e == nil {
		t.Fatal("exhausted pool accepted")
	}
}
