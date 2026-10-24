package platform

import (
	"strings"
	"testing"
)

func TestModelPayloadCanonicalHash(t *testing.T) {
	empty, e := modelContentHash(PublishModelVersionInput{})
	if e != nil {
		t.Fatal(e)
	}
	explicit, e := modelContentHash(PublishModelVersionInput{HwSpec: map[string]any{}, CapabilityMatrix: map[string]any{}, Slots: []ModelSlot{}})
	if e != nil || empty != explicit {
		t.Fatal("omitted and empty payloads changed identity")
	}
	first := ModelSlot{SlotNo: 1, PcieSlot: "PCIe-1", ExpectNicModel: "fixture", PanelPort: "1", DefaultRole: "compute"}
	second := first
	second.SlotNo = 2
	a := PublishModelVersionInput{Slots: []ModelSlot{second, first}}
	b := PublishModelVersionInput{Slots: []ModelSlot{first, second}}
	ah, _ := modelContentHash(a)
	bh, _ := modelContentHash(b)
	if ah != bh || a.Slots[0].SlotNo != 2 {
		t.Fatal("slot ordering changed semantic identity or mutated caller")
	}
	b.ExpectedTopology = map[string]any{"source": "independent-fixture"}
	bh, _ = modelContentHash(b)
	if ah == bh {
		t.Fatal("topology omitted from hash")
	}
	b = a
	b.Slots = []ModelSlot{first}
	b.Slots[0].PcieSlot = strings.Repeat("x", 65)
	if e = validateModelPayload(b); e == nil {
		t.Fatal("overlong PCIe field admitted")
	}
}
