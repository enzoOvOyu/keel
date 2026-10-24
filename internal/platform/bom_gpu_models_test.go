package platform

import (
	"encoding/json"
	"testing"
)

func TestModelGPUValidation(t *testing.T) {
	for _, spec := range []map[string]any{{"gpu_count": -1}, {"gpu_count": 1.5}, {"gpu_count": "2"}, {"gpu_count": nil}, {"gpu_count": true}, {"gpu": ""}, {"gpu": nil}, {"gpu": []string{"B300"}}, {"gpu": " B300 "}} {
		if err := validateModelPayload(PublishModelVersionInput{HwSpec: spec}); err == nil {
			t.Fatalf("accepted malformed GPU expectation: %#v", spec)
		}
	}
	for _, spec := range []map[string]any{nil, {}, {"gpu_count": 0}, {"gpu_count": 8}, {"gpu_count": float64(8), "gpu": "B300"}, {"gpu": "B300"}} {
		if err := validateModelPayload(PublishModelVersionInput{HwSpec: spec}); err != nil {
			t.Fatalf("rejected GPU expectation: %#v %v", spec, err)
		}
	}
}

func TestServerGPUHardwareDifferences(t *testing.T) {
	server := BOMPackageRow{File: "server/servers.csv", Line: 1, Values: map[string]string{"model_ref": "server-v1"}}
	gpu := BOMPackageRow{File: "server/server_components.csv", Line: 8, Values: map[string]string{"model_ref": "gpu-v1", "sub_type": "untrusted"}}
	for _, tc := range []struct {
		name, spec string
		rows       []BOMPackageRow
		names      map[string]string
		code       string
	}{
		{"exact", `{"gpu_count":1,"gpu":"B300"}`, []BOMPackageRow{gpu}, map[string]string{"gpu-v1": "B300"}, ""},
		{"count", `{"gpu_count":2}`, []BOMPackageRow{gpu}, nil, "model_gpu_count_mismatch"},
		{"missing", `{"gpu_count":1}`, nil, nil, "model_gpu_count_mismatch"},
		{"zero", `{"gpu_count":0}`, nil, nil, ""},
		{"unexpected", `{"gpu_count":0}`, []BOMPackageRow{gpu}, nil, "model_gpu_count_mismatch"},
		{"wrong model", `{"gpu":"B300"}`, []BOMPackageRow{gpu}, map[string]string{"gpu-v1": "Other-GPU"}, "model_gpu_type_mismatch"},
		{"unresolved", `{"gpu":"B300"}`, []BOMPackageRow{gpu}, nil, "model_gpu_type_mismatch"},
		{"no inventory", `{"gpu":"B300"}`, nil, nil, "model_gpu_type_unverified"},
		{"unspecified", `{}`, []BOMPackageRow{gpu}, nil, ""},
		{"invalid stored count", `{"gpu_count":null}`, nil, nil, "model_gpu_expectation_invalid"},
		{"invalid stored type", `{"gpu":null}`, nil, nil, "model_gpu_expectation_invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			issues := compareServerGPUHardware(server, tc.rows, json.RawMessage(tc.spec), tc.names)
			if tc.code == "" {
				if len(issues) != 0 {
					t.Fatalf("unexpected differences: %+v", issues)
				}
				return
			}
			if len(issues) != 1 || issues[0].Code != tc.code || issues[0].ModelVersionRef != "server-v1" || issues[0].Expected == "" || issues[0].Actual == "" || issues[0].BOMLineNo == 0 {
				t.Fatalf("missing located difference: %+v", issues)
			}
		})
	}
	server.File = "legacy/wide.csv"
	for _, spec := range []string{`{"gpu_count":0}`, `{"gpu_count":8}`, `{"gpu":"B300"}`} {
		issues := compareServerGPUHardware(server, nil, json.RawMessage(spec), nil)
		if len(issues) != 1 || issues[0].Code != "legacy_gpu_inventory_unverifiable" || issues[0].Severity != "BLOCK" {
			t.Fatalf("wide MAC rows proved GPU inventory: %+v", issues)
		}
	}
}
