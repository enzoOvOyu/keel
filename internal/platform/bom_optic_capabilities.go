package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
)

type bomOpticModelCapability struct {
	Speed      string   `json:"speed"`
	Speeds     []string `json:"speeds"`
	MediaTypes []string `json:"media_types"`
}

type bomSwitchOpticPortCapability struct {
	Speed           string   `json:"speed"`
	Speeds          []string `json:"speeds"`
	OpticMediaTypes []string `json:"optic_media_types"`
}

func oneDeclaredSpeed(single string, many []string) string {
	if single != "" {
		return strings.ToUpper(strings.TrimSpace(single))
	}
	if len(many) == 1 {
		return strings.ToUpper(strings.TrimSpace(many[0]))
	}
	return ""
}
func containsFold(values []string, value string) bool {
	for _, v := range values {
		if strings.EqualFold(strings.TrimSpace(v), strings.TrimSpace(value)) {
			return true
		}
	}
	return false
}
func opticCompatibilityIssues(row BOMPackageRow, portSpeed string, portCap bomSwitchOpticPortCapability, opticCap bomOpticModelCapability) []BOMPackageIssue {
	out := []BOMPackageIssue{}
	add := func(severity, field, code string) {
		out = append(out, BOMPackageIssue{Severity: severity, File: row.File, BOMLineNo: row.Line, Field: field, Code: code})
	}
	opticSpeed := strings.ToUpper(strings.TrimSpace(row.Values["speed"]))
	modelSpeed := oneDeclaredSpeed(opticCap.Speed, opticCap.Speeds)
	if opticSpeed == "" {
		opticSpeed = modelSpeed
	}
	if modelSpeed != "" && opticSpeed != "" && modelSpeed != opticSpeed {
		add("BLOCK", "speed", "optic_model_speed_mismatch")
	}
	if opticSpeed != "" && len(opticCap.Speeds) > 1 && !containsFold(opticCap.Speeds, opticSpeed) {
		add("BLOCK", "speed", "optic_model_speed_mismatch")
	}
	portSpeed = strings.ToUpper(strings.TrimSpace(portSpeed))
	if portSpeed == "" {
		portSpeed = oneDeclaredSpeed(portCap.Speed, portCap.Speeds)
	}
	if opticSpeed == "" || portSpeed == "" {
		add("WARN", "speed", "optic_speed_unverified")
	} else if opticSpeed != portSpeed {
		add("BLOCK", "speed", "optic_port_speed_mismatch")
	}
	media := strings.TrimSpace(row.Values["media_type"])
	if media != "" && len(opticCap.MediaTypes) > 0 && !containsFold(opticCap.MediaTypes, media) {
		add("BLOCK", "media_type", "optic_model_media_mismatch")
	}
	if media != "" && len(portCap.OpticMediaTypes) > 0 && !containsFold(portCap.OpticMediaTypes, media) {
		add("BLOCK", "media_type", "optic_port_media_mismatch")
	}
	return out
}

// validateOpticCapabilities uses only published model versions and declared
// package facts. An unresolved or underspecified speed remains WARN, never a
// fabricated engineering PASS; reference validation handles missing parents.
func validateOpticCapabilities(ctx context.Context, tx *sql.Tx, projectID string, pkg *BOMPackage) ([]BOMPackageIssue, error) {
	out := []BOMPackageIssue{}
	byLine := map[string]BOMPackageRow{}
	byBusiness := map[string][]BOMPackageRow{}
	for _, port := range pkg.Rows["fabric/switch_ports.csv"] {
		byLine[strconv.Itoa(port.Line)] = port
		seen := map[string]bool{}
		for _, key := range []string{port.Values["port_name"], port.Values["parent_ref"] + ":" + port.Values["port_index"]} {
			if key != "" && key != ":" && !seen[key] {
				byBusiness[key] = append(byBusiness[key], port)
				seen[key] = true
			}
		}
	}
	switchModels := map[string]string{}
	for _, sw := range pkg.Rows["fabric/switches.csv"] {
		switchModels[sw.Values["sn"]] = sw.Values["model_ref"]
		switchModels[strconv.Itoa(sw.Line)] = sw.Values["model_ref"]
	}
	switchMatrices := map[string]json.RawMessage{}
	for _, row := range pkg.Rows["fabric/optic_modules.csv"] {
		modelRef := row.Values["model_ref"]
		var hwRaw, capRaw []byte
		err := tx.QueryRowContext(ctx, `SELECT hw_spec,capability_matrix FROM device_model_versions WHERE id=$1`, modelRef).Scan(&hwRaw, &capRaw)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		} // Model validation reports it.
		if err != nil {
			return nil, err
		}
		opticCap := bomOpticModelCapability{}
		_ = json.Unmarshal(capRaw, &opticCap)
		hw := bomOpticModelCapability{}
		_ = json.Unmarshal(hwRaw, &hw)
		if opticCap.Speed == "" && len(opticCap.Speeds) == 0 {
			opticCap.Speed = hw.Speed
			opticCap.Speeds = hw.Speeds
		}
		if len(opticCap.MediaTypes) == 0 {
			opticCap.MediaTypes = hw.MediaTypes
		}
		ref := row.Values["parent_ref"]
		parents := byBusiness[ref]
		if port, byNumber := byLine[ref]; byNumber {
			parents = []BOMPackageRow{port}
		}
		if len(parents) != 1 {
			out = append(out, BOMPackageIssue{Severity: "WARN", File: row.File, BOMLineNo: row.Line, Field: "parent_ref", Code: "optic_parent_speed_unverified"})
			continue
		}
		parent := parents[0]
		var portCap bomSwitchOpticPortCapability
		switchRef := switchModels[parent.Values["parent_ref"]]
		if switchRef == "" {
			err = tx.QueryRowContext(ctx, `SELECT i.model_version_id FROM bom_items i WHERE i.category='switch' AND (i.item_uid=$1 OR i.serial=$1) AND EXISTS(SELECT 1 FROM bom_item_revisions r WHERE r.item_uid=i.item_uid AND r.project_id=$2)`, parent.Values["parent_ref"], projectID).Scan(&switchRef)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return nil, err
			}
		}
		if switchRef != "" {
			matrix, ok := switchMatrices[switchRef]
			if !ok {
				err = tx.QueryRowContext(ctx, `SELECT capability_matrix FROM device_model_versions WHERE id=$1`, switchRef).Scan(&matrix)
				if err != nil && !errors.Is(err, sql.ErrNoRows) {
					return nil, err
				}
				switchMatrices[switchRef] = matrix
			}
			var doc struct {
				Ports map[string]bomSwitchOpticPortCapability `json:"ports"`
			}
			_ = json.Unmarshal(matrix, &doc)
			portCap = doc.Ports[parent.Values["port_index"]]
			if child := breakoutChildIndex.FindStringSubmatch(parent.Values["port_index"]); child != nil {
				base := doc.Ports[child[1]]
				if portCap.Speed == "" && len(portCap.Speeds) == 0 {
					portCap.Speed, portCap.Speeds = base.Speed, base.Speeds
				}
				if len(portCap.OpticMediaTypes) == 0 {
					portCap.OpticMediaTypes = base.OpticMediaTypes
				}
			}
		}
		out = append(out, opticCompatibilityIssues(row, parent.Values["speed"], portCap, opticCap)...)
	}
	return out, nil
}
