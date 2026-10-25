package platform

import "testing"

func TestModelSlotIdentifiers(t *testing.T) {
	for _, tc := range []struct {
		name          string
		first, second string
		valid         bool
	}{
		{"distinct", "PCIe-1", "PCIe-2", true},
		{"own numeric alias", "1", "2", true},
		{"opaque labels", "01", "02", true},
		{"same PCIe location", "PCIe-1", "PCIe-1", false},
		{"forward number collision", "2", "PCIe-2", false},
		{"backward number collision", "PCIe-1", "1", false},
		{"padded location", " PCIe-1 ", "PCIe-2", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, reverse := range []bool{false, true} {
				slots := []ModelSlot{{SlotNo: 1, PcieSlot: tc.first, ExpectNicModel: "fixture", PanelPort: "p1", DefaultRole: "compute"}, {SlotNo: 2, PcieSlot: tc.second, ExpectNicModel: "fixture", PanelPort: "p2", DefaultRole: "unused"}}
				if reverse {
					slots[0], slots[1] = slots[1], slots[0]
				}
				err := validateModelPayload(PublishModelVersionInput{Slots: slots})
				if (err == nil) != tc.valid {
					t.Fatalf("reverse=%v valid=%v: %v", reverse, tc.valid, err)
				}
				if err != nil {
					assertErrorCode(t, err, "VALIDATION_ERROR")
				}
			}
		})
	}
}
