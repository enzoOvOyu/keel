package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
)

// validateServerRailScale compares the complete package's planned compute
// rail labels with the bound immutable server model. A missing model rail_count
// is recorded as unknown; it cannot be promoted to an engineering PASS.
func validateServerRailScale(ctx context.Context, tx *sql.Tx, projectID string, pkg *BOMPackage) ([]BOMPackageIssue, error) {
	issues := []BOMPackageIssue{}
	servers := map[string]BOMPackageRow{}
	serverBySN := map[string]BOMPackageRow{}
	for _, row := range pkg.Rows["server/servers.csv"] {
		servers[strconv.Itoa(row.Line)] = row
		servers[row.Values["sn"]] = row
		serverBySN[row.Values["sn"]] = row
	}
	for _, row := range pkg.Rows["server/server_components.csv"] {
		if row.Values["category"] != "nic" {
			continue
		}
		server, ok := servers[row.Values["parent_sn"]]
		if !ok {
			continue
		}
		for _, key := range []string{strconv.Itoa(row.Line), row.Values["serial"]} {
			if key != "" {
				servers[key] = server
			}
		}
	}
	rails := map[string]map[string]bool{}
	externalParent := map[string]string{}
	for _, port := range pkg.Rows["server/server_ports.csv"] {
		if port.Values["port_role"] != "compute" {
			continue
		}
		ref := port.Values["parent_ref"]
		server, ok := servers[ref]
		if !ok {
			sn, cached := externalParent[ref]
			if !cached {
				err := tx.QueryRowContext(ctx, `SELECT CASE WHEN count(DISTINCT m.id)=1 THEN min(m.serial_number) ELSE '' END FROM machines m WHERE m.id=$1 OR EXISTS(SELECT 1 FROM components c WHERE c.id=$1 AND c.machine_id=m.id) OR EXISTS(SELECT 1 FROM components c WHERE c.serial_number=$1 AND c.machine_id=m.id)`, ref).Scan(&sn)
				if err != nil && !errors.Is(err, sql.ErrNoRows) {
					return nil, err
				}
				externalParent[ref] = sn
			}
			server, ok = serverBySN[sn]
		}
		if !ok {
			continue
		} // Reference validation reports unresolved parents.
		sn := server.Values["sn"]
		rail := strings.TrimSpace(port.Values["rail"])
		if rail == "" {
			issues = append(issues, BOMPackageIssue{Severity: "BLOCK", File: port.File, BOMLineNo: port.Line, Field: "rail", Code: "compute_rail_missing"})
			continue
		}
		if rails[sn] == nil {
			rails[sn] = map[string]bool{}
		}
		rails[sn][rail] = true
	}
	for sn, row := range serverBySN {
		var raw []byte
		err := tx.QueryRowContext(ctx, `SELECT expected_topology FROM device_model_versions WHERE id=$1`, row.Values["model_ref"]).Scan(&raw)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		} // Model validation reports it.
		if err != nil {
			return nil, err
		}
		var topology struct {
			RailCount *int `json:"rail_count"`
		}
		if err = json.Unmarshal(raw, &topology); err != nil || topology.RailCount == nil || *topology.RailCount < 1 {
			issues = append(issues, BOMPackageIssue{Severity: "WARN", File: row.File, BOMLineNo: row.Line, Field: "rail", Code: "rail_scale_unverified"})
			continue
		}
		if len(rails[sn]) != *topology.RailCount {
			issues = append(issues, BOMPackageIssue{Severity: "BLOCK", File: row.File, BOMLineNo: row.Line, Field: "rail", Code: "rail_count_mismatch"})
		}
	}
	return issues, nil
}
