package platform

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// layout_v1 is optional: legacy metadata is preserved without inventing a layout.
type modelPanelLayout struct {
	Rows    int              `json:"rows"`
	Columns int              `json:"columns"`
	Ports   []modelPanelPort `json:"ports"`
}
type modelPanelPort struct {
	ID     string          `json:"id"`
	Label  string          `json:"label"`
	Row    int             `json:"row"`
	Column int             `json:"column"`
	SlotNo json.RawMessage `json:"slot_no,omitempty"`
}

func validateModelPanelLayout(raw map[string]any, slots []ModelSlot) error {
	value, exists := raw["layout_v1"]
	if !exists {
		return nil
	}
	encoded, err := json.Marshal(value)
	var layout modelPanelLayout
	if err != nil || string(encoded) == "null" || json.Unmarshal(encoded, &layout) != nil {
		return apiError(400, "VALIDATION_ERROR", "panel_layout.layout_v1 must be a layout object")
	}
	if layout.Rows < 1 || layout.Rows > 16 || layout.Columns < 1 || layout.Columns > 64 || len(layout.Ports) < 1 || len(layout.Ports) > 256 {
		return apiError(400, "VALIDATION_ERROR", "Panel layout requires 1-16 rows, 1-64 columns and 1-256 ports")
	}
	declared := map[int]bool{}
	for _, slot := range slots {
		declared[slot.SlotNo] = true
	}
	ids := map[string]bool{}
	cells := map[[2]int]bool{}
	for _, port := range layout.Ports {
		if port.ID == "" || port.ID != strings.TrimSpace(port.ID) || utf8.RuneCountInString(port.ID) > 64 || strings.TrimSpace(port.Label) == "" || utf8.RuneCountInString(port.Label) > 120 {
			return apiError(400, "VALIDATION_ERROR", "Panel ports require an ID of 1-64 characters without surrounding whitespace and a label of 1-120 characters")
		}
		cell := [2]int{port.Row, port.Column}
		if port.Row < 1 || port.Row > layout.Rows || port.Column < 1 || port.Column > layout.Columns || ids[port.ID] || cells[cell] {
			return apiError(400, "VALIDATION_ERROR", "Panel port IDs and cells must be unique and within the declared grid")
		}
		if len(port.SlotNo) > 0 {
			var no int
			if string(port.SlotNo) == "null" || json.Unmarshal(port.SlotNo, &no) != nil || !declared[no] {
				return apiError(400, "VALIDATION_ERROR", "Panel port slot_no must reference a slot in this version")
			}
		}
		ids[port.ID] = true
		cells[cell] = true
	}
	return nil
}
