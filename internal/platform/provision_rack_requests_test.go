package platform

import (
	"encoding/json"
	"strings"
	"testing"
)

func rackInputFixture() CreateProvisionRackRequestsInput {
	return CreateProvisionRackRequestsInput{Rack: " Rack-A ", Reason: "explicit reinstall preparation", Items: []ProvisionRackRequestItem{
		{AssetID: strings.Repeat("b", 32), ProvisionPreviewInput: ProvisionPreviewInput{AssetVersion: 3, BootPortID: strings.Repeat("c", 32), MediaID: strings.Repeat("d", 32), DomainKeyID: strings.Repeat("e", 32)}, ConfirmSerial: "SERVER-2"},
		{AssetID: strings.Repeat("a", 32), ProvisionPreviewInput: ProvisionPreviewInput{AssetVersion: 4, BootPortID: strings.Repeat("f", 32), MediaID: strings.Repeat("d", 32), DomainKeyID: strings.Repeat("e", 32)}, ConfirmSerial: "SERVER-1"},
	}}
}

func TestProvisionRackCanonicalSelection(t *testing.T) {
	in := rackInputFixture()
	out, err := canonicalProvisionRackInput(in)
	if err != nil || out.Items[0].AssetID >= out.Items[1].AssetID || in.Items[0].AssetID != strings.Repeat("b", 32) || out.Rack != " Rack-A " {
		t.Fatalf("canonicalization lost exact BOM or mutated selection: %v", err)
	}
	in.Items[0], in.Items[1] = in.Items[1], in.Items[0]
	again, err := canonicalProvisionRackInput(in)
	firstBytes, _ := json.Marshal(out)
	secondBytes, _ := json.Marshal(again)
	if err != nil || string(firstBytes) != string(secondBytes) {
		t.Fatal("same explicit selection in a different order changed its identity")
	}
	for _, mutate := range []func(*CreateProvisionRackRequestsInput){
		func(x *CreateProvisionRackRequestsInput) { x.Rack = "rack-a" },
		func(x *CreateProvisionRackRequestsInput) { x.Items[0].AssetVersion++ },
		func(x *CreateProvisionRackRequestsInput) { x.Items[0].ConfirmSerial += "-changed" },
		func(x *CreateProvisionRackRequestsInput) { x.Items[0].BootPortID = strings.Repeat("1", 32) },
		func(x *CreateProvisionRackRequestsInput) { x.Items[0].MediaID = strings.Repeat("2", 32) },
		func(x *CreateProvisionRackRequestsInput) { x.Items[0].DomainKeyID = strings.Repeat("3", 32) },
	} {
		changed := rackInputFixture()
		mutate(&changed)
		changed, err = canonicalProvisionRackInput(changed)
		encoded, _ := json.Marshal(changed)
		if err != nil || string(encoded) == string(firstBytes) {
			t.Fatal("an exact selected field was dropped from the canonical request")
		}
	}
}

func TestProvisionRackRejectsMissingDuplicateAndUnboundedSelection(t *testing.T) {
	for name, mutate := range map[string]func(*CreateProvisionRackRequestsInput){
		"empty":           func(x *CreateProvisionRackRequestsInput) { x.Items = nil },
		"over-limit":      func(x *CreateProvisionRackRequestsInput) { x.Items = make([]ProvisionRackRequestItem, 51) },
		"duplicate":       func(x *CreateProvisionRackRequestsInput) { x.Items[1] = x.Items[0] },
		"blank-rack":      func(x *CreateProvisionRackRequestsInput) { x.Rack = "  " },
		"long-rack":       func(x *CreateProvisionRackRequestsInput) { x.Rack = strings.Repeat("中", 121) },
		"rack-control":    func(x *CreateProvisionRackRequestsInput) { x.Rack = "rack\nA" },
		"empty-reason":    func(x *CreateProvisionRackRequestsInput) { x.Reason = " " },
		"missing-serial":  func(x *CreateProvisionRackRequestsInput) { x.Items[0].ConfirmSerial = "" },
		"partial-version": func(x *CreateProvisionRackRequestsInput) { x.Items[0].AssetVersion = 0 },
		"missing-port":    func(x *CreateProvisionRackRequestsInput) { x.Items[0].BootPortID = "" },
		"missing-os":      func(x *CreateProvisionRackRequestsInput) { x.Items[0].MediaID = "" },
		"missing-key":     func(x *CreateProvisionRackRequestsInput) { x.Items[0].DomainKeyID = "" },
	} {
		t.Run(name, func(t *testing.T) {
			in := rackInputFixture()
			mutate(&in)
			if _, err := canonicalProvisionRackInput(in); err == nil {
				t.Fatal("invalid or implicit selection accepted")
			}
		})
	}
}
