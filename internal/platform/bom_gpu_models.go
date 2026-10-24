package platform

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// These are the existing hardware specification fields, not inferred BOM facts.
func modelGPUExpectation(raw json.RawMessage) (*int, string, error) {
	var spec map[string]json.RawMessage
	if err := json.Unmarshal(raw, &spec); err != nil {
		return nil, "", err
	}
	var count *int
	if value, ok := spec["gpu_count"]; ok {
		var n int
		if string(value) == "null" || json.Unmarshal(value, &n) != nil || n < 0 {
			return nil, "", fmt.Errorf("hw_spec.gpu_count must be a nonnegative integer")
		}
		count = &n
	}
	name := ""
	if value, ok := spec["gpu"]; ok {
		if json.Unmarshal(value, &name) != nil || strings.TrimSpace(name) == "" || name != strings.TrimSpace(name) || utf8.RuneCountInString(name) > 120 {
			return nil, "", fmt.Errorf("hw_spec.gpu must be a published GPU model name of 1-120 characters without surrounding whitespace")
		}
	}
	return count, name, nil
}

func compareServerGPUHardware(server BOMPackageRow, gpus []BOMPackageRow, raw json.RawMessage, names map[string]string) []BOMPackageIssue {
	issues := []BOMPackageIssue{}
	add := func(row BOMPackageRow, code, expected, actual string) {
		issues = append(issues, BOMPackageIssue{Severity: "BLOCK", File: row.File, BOMLineNo: row.Line, Field: "model_ref", Code: code, ModelVersionRef: server.Values["model_ref"], Expected: expected, Actual: actual})
	}
	count, name, err := modelGPUExpectation(raw)
	if err != nil {
		add(server, "model_gpu_expectation_invalid", "nonnegative gpu_count and nonempty published GPU model name", string(raw))
		return issues
	}
	if count == nil && name == "" {
		return issues
	} // Older models supply no independent GPU rule.
	if server.File == "legacy/wide.csv" {
		add(server, "legacy_gpu_inventory_unverifiable", "entity BOM with physical GPU inventory and published GPU model references", "MAC rows cannot establish physical GPU inventory")
		return issues
	}
	if count != nil && len(gpus) != *count {
		add(server, "model_gpu_count_mismatch", strconv.Itoa(*count), strconv.Itoa(len(gpus)))
	}
	for _, gpu := range gpus {
		actual := names[gpu.Values["model_ref"]]
		if name != "" && actual != name {
			if actual == "" {
				actual = "unresolved published GPU model"
			}
			add(gpu, "model_gpu_type_mismatch", name, actual)
		}
	}
	if name != "" && len(gpus) == 0 && (count == nil || *count == 0) {
		issues = append(issues, BOMPackageIssue{Severity: "WARN", File: server.File, BOMLineNo: server.Line, Field: "model_ref", Code: "model_gpu_type_unverified", ModelVersionRef: server.Values["model_ref"], Expected: name, Actual: "no GPU inventory supplied"})
	}
	return issues
}
