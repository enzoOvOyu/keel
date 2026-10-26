package platform

import (
	"reflect"
	"strings"
	"testing"
)

func TestProvisionKeyRepairBatchCanonicalSelection(t *testing.T) {
	first := ProvisionKeyRepairBatchItem{InstallationID: strings.Repeat("a", 32), ExpectedVersion: 2, RepairEventID: 12, ConfirmSerial: "fixture-a"}
	second := ProvisionKeyRepairBatchItem{InstallationID: strings.Repeat("b", 32), ExpectedVersion: 3, RepairEventID: 13, ConfirmSerial: "fixture-b"}
	in := ProvisionKeyRepairBatchInput{Action: "create", Items: []ProvisionKeyRepairBatchItem{second, first}, Reason: "explicit restore selection"}
	canonical, err := canonicalProvisionKeyRepairBatch(in)
	if err != nil || canonical.Items[0] != first || in.Items[0] != second {
		t.Fatal("canonical selection mutated caller", err)
	}
	other := in
	other.Items = []ProvisionKeyRepairBatchItem{first, second}
	sorted, err := canonicalProvisionKeyRepairBatch(other)
	if err != nil || !reflect.DeepEqual(canonical, sorted) {
		t.Fatal("order changed canonical batch")
	}
	for _, action := range []string{"approve", "revoke", "query"} {
		item := ProvisionKeyRepairBatchItem{InstallationID: first.InstallationID, ExpectedVersion: 1, RepairID: strings.Repeat("c", 32)}
		if _, err := canonicalProvisionKeyRepairBatch(ProvisionKeyRepairBatchInput{Action: action, Items: []ProvisionKeyRepairBatchItem{item}, Reason: "explicit action"}); err != nil {
			t.Fatal(action, err)
		}
		item.RepairEventID = 12
		if _, err := canonicalProvisionKeyRepairBatch(ProvisionKeyRepairBatchInput{Action: action, Items: []ProvisionKeyRepairBatchItem{item}, Reason: "ambiguous action"}); err == nil {
			t.Fatal("mixed create/action fields accepted")
		}
	}
	for _, items := range [][]ProvisionKeyRepairBatchItem{nil, {first, first}, make([]ProvisionKeyRepairBatchItem, 33)} {
		if _, err := canonicalProvisionKeyRepairBatch(ProvisionKeyRepairBatchInput{Action: "create", Items: items, Reason: "bad selection"}); err == nil {
			t.Fatal("invalid batch accepted")
		}
	}
	first.RepairID = strings.Repeat("c", 32)
	if _, err := canonicalProvisionKeyRepairBatch(ProvisionKeyRepairBatchInput{Action: "create", Items: []ProvisionKeyRepairBatchItem{first}, Reason: "ambiguous create"}); err == nil {
		t.Fatal("creation reused repair ID")
	}
}
