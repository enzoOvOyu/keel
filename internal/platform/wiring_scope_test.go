package platform

import (
	"reflect"
	"strings"
	"testing"
)

func TestWiringSelectionCanonicalCopiesAndLimits(t *testing.T) {
	id := func(s string) string { return strings.Repeat(s, 32) }
	in := []WiringTargetInput{{AssetID: id("b"), PhysicalPortIDs: []string{id("d"), id("c")}}, {AssetID: id("a"), PhysicalPortIDs: []string{id("e")}}}
	out, e := normalizeWiringTargets(in)
	if e != nil {
		t.Fatal(e)
	}
	if out[0].AssetID != id("a") || out[1].PhysicalPortIDs[0] != id("c") {
		t.Fatal("selection is not canonical")
	}
	if in[0].PhysicalPortIDs[0] != id("d") {
		t.Fatal("canonicalization mutated caller selection")
	}
	again, e := normalizeWiringTargets(out)
	if e != nil || !reflect.DeepEqual(out, again) {
		t.Fatal("selection canonicalization is unstable")
	}
	for _, bad := range [][]WiringTargetInput{nil, {{AssetID: id("a")}}, {{AssetID: id("a"), PhysicalPortIDs: []string{id("c"), id("c")}}}, {{AssetID: "untrusted", PhysicalPortIDs: []string{id("c")}}}, {in[0], in[0]}} {
		if _, e = normalizeWiringTargets(bad); e == nil {
			t.Fatalf("bad selection admitted: %+v", bad)
		}
	}
	many := make([]WiringTargetInput, 201)
	if _, e = normalizeWiringTargets(many); e == nil {
		t.Fatal("201 targets admitted")
	}
	if wiringHandshakeIdentity(id("a"), id("b"), "key") == wiringHandshakeIdentity(id("c"), id("b"), "key") {
		t.Fatal("idempotency key crosses project scope")
	}
}
