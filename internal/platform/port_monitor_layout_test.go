package platform

import (
	"encoding/json"
	"testing"
)

func TestPortMonitorPanelNeverGuessesAmbiguousOrMissingPhysicalPort(t *testing.T) {
	grid := modelPanelLayout{Rows: 1, Columns: 2, Ports: []modelPanelPort{{ID: "nic1", Label: "NIC 1", Row: 1, Column: 1, SlotNo: json.RawMessage(`1`)}, {ID: "nic2", Label: "NIC 2", Row: 1, Column: 2, SlotNo: json.RawMessage(`2`)}}}
	slots := []ModelSlot{{SlotNo: 1, PcieSlot: "PCIe-1"}, {SlotNo: 2, PcieSlot: "PCIe-2"}}
	items := []PortMonitorItem{{PhysicalPortID: "physical1", Plan: MappingPortPlan{ComponentID: "nic1"}}}
	components := map[string]string{"nic1": "PCIe-1"}
	cells := portMonitorPanelCells(grid, slots, components, items)
	if cells[0].PhysicalPortID != "physical1" || cells[1].PhysicalPortID != "" {
		t.Fatal("exact binding lost or missing binding invented", cells)
	}
	items = append(items, PortMonitorItem{PhysicalPortID: "physical2", Plan: MappingPortPlan{ComponentID: "nic1"}})
	cells = portMonitorPanelCells(grid, slots, components, items)
	if cells[0].PhysicalPortID != "" || cells[0].Reason != "physical_binding_ambiguous" {
		t.Fatal("multiple physical ports reduced to an arbitrary lamp", cells)
	}
	grid.Ports[1].SlotNo = json.RawMessage(`1`)
	cells = portMonitorPanelCells(grid, slots, components, items[:1])
	if cells[0].PhysicalPortID != "" || cells[1].PhysicalPortID != "" {
		t.Fatal("one physical port duplicated over multiple model cells", cells)
	}
}
