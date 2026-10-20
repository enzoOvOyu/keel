package platform

import (
	"net"
	"regexp"
	"strconv"
	"strings"
)

var bomUIDPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

// The template mapping may rename source headers only to fields with a defined
// BOM meaning. An unknown source column remains visible as WARN in preview.
var canonicalBOMColumns = map[string]string{
	"server/servers.csv":           "bom_line_no rack u dc pod sn hostname rail model_ref sub_type ip_hint credential_batch_ref source_of_record status",
	"server/server_components.csv": "bom_line_no parent_sn category sub_type slot serial mac model_ref vendor source_of_record status",
	"server/server_ports.csv":      "bom_line_no parent_ref port_index port_name mac port_role rail source_of_record status",
	"fabric/switches.csv":          "bom_line_no dc pod role rack idx sn hostname model_ref nos_intent source_of_record status credential_batch_ref",
	"fabric/switch_ports.csv":      "bom_line_no parent_ref port_index port_name port_role speed breakout_mode source_of_record status",
	"fabric/optic_modules.csv":     "bom_line_no parent_ref sub_type serial vendor model_ref media_type speed source_of_record status",
	"fabric/cables.csv":            "bom_line_no endpoint_a endpoint_b cable_type sub_type vendor length_m status source_of_record",
}

func isCanonicalBOMColumn(file, column string) bool {
	for _, known := range strings.Fields(canonicalBOMColumns[file]) {
		if known == column {
			return true
		}
	}
	return false
}

var requiredBOMValues = map[string][]string{
	"server/servers.csv":           {"bom_line_no", "sn", "hostname", "model_ref", "rack", "u", "sub_type"},
	"server/server_components.csv": {"bom_line_no", "parent_sn", "category", "sub_type", "model_ref"},
	"server/server_ports.csv":      {"bom_line_no", "parent_ref", "port_index", "port_role"},
	"fabric/switches.csv":          {"bom_line_no", "sn", "hostname", "model_ref", "role"},
	"fabric/switch_ports.csv":      {"bom_line_no", "parent_ref", "port_index"},
	"fabric/optic_modules.csv":     {"bom_line_no", "parent_ref", "serial", "model_ref"},
	"fabric/cables.csv":            {"bom_line_no", "endpoint_a", "endpoint_b", "cable_type", "sub_type"},
}

// ApplyBOMMappingProfile resolves the frozen project-template source aliases
// after raw byte hash validation. It checks row references inside the package;
// the database preview additionally resolves existing stable item_uid refs.
func ApplyBOMMappingProfile(pkg *BOMPackage, profile *BOMMappingProfile) {
	add := func(file string, line int, field, code string) {
		pkg.Issues = append(pkg.Issues, BOMPackageIssue{Severity: "BLOCK", File: file, BOMLineNo: line, Field: field, Code: code})
	}
	all := map[string][]BOMPackageRow{}
	lineSeen := map[int]string{}
	for file, rows := range pkg.Rows {
		if _, entityFile := requiredBOMFiles[file]; !entityFile {
			continue
		}
		mapping := profile.ServerColumns[file]
		if requiredBOMFiles[file].domain == "fabric" {
			mapping = profile.FabricColumns[file]
		}
		if len(rows) > 0 {
			for source := range rows[0].Values {
				if _, mapped := mapping[source]; !mapped && !isCanonicalBOMColumn(file, source) {
					pkg.Issues = append(pkg.Issues, BOMPackageIssue{Severity: "WARN", File: file, Field: source, Code: "unmapped_column"})
				}
			}
		}
		for i := range rows {
			values := rows[i].Values
			for source, target := range mapping {
				value, exists := values[source]
				if !exists || source == target {
					continue
				}
				if _, collision := values[target]; collision {
					add(file, rows[i].Line, target, "mapped_column_collision")
					continue
				}
				delete(values, source)
				values[target] = value
			}
			line, parseErr := strconv.Atoi(values["bom_line_no"])
			if parseErr != nil || line < 1 {
				add(file, rows[i].Line, "bom_line_no", "invalid_line_number")
			} else {
				rows[i].Line = line
				if previous, exists := lineSeen[line]; exists {
					add(file, line, "bom_line_no", "duplicate_line_number_with_"+previous)
				} else {
					lineSeen[line] = file
				}
			}
			for _, field := range requiredBOMValues[file] {
				if strings.TrimSpace(values[field]) == "" {
					add(file, rows[i].Line, field, "required_value_missing")
				}
			}
			all[file] = append(all[file], rows[i])
		}
		pkg.Rows[file] = rows
	}
	// Ref indexes carry physical row identity, not the display token. A display
	// token that matches multiple rows is ambiguous and must never pick one.
	refs := map[string]map[string][]string{}
	lineRefs := map[string]map[string]string{}
	addRef := func(kind, token, id string) {
		if token == "" {
			return
		}
		if refs[kind] == nil {
			refs[kind] = map[string][]string{}
		}
		refs[kind][token] = append(refs[kind][token], id)
	}
	for file, rows := range all {
		kind := ""
		switch file {
		case "server/servers.csv":
			kind = "server"
		case "server/server_components.csv":
			kind = "component"
		case "fabric/switches.csv":
			kind = "switch"
		case "server/server_ports.csv", "fabric/switch_ports.csv":
			kind = "port"
		}
		if kind == "" {
			continue
		}
		for _, row := range rows {
			id := file + ":" + strconv.Itoa(row.Line)
			if lineRefs[strconv.Itoa(row.Line)] == nil {
				lineRefs[strconv.Itoa(row.Line)] = map[string]string{}
			}
			lineRefs[strconv.Itoa(row.Line)][id] = kind
			v := row.Values
			switch kind {
			case "server", "switch":
				addRef(kind, v["sn"], id)
			case "component":
				addRef(kind, v["serial"], id)
			case "port":
				addRef(kind, v["port_name"], id)
				addRef(kind, v["parent_ref"]+":"+v["port_index"], id)
				if v["port_name"] != "" {
					addRef(kind, v["parent_ref"]+":"+v["port_name"], id)
				}
			}
		}
	}
	resolve := func(file string, row BOMPackageRow, field, token string, kinds ...string) string {
		if token == "" {
			return ""
		}
		found := map[string]bool{}
		// A package line number takes precedence over a matching business code.
		lineMatched := false
		if _, numeric := strconv.Atoi(token); numeric == nil {
			for id, kind := range lineRefs[token] {
				lineMatched = true
				for _, allowed := range kinds {
					if kind == allowed {
						found[id] = true
					}
				}
			}
		}
		if len(found) == 0 && !lineMatched {
			for _, kind := range kinds {
				for _, id := range refs[kind][token] {
					found[id] = true
				}
			}
		}
		if len(found) == 0 {
			if !bomUIDPattern.MatchString(token) {
				add(file, row.Line, field, "unresolved_reference")
			}
			return ""
		}
		if len(found) > 1 {
			add(file, row.Line, field, "ambiguous_reference")
			return ""
		}
		for id := range found {
			return id
		}
		return ""
	}
	for file, rows := range all {
		for _, row := range rows {
			v := row.Values
			switch file {
			case "server/server_components.csv":
				resolve(file, row, "parent_sn", v["parent_sn"], "server")
			case "server/server_ports.csv":
				resolve(file, row, "parent_ref", v["parent_ref"], "server", "component")
			case "fabric/switch_ports.csv":
				resolve(file, row, "parent_ref", v["parent_ref"], "switch")
			case "fabric/optic_modules.csv":
				resolve(file, row, "parent_ref", v["parent_ref"], "port")
			}
		}
	}
	macs := map[string]string{}
	for _, row := range all["server/server_ports.csv"] {
		mac := row.Values["mac"]
		if mac == "" {
			continue
		}
		hw, e := net.ParseMAC(mac)
		if e != nil || len(hw) != 6 {
			add(row.File, row.Line, "mac", "invalid_mac")
			continue
		}
		normalized := strings.ToLower(hw.String())
		if _, seen := macs[normalized]; seen {
			add(row.File, row.Line, "mac", "duplicate_mac")
		}
		macs[normalized] = row.File
		row.Values["mac"] = normalized
	}
	pairs := map[string]bool{}
	for _, row := range all["fabric/cables.csv"] {
		a := resolve(row.File, row, "endpoint_a", row.Values["endpoint_a"], "port")
		b := resolve(row.File, row, "endpoint_b", row.Values["endpoint_b"], "port")
		if a == "" || b == "" {
			continue
		}
		if a == b {
			add(row.File, row.Line, "endpoint_b", "same_endpoint")
			continue
		}
		if a > b {
			a, b = b, a
		}
		key := a + "|" + b
		if pairs[key] {
			add(row.File, row.Line, "endpoint_a", "duplicate_cable_pair")
		}
		pairs[key] = true
	}
}
