package platform

import (
	"encoding/json"
	"testing"
)

func TestTopologyFirmwareSimulationRequiresExactApprovedCompatibility(t *testing.T) {
	deps := map[string]any{"compatibility": map[string]any{"source": "vendor-reviewed-matrix", "version": "1", "report_ref": "review://firmware-compatibility", "requires": map[string][]string{"device_model_version": {"model-version"}, "repair_action": {"flash_bios"}}}}
	raw, e := json.Marshal(deps)
	if e != nil {
		t.Fatal(e)
	}
	media := MediaItemSummary{Type: "firmware", State: "approved", Arch: "amd64", ArtifactStored: true, Deps: raw}
	if !firmwareCompatibility(&media, "model-version", "amd64", "flash_bios") {
		t.Fatal("exact independent approved matrix rejected")
	}
	for _, tc := range []struct {
		name                string
		change              func(*MediaItemSummary)
		model, arch, action string
	}{
		{"other model", func(*MediaItemSummary) {}, "other", "amd64", "flash_bios"},
		{"wrong architecture", func(*MediaItemSummary) {}, "model-version", "arm64", "flash_bios"},
		{"wrong action", func(*MediaItemSummary) {}, "model-version", "amd64", "update_nvswitch"},
		{"unapproved", func(m *MediaItemSummary) { m.State = "verified" }, "model-version", "amd64", "flash_bios"},
		{"unknown matrix", func(m *MediaItemSummary) { m.Deps = json.RawMessage(`{}`) }, "model-version", "amd64", "flash_bios"},
		{"quarantined", func(m *MediaItemSummary) { m.ArtifactQuarantined = true }, "model-version", "amd64", "flash_bios"},
		{"no original bytes", func(m *MediaItemSummary) { m.ArtifactStored = false }, "model-version", "amd64", "flash_bios"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := media
			tc.change(&m)
			if firmwareCompatibility(&m, tc.model, tc.arch, tc.action) {
				t.Fatal("missing independent compatibility or integrity was accepted")
			}
		})
	}
}
