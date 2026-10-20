package platform

import (
	"context"
	"database/sql"
	"strings"
)

type switchIdentityMAC struct{ kind, mac string }

func switchIdentityMACs(values map[string]string) ([]switchIdentityMAC, bool) {
	out := []switchIdentityMAC{}
	declared := false
	for _, field := range []struct{ column, kind string }{{"mgmt_mac", "mgmt"}, {"base_mac", "base"}, {"other_macs", "other"}} {
		raw, ok := values[field.column]
		if ok {
			declared = true
		}
		if raw == "" {
			continue
		}
		for _, part := range strings.Split(raw, ";") {
			out = append(out, switchIdentityMAC{kind: field.kind, mac: strings.TrimSpace(part)})
		}
	}
	return out, declared
}

// Check the planned switch identities before any BOM write. A MAC may belong
// to one physical switch only; server-port MACs cannot alias switch identity.
func validateSwitchIdentityMACs(ctx context.Context, tx *sql.Tx, pkg *BOMPackage) ([]BOMPackageIssue, error) {
	issues := []BOMPackageIssue{}
	add := func(sev string, row BOMPackageRow, field, code string) {
		issues = append(issues, BOMPackageIssue{Severity: sev, File: row.File, BOMLineNo: row.Line, Field: field, Code: code})
	}
	seen := map[string]string{}
	for _, row := range pkg.Rows["server/server_ports.csv"] {
		if mac := row.Values["mac"]; mac != "" {
			seen[mac] = "server"
		}
	}
	for _, row := range pkg.Rows["fabric/switches.csv"] {
		macs, declared := switchIdentityMACs(row.Values)
		if declared {
			for _, field := range []string{"mgmt_mac", "base_mac", "other_macs"} {
				if _, ok := row.Values[field]; !ok {
					add("BLOCK", row, field, "switch_identity_columns_incomplete")
				}
			}
		}
		if !declared || len(macs) == 0 {
			add("WARN", row, "mgmt_mac", "switch_identity_mac_unverified")
			continue
		}
		kinds := map[string]bool{}
		for _, identity := range macs {
			field := identity.kind + "_mac"
			if identity.kind == "other" {
				field = "other_macs"
			}
			mac, valid := normalizeMAC(identity.mac)
			if !valid {
				add("BLOCK", row, field, "invalid_switch_identity_mac")
				continue
			}
			if identity.kind != "other" && kinds[identity.kind] {
				add("BLOCK", row, field, "duplicate_switch_identity_kind")
			}
			kinds[identity.kind] = true
			if prior, found := seen[mac]; found {
				if prior == row.Values["sn"] {
					add("BLOCK", row, field, "duplicate_switch_identity_mac")
				} else {
					add("BLOCK", row, field, "switch_identity_mac_conflict")
				}
				continue
			}
			seen[mac] = row.Values["sn"]
			var existingSN string
			err := tx.QueryRowContext(ctx, `SELECT s.serial_number FROM switch_mac_bindings b JOIN switches s ON s.id=b.switch_id WHERE b.mac=$1`, mac).Scan(&existingSN)
			if err != nil && err != sql.ErrNoRows {
				return nil, err
			}
			if err == nil && existingSN != row.Values["sn"] {
				add("BLOCK", row, field, "switch_identity_mac_conflict")
			}
			var serverPort bool
			err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM physical_ports WHERE machine_id IS NOT NULL AND mac_address=$1)`, mac).Scan(&serverPort)
			if err != nil {
				return nil, err
			}
			if serverPort {
				add("BLOCK", row, field, "switch_identity_mac_conflict")
			}
			switch identity.kind {
			case "mgmt":
				row.Values["mgmt_mac"] = mac
			case "base":
				row.Values["base_mac"] = mac
			}
		}
		if raw := row.Values["other_macs"]; raw != "" {
			normalized := []string{}
			for _, part := range strings.Split(raw, ";") {
				if mac, ok := normalizeMAC(strings.TrimSpace(part)); ok {
					normalized = append(normalized, mac)
				}
			}
			row.Values["other_macs"] = strings.Join(normalized, ";")
		}
	}
	return issues, nil
}
