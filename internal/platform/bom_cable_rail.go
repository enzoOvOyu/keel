package platform

import "strconv"

type packageCablePort struct {
	row    BOMPackageRow
	domain string
}

// This only compares planned endpoints explicitly present in the same package.
// An external UID or unresolved endpoint cannot be inferred from a display name;
// reference validation and G2 LLDP confirmation remain separate gates.
func validatePackageCableRails(pkg *BOMPackage) []BOMPackageIssue {
	issues := []BOMPackageIssue{}
	ports := map[string]packageCablePort{}
	refs := map[string]map[string]bool{}
	addRef := func(token, id string) {
		if token == "" {
			return
		}
		if refs[token] == nil {
			refs[token] = map[string]bool{}
		}
		refs[token][id] = true
	}
	for _, domain := range []struct{ file, name string }{{"server/server_ports.csv", "server"}, {"fabric/switch_ports.csv", "switch"}} {
		for _, row := range pkg.Rows[domain.file] {
			id := domain.file + ":" + strconv.Itoa(row.Line)
			ports[id] = packageCablePort{row: row, domain: domain.name}
			addRef(strconv.Itoa(row.Line), id)
			addRef(row.Values["port_name"], id)
			addRef(row.Values["parent_ref"]+":"+row.Values["port_index"], id)
			if row.Values["port_name"] != "" {
				addRef(row.Values["parent_ref"]+":"+row.Values["port_name"], id)
			}
		}
	}
	resolve := func(token string) (packageCablePort, bool) {
		found := refs[token]
		if len(found) != 1 {
			return packageCablePort{}, false
		}
		for id := range found {
			return ports[id], true
		}
		return packageCablePort{}, false
	}
	for _, cable := range pkg.Rows["fabric/cables.csv"] {
		a, okA := resolve(cable.Values["endpoint_a"])
		b, okB := resolve(cable.Values["endpoint_b"])
		if !okA || !okB {
			continue
		} // prior reference check or external UID
		var server, switchPort packageCablePort
		if a.domain == "server" && b.domain == "switch" {
			server, switchPort = a, b
		} else if b.domain == "server" && a.domain == "switch" {
			server, switchPort = b, a
		} else {
			continue
		}
		if server.row.Values["port_role"] != "compute" {
			continue
		}
		field := "endpoint_b"
		if a.domain == "switch" {
			field = "endpoint_a"
		}
		if switchPort.row.Values["port_role"] == "" {
			issues = append(issues, BOMPackageIssue{Severity: "WARN", File: cable.File, BOMLineNo: cable.Line, Field: field, Code: "cable_switch_port_role_unverified"})
		} else if switchPort.row.Values["port_role"] != "compute" {
			issues = append(issues, BOMPackageIssue{Severity: "BLOCK", File: cable.File, BOMLineNo: cable.Line, Field: field, Code: "cable_switch_port_role_mismatch"})
			continue
		}
		serverRail := server.row.Values["rail"]
		switchRail := switchPort.row.Values["rail"]
		if serverRail != "" && switchRail != "" && serverRail != switchRail {
			issues = append(issues, BOMPackageIssue{Severity: "BLOCK", File: cable.File, BOMLineNo: cable.Line, Field: field, Code: "cable_rail_mismatch"})
		}
	}
	return issues
}
