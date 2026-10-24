package platform

import (
	"encoding/json"
	"testing"
)

func TestCompatibilityMetadataRejectsUnverifiableRules(t *testing.T) {
	for _, raw := range []string{
		`{"unknown":{"os.version":["v1"]}}`,
		`{"compatibility":{"source":"vendor","version":"v1","requires":{"cpu.arch":["amd64"]}}}`,
		`{"compatibility":{"source":"vendor","version":"v1","report_ref":"report:1","requires":{"cpu.arch":[]}}}`,
		`{"compatibility":{"source":"vendor","version":"v1","report_ref":"report:1","requires":{"cpu.arch":["amd64","amd64"]}}}`,
		`{"compatibility":{"source":"vendor","version":"v1","report_ref":"report:1","requires":{"cpu.arch":["amd64"]},"ignored":true}}`,
	} {
		if _, e := decodeCompatibility(json.RawMessage(raw)); e == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	for _, raw := range []string{`{}`, `null`, `{"compatibility":{"source":"vendor-report","version":"reviewed-1","report_ref":"report:sha256:123","requires":{"os.kernel":["6.8.0-52"],"cpu.arch":["amd64"]}}}`} {
		if _, e := decodeCompatibility(json.RawMessage(raw)); e != nil {
			t.Fatal(e)
		}
	}
}

func TestCompatibilityCombinations(t *testing.T) {
	base := Compatibility{Source: "independent-fixture", Version: "matrix-2", ReportRef: "report:fixture", Combinations: []map[string]string{
		{"os.kernel": "kernel-a", "runtime.profile": "tool-a"},
		{"os.kernel": "kernel-b", "runtime.profile": "tool-b"},
	}}
	raw, _ := json.Marshal(catalogDependencies{Compatibility: &base})
	c, e := decodeCompatibility(raw)
	if e != nil {
		t.Fatal(e)
	}
	keys := map[string]ParamKeySummary{"os.kernel": {State: "active"}, "runtime.profile": {State: "active"}}
	for _, tc := range []struct {
		name   string
		values map[string]any
		valid  bool
	}{
		{"first row", map[string]any{"os.kernel": "kernel-a", "runtime.profile": "tool-a"}, true},
		{"second row", map[string]any{"os.kernel": "kernel-b", "runtime.profile": "tool-b"}, true},
		{"cross row", map[string]any{"os.kernel": "kernel-a", "runtime.profile": "tool-b"}, false},
		{"reverse cross row", map[string]any{"os.kernel": "kernel-b", "runtime.profile": "tool-a"}, false},
		{"missing", map[string]any{"os.kernel": "kernel-a"}, false},
		{"wrong type", map[string]any{"os.kernel": "kernel-a", "runtime.profile": true}, false},
		{"exact case", map[string]any{"os.kernel": "KERNEL-A", "runtime.profile": "tool-a"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchesCompatibilityCombination(c, tc.values, keys); got != tc.valid {
				t.Fatalf("match %v, wanted %v", got, tc.valid)
			}
		})
	}
	values := map[string]any{"os.kernel": "kernel-a", "runtime.profile": "tool-a"}
	delete(keys, "runtime.profile")
	if matchesCompatibilityCombination(c, values, keys) {
		t.Fatal("unregistered dependency accepted")
	}
	keys["runtime.profile"] = ParamKeySummary{State: "retired"}
	if matchesCompatibilityCombination(c, values, keys) {
		t.Fatal("retired dependency accepted")
	}
	for _, rows := range []any{
		[]map[string]string{{}},
		[]map[string]string{{"os.kernel": "a"}, {"runtime.profile": "b"}},
		[]map[string]string{{"os.kernel": "a"}, {"os.kernel": "a", "runtime.profile": "b"}},
		[]map[string]string{{"Bad Key": "a"}},
		[]map[string]string{{"os.kernel": " "}},
		[]map[string]string{{"os.kernel": "a"}, {"os.kernel": "a"}},
		[]map[string]any{{"os.kernel": 1}},
		make([]map[string]string, 129),
	} {
		encoded, _ := json.Marshal(map[string]any{"compatibility": map[string]any{"source": "fixture", "version": "v1", "report_ref": "report:1", "combinations": rows}})
		if _, err := decodeCompatibility(encoded); err == nil {
			t.Fatalf("invalid combinations accepted %s", encoded)
		}
	}
}
