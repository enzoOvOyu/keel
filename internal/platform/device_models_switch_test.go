package platform

import "testing"

func TestSwitchCapabilityMatrixChangesVersionHash(t *testing.T) {
	base := PublishModelVersionInput{HwSpec: map[string]any{"vendor": "NVIDIA"}, CapabilityMatrix: map[string]any{"ports": map[string]any{"swp1": "800G"}}}
	if e := validateModelPayload(base); e != nil {
		t.Fatal(e)
	}
	first, e := modelContentHash(base)
	if e != nil {
		t.Fatal(e)
	}
	base.CapabilityMatrix = map[string]any{"ports": map[string]any{"swp1": "400G"}}
	second, e := modelContentHash(base)
	if e != nil {
		t.Fatal(e)
	}
	if first == second {
		t.Fatal("switch capability changes must create a new immutable version hash")
	}
}
