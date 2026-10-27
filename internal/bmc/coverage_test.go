package bmc

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func l3InventoryFixture(t *testing.T) *FixtureReader {
	t.Helper()
	raw, e := os.ReadFile("../../sim/fixtures/bmc/redfish-l3-sim.json")
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(t.TempDir(), "l3.json")
	if e = os.WriteFile(path, raw, 0600); e != nil {
		t.Fatal(e)
	}
	fixture, _, e := loadInventoryFixture(path)
	if e != nil {
		t.Fatal(e)
	}
	return fixture
}

// Explicit synthetic inventory is separate from the identity-only default sim.
func TestInventoryL3CoversReadOnlyHardwareGraph(t *testing.T) {
	target := testTarget()
	target.Serial = "SIM-BMC-0001"
	fixture := l3InventoryFixture(t)
	for _, level := range []string{"L1", "L2", "L3"} {
		t.Run(level, func(t *testing.T) {
			sink := &testRawSink{}
			out, e := (&Collector{Reader: fixture, Raw: sink, Config: DefaultConfig()}).Collect(context.Background(), target, level)
			if e != nil || out.State != "completed" {
				t.Fatal("synthetic inventory did not cover requested level", e, out)
			}
			if e = out.Validate(); e != nil {
				t.Fatal(e)
			}
			seen := map[string]bool{}
			for _, resource := range out.Resources {
				seen[resource.Path] = true
				if _, ok := sink.objects[resource.Raw.Key]; !ok {
					t.Fatal("inventory lacks raw bytes")
				}
			}
			switch level {
			case "L1":
				if seen["/redfish/v1/Systems/sim/Processors"] || seen["/redfish/v1/Systems/sim/EthernetInterfaces"] {
					t.Fatal("L1 crossed scope")
				}
			case "L2":
				if !seen["/redfish/v1/Chassis/sim/NetworkAdapters/sim/Ports/sim"] || seen["/redfish/v1/Systems/sim/Bios"] {
					t.Fatal("network graph incomplete or crossed into L3")
				}
			case "L3":
				for path := range fixture.documents {
					if !seen[path] {
						t.Error("advertised inventory resource omitted", path)
					}
				}
			}
		})
	}
}
func TestInventoryRejectsWrongMetadataAndMissingMembers(t *testing.T) {
	for _, kind := range []string{"wrong-type", "wrong-identity", "missing-count", "omitted-members", "missing-member-document"} {
		t.Run(kind, func(t *testing.T) {
			target := testTarget()
			target.Serial = "SIM-BMC-0001"
			fixture := l3InventoryFixture(t)
			path := "/redfish/v1/Systems/sim/Memory"
			var doc map[string]any
			if e := json.Unmarshal(fixture.documents[path], &doc); e != nil {
				t.Fatal(e)
			}
			switch kind {
			case "wrong-type":
				doc["@odata.type"] = "#Account.v1_0_0.Account"
			case "wrong-identity":
				doc["@odata.id"] = "/redfish/v1/Systems/other/Memory"
			case "missing-count":
				delete(doc, "Members@odata.count")
			case "omitted-members":
				doc["Members@odata.count"] = 2
			case "missing-member-document":
				delete(fixture.documents, "/redfish/v1/Systems/sim/Memory/sim")
			}
			fixture.documents[path], _ = json.Marshal(doc)
			out, e := (&Collector{Reader: fixture, Raw: &testRawSink{}, Config: DefaultConfig()}).Collect(context.Background(), target, "L3")
			if e != nil || out.State == "completed" {
				t.Fatal("invalid inventory envelope became complete", e)
			}
			if e = out.Validate(); e != nil {
				t.Fatal("classified partial evidence was lost", e)
			}
			if len(out.Failures) == 0 {
				t.Fatal("missing hardware evidence has no failure detail")
			}
		})
	}
}
func TestInventoryNeverNavigatesAccountActionOrOEMMembers(t *testing.T) {
	for _, forbidden := range []string{"/redfish/v1/AccountService/Accounts/1", "/redfish/v1/Systems/sim/Actions/ComputerSystem.Reset", "/redfish/v1/Managers/sim/Oem/Secret", "/redfish/v1/Managers/sim/LogServices/Log/Entries", "/redfish/v1/Systems/sim/Bios/Settings"} {
		t.Run(forbidden, func(t *testing.T) {
			if _, e := RedfishInventoryPath(forbidden); e == nil {
				t.Fatal("forbidden resource has an inventory path")
			}
			reader, _ := NewSimReader(testTarget())
			reader.documents["/redfish/v1/Systems"] = []byte(`{"@odata.id":"/redfish/v1/Systems","@odata.type":"#ComputerSystemCollection.ComputerSystemCollection","Members":[{"@odata.id":"` + forbidden + `"}],"Members@odata.count":1}`)
			reader.documents[forbidden] = []byte(`{"secret":"must-not-be-read"}`)
			sink := &testRawSink{}
			out, e := (&Collector{Reader: reader, Raw: sink, Config: DefaultConfig()}).Collect(context.Background(), testTarget(), "L1")
			if e != nil || out.State == "completed" {
				t.Fatal("forbidden navigation counted as complete")
			}
			for _, raw := range sink.objects {
				if strings.Contains(string(raw), "must-not-be-read") {
					t.Fatal("forbidden resource was read")
				}
			}
		})
	}
}
func TestCompleteInventoryRequiresRawTypedCapabilities(t *testing.T) {
	target := testTarget()
	reader, _ := NewSimReader(target)
	out, e := (&Collector{Reader: reader, Raw: &testRawSink{}, Config: DefaultConfig()}).Collect(context.Background(), target, "L1")
	if e != nil {
		t.Fatal(e)
	}
	out.Level = "L3"
	for _, feature := range levelFeatures["L3"] {
		out.Capabilities[feature] = "collected"
	}
	if out.Validate() == nil {
		t.Fatal("asserted capability map replaced missing hardware resources")
	}
}
func TestInventoryPaginationCannotHideTruncationOrCycles(t *testing.T) {
	for _, kind := range []string{"truncated-final-page", "cycle", "changed-count", "complete"} {
		t.Run(kind, func(t *testing.T) {
			target := testTarget()
			reader, _ := NewSimReader(target)
			first := map[string]any{"@odata.id": "/redfish/v1/Systems", "@odata.type": "#ComputerSystemCollection.ComputerSystemCollection", "Members": []any{map[string]string{"@odata.id": "/redfish/v1/Systems/sim"}}, "Members@odata.count": 2, "Members@odata.nextLink": "/redfish/v1/Systems?$skip=1"}
			second := map[string]any{"@odata.id": "/redfish/v1/Systems", "@odata.type": "#ComputerSystemCollection.ComputerSystemCollection", "Members": []any{map[string]string{"@odata.id": "/redfish/v1/Systems/second"}}, "Members@odata.count": 2}
			switch kind {
			case "truncated-final-page":
				second["Members"] = []any{}
			case "cycle":
				second["Members@odata.nextLink"] = "/redfish/v1/Systems"
			case "changed-count":
				second["Members@odata.count"] = 3
			}
			reader.documents["/redfish/v1/Systems"], _ = json.Marshal(first)
			reader.documents["/redfish/v1/Systems?$skip=1"], _ = json.Marshal(second)
			reader.documents["/redfish/v1/Systems/second"] = []byte(`{"@odata.id":"/redfish/v1/Systems/second","@odata.type":"#ComputerSystem.v1_0_0.ComputerSystem","SerialNumber":"SECOND-SIM"}`)
			out, e := (&Collector{Reader: reader, Raw: &testRawSink{}, Config: DefaultConfig()}).Collect(context.Background(), target, "L1")
			if e != nil {
				t.Fatal(e)
			}
			if (out.State == "completed") != (kind == "complete") {
				t.Fatal("collection completeness ignored paging evidence", kind, out.State)
			}
			if e = out.Validate(); e != nil {
				t.Fatal(e)
			}
		})
	}
}
