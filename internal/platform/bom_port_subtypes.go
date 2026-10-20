package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
)

type bomPortSubtypeCapability struct {
	SubType      string `json:"sub_type"`
	ChildSubType string `json:"child_sub_type"`
}

func subtypeForPort(index string, raw json.RawMessage) string {
	var doc struct {
		Ports map[string]bomPortSubtypeCapability `json:"ports"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return ""
	}
	if subtype := strings.TrimSpace(doc.Ports[index].SubType); subtype != "" {
		return subtype
	}
	if child := breakoutChildIndex.FindStringSubmatch(index); child != nil {
		return strings.TrimSpace(doc.Ports[child[1]].ChildSubType)
	}
	return ""
}

// Standard package ports obtain subtype from the bound immutable parent model.
// Legacy wide CSV ports have only provisional MAC identities and use their
// explicit compatibility subtype in the legacy writer path.
func deriveBOMPortSubtypes(ctx context.Context, tx *sql.Tx, projectID string, pkg *BOMPackage) ([]BOMPackageIssue, error) {
	issues := []BOMPackageIssue{}
	lineParents := map[string]string{}
	businessParents := map[string]string{}
	for _, file := range []string{"server/servers.csv", "server/server_components.csv", "fabric/switches.csv"} {
		for _, row := range pkg.Rows[file] {
			model := row.Values["model_ref"]
			if model == "" {
				continue
			}
			lineParents[strconv.Itoa(row.Line)] = model
			key := row.Values["serial"]
			if file != "server/server_components.csv" {
				key = row.Values["sn"]
			}
			if key != "" {
				businessParents[key] = model
			}
		}
	}
	external := map[string]string{}
	matrices := map[string]json.RawMessage{}
	for _, file := range []string{"server/server_ports.csv", "fabric/switch_ports.csv"} {
		for _, row := range pkg.Rows[file] {
			ref := row.Values["parent_ref"]
			model := lineParents[ref]
			if model == "" {
				model = businessParents[ref]
			}
			if model == "" && bomUIDPattern.MatchString(ref) {
				if cached, ok := external[ref]; ok {
					model = cached
				} else {
					var value sql.NullString
					err := tx.QueryRowContext(ctx, `SELECT i.model_version_id FROM bom_items i WHERE i.item_uid=$1 AND i.category IN ('server','nic','switch') AND EXISTS(SELECT 1 FROM bom_item_revisions r WHERE r.item_uid=i.item_uid AND r.project_id=$2)`, ref, projectID).Scan(&value)
					if err != nil && !errors.Is(err, sql.ErrNoRows) {
						return nil, err
					}
					model = value.String
					external[ref] = model
				}
			}
			if model == "" {
				continue
			} // parent or model reference issues are reported elsewhere
			matrix, ok := matrices[model]
			if !ok {
				err := tx.QueryRowContext(ctx, `SELECT capability_matrix FROM device_model_versions WHERE id=$1`, model).Scan(&matrix)
				if errors.Is(err, sql.ErrNoRows) {
					continue
				}
				if err != nil {
					return nil, err
				}
				matrices[model] = matrix
			}
			subtype := subtypeForPort(row.Values["port_index"], matrix)
			if subtype == "" || len(subtype) > 120 {
				issues = append(issues, BOMPackageIssue{Severity: "BLOCK", File: row.File, BOMLineNo: row.Line, Field: "sub_type", Code: "port_sub_type_not_in_model"})
				continue
			}
			row.Values["sub_type"] = subtype
		}
	}
	return issues, nil
}
