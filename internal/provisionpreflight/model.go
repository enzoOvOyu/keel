package provisionpreflight

import (
	"encoding/json"
	"strings"
)

// ExpectationFromModel consumes the exact immutable model version pinned by the
// request. The caller must check version/content hash and approval separately.
// Missing counts remain nil; neither imported MAC rows nor this host's observed
// inventory may supply independent hardware expectations.
func ExpectationFromModel(serial, bootMAC string, hardware, bom []byte) (Expectation, error) {
	out := Expectation{Serial: serial, BootMAC: bootMAC, MinimumDisks: 1}
	if len(hardware) > 1<<20 || len(bom) > 1<<20 {
		return out, ErrInvalid
	}
	var hw, expected map[string]json.RawMessage
	if json.Unmarshal(hardware, &hw) != nil || hw == nil || json.Unmarshal(bom, &expected) != nil || expected == nil {
		return out, ErrInvalid
	}
	if json.Unmarshal(hw["cpu_arch"], &out.Arch) != nil || (out.Arch != "amd64" && out.Arch != "arm64") {
		return out, ErrInvalid
	}
	var err error
	if out.GPUCount, err = modelCount(hw, "gpu_count", 0); err != nil {
		return out, err
	}
	if out.NICCount, err = modelCount(expected, "nic_count", 1); err != nil {
		return out, err
	}
	if out.GPUIdentities, out.NonGPUIdentities, err = ModelPCIIdentities(hardware); err != nil {
		return out, err
	}
	return out, nil
}
func modelCount(raw map[string]json.RawMessage, key string, minimum int) (*int, error) {
	value, ok := raw[key]
	if !ok {
		return nil, nil
	}
	var count int
	if string(value) == "null" || json.Unmarshal(value, &count) != nil || count < minimum || count > 256 {
		return nil, ErrInvalid
	}
	return &count, nil
}

// ModelPCIIdentities validates optional independently reviewed PCI IDs. An
// explicit non-GPU list distinguishes a BMC display controller from a GPU; no
// vendor/model-name heuristic silently excludes unknown accelerators.
func ModelPCIIdentities(hardware []byte) (gpu, nonGPU []string, err error) {
	if len(hardware) > 1<<20 {
		return nil, nil, ErrInvalid
	}
	var raw map[string]json.RawMessage
	if json.Unmarshal(hardware, &raw) != nil || raw == nil {
		return nil, nil, ErrInvalid
	}
	seen := map[string]bool{}
	for _, field := range []struct {
		key string
		dst *[]string
	}{{"gpu_pci_ids", &gpu}, {"non_gpu_pci_ids", &nonGPU}} {
		value, ok := raw[field.key]
		if !ok {
			continue
		}
		if string(value) == "null" || json.Unmarshal(value, field.dst) != nil || len(*field.dst) == 0 || len(*field.dst) > 256 {
			return nil, nil, ErrInvalid
		}
		for _, identity := range *field.dst {
			if !pciIdentity.MatchString(identity) || identity != strings.ToLower(identity) || seen[identity] {
				return nil, nil, ErrInvalid
			}
			seen[identity] = true
		}
	}
	return gpu, nonGPU, nil
}
