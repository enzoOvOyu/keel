package platform

import (
	"sort"
	"strings"
)

// Compare summary MACs only after parent_ref has been resolved with the same
// UID > line > business precedence as the writer. Raw names are not identity.
func validateResolvedComponentPortMACs(materials []*bomMaterial) []BOMPackageIssue {
	issues := []BOMPackageIssue{}
	ports := map[string][]BOMPackageRow{}
	for _, material := range materials {
		if material.category == "port" {
			ports[material.parentUID] = append(ports[material.parentUID], material.row)
		}
	}
	keys := func(set map[string]bool) string {
		out := []string{}
		for value := range set {
			out = append(out, value)
		}
		sort.Strings(out)
		return strings.Join(out, ",")
	}
	for _, nic := range materials {
		if nic.category != "nic" || nic.row.Values["mac"] == "" {
			continue
		}
		expected, actual := map[string]bool{}, map[string]bool{}
		for _, raw := range strings.Split(nic.row.Values["mac"], ",") {
			if mac, ok := normalizeMAC(raw); ok {
				expected[mac] = true
			}
		}
		for _, port := range ports[nic.uid] {
			if port.Values["mac"] == "" {
				issues = append(issues, bomMACIssue(port, nic.row.Values["model_ref"], "per_port_mac_missing", "permanent MAC", ""))
				continue
			}
			if mac, ok := normalizeMAC(port.Values["mac"]); ok {
				actual[mac] = true
			}
		}
		if keys(expected) != keys(actual) {
			issues = append(issues, bomMACIssue(nic.row, nic.row.Values["model_ref"], "component_port_mac_mismatch", keys(expected), keys(actual)))
		}
	}
	return issues
}
