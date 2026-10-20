package platform

import (
	"net"
	"strconv"
	"strings"
)

// validateMappedBOMRows checks the domain rules whose meaning depends on the
// approved column mapping. Database facts and model capability matrices are
// checked separately during preview and repeated by commit.
func validateMappedBOMRows(pkg *BOMPackage) {
	add := func(row BOMPackageRow, field, code string) {
		pkg.Issues = append(pkg.Issues, BOMPackageIssue{Severity: "BLOCK", File: row.File, BOMLineNo: row.Line, Field: field, Code: code})
	}
	serials := map[string]BOMPackageRow{}
	ports := map[string]BOMPackageRow{}
	for file, rows := range pkg.Rows {
		if _, required := requiredBOMFiles[file]; !required {
			continue
		}
		for _, row := range rows {
			v := row.Values
			category := ""
			switch file {
			case "server/servers.csv":
				category = "server"
			case "fabric/switches.csv":
				category = "switch"
			case "fabric/optic_modules.csv":
				category = "optic_module"
			case "server/server_components.csv":
				category = v["category"]
				if category != "nic" && category != "gpu" && category != "psu" && category != "bmc" {
					add(row, "category", "invalid_component_category")
				}
			}
			if sn := strings.TrimSpace(v["sn"]); sn != "" && (file == "server/servers.csv" || file == "fabric/switches.csv") {
				if !snPattern.MatchString(sn) {
					add(row, "sn", "invalid_serial")
				}
			}
			serial := v["serial"]
			if file == "server/servers.csv" || file == "fabric/switches.csv" {
				serial = v["sn"]
			}
			if serial != "" {
				if !snPattern.MatchString(serial) {
					add(row, "serial", "invalid_serial")
				}
				key := category + "\x00" + serial
				if _, exists := serials[key]; exists {
					add(row, "serial", "duplicate_category_serial")
				} else {
					serials[key] = row
				}
			}
			if file == "server/servers.csv" || file == "fabric/switches.csv" {
				if !hostPattern.MatchString(v["hostname"]) {
					add(row, "hostname", "invalid_hostname")
				}
			}
			if source := v["source_of_record"]; source != "" || file == "server/servers.csv" || file == "fabric/switches.csv" {
				if source != "design_bom" && source != "discovered" && source != "manual" {
					add(row, "source_of_record", "invalid_source")
				}
			}
			if status := v["status"]; status != "" && status != "planned" && status != "imported" && status != "verified" && status != "installed" && status != "retired" {
				add(row, "status", "invalid_status")
			}
			if file == "fabric/switches.csv" {
				if v["role"] != "leaf" && v["role"] != "spine" && v["role"] != "superspine" && v["role"] != "mgmt" {
					add(row, "role", "invalid_switch_role")
				}
				for _, field := range []string{"dc", "pod", "rack", "idx"} {
					if v[field] == "" {
						add(row, field, "required_value_missing")
					}
				}
			}
			if file == "server/server_ports.csv" || file == "fabric/switch_ports.csv" {
				key := v["parent_ref"] + "\x00" + v["port_index"]
				if _, exists := ports[key]; exists {
					add(row, "port_index", "duplicate_parent_port_index")
				} else {
					ports[key] = row
				}
				if role := v["port_role"]; role != "" && role != "management" && role != "storage" && role != "compute" && role != "oob" && role != "unused" {
					add(row, "port_role", "invalid_port_role")
				}
			}
			if file == "fabric/cables.csv" {
				if v["cable_type"] != "DAC" && v["cable_type"] != "AOC" && v["cable_type"] != "fiber" {
					add(row, "cable_type", "invalid_cable_type")
				}
				if length := v["length_m"]; length != "" {
					n, err := strconv.ParseFloat(length, 64)
					if err != nil || n < 0 {
						add(row, "length_m", "invalid_length")
					}
				}
			}
		}
	}
	validateComponentPortMACs(pkg)
}

func validateComponentPortMACs(pkg *BOMPackage) {
	for _, component := range pkg.Rows["server/server_components.csv"] {
		if component.Values["category"] != "nic" || component.Values["mac"] == "" {
			continue
		}
		expected := map[string]bool{}
		for _, raw := range strings.Split(component.Values["mac"], ",") {
			parsed, err := net.ParseMAC(strings.TrimSpace(raw))
			if err != nil || len(parsed) != 6 {
				pkg.Issues = append(pkg.Issues, BOMPackageIssue{Severity: "BLOCK", File: component.File, BOMLineNo: component.Line, Field: "mac", Code: "invalid_component_mac"})
				continue
			}
			expected[strings.ToLower(parsed.String())] = true
		}
		actual := map[string]bool{}
		line := strconv.Itoa(component.Line)
		for _, port := range pkg.Rows["server/server_ports.csv"] {
			if port.Values["parent_ref"] != line && port.Values["parent_ref"] != component.Values["serial"] {
				continue
			}
			if port.Values["mac"] == "" {
				pkg.Issues = append(pkg.Issues, BOMPackageIssue{Severity: "BLOCK", File: port.File, BOMLineNo: port.Line, Field: "mac", Code: "per_port_mac_missing"})
				continue
			}
			actual[port.Values["mac"]] = true
		}
		if len(expected) != len(actual) {
			pkg.Issues = append(pkg.Issues, BOMPackageIssue{Severity: "BLOCK", File: component.File, BOMLineNo: component.Line, Field: "mac", Code: "component_port_mac_mismatch"})
			continue
		}
		for mac := range expected {
			if !actual[mac] {
				pkg.Issues = append(pkg.Issues, BOMPackageIssue{Severity: "BLOCK", File: component.File, BOMLineNo: component.Line, Field: "mac", Code: "component_port_mac_mismatch"})
				break
			}
		}
	}
}
