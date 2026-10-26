package platform

import (
	"encoding/json"
	"testing"
)

func TestModelPreflightPCIIdentitiesAtPublicationBoundary(t *testing.T) {
	for _, raw := range []string{`null`, `{}`, `{"gpu_count":0}`, `{"gpu_pci_ids":["10de:2901"],"non_gpu_pci_ids":["1a03:2000"]}`} {
		if _, _, err := modelGPUExpectation(json.RawMessage(raw)); err != nil {
			t.Fatalf("rejected compatible/valid hardware %s: %v", raw, err)
		}
	}
	for _, raw := range []string{`{"gpu_pci_ids":null}`, `{"gpu_pci_ids":[]}`, `{"gpu_pci_ids":["10DE:2901"]}`, `{"gpu_pci_ids":["10de:2901"],"non_gpu_pci_ids":["10de:2901"]}`, `{"non_gpu_pci_ids":["1a03:2000","1a03:2000"]}`} {
		if _, _, err := modelGPUExpectation(json.RawMessage(raw)); err == nil {
			t.Fatalf("accepted invalid publication hardware %s", raw)
		}
	}
}
