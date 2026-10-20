package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
)

type bomSwitchPortCapability struct {
	Speed         string   `json:"speed"`
	Speeds        []string `json:"speeds"`
	BreakoutModes []string `json:"breakout_modes"`
}

var breakoutChildIndex = regexp.MustCompile(`^(swp[0-9]+)s([0-9]+)$`)
var breakoutModeShape = regexp.MustCompile(`^([0-9]+)x([0-9]+[GMT])$`)

func switchPortCapabilityIssue(port, mode string, matrix json.RawMessage) string {
	var doc struct {
		Ports map[string]bomSwitchPortCapability `json:"ports"`
	}
	if err := json.Unmarshal(matrix, &doc); err != nil || len(doc.Ports) == 0 {
		return "switch_capability_ports_missing"
	}
	cap, ok := doc.Ports[port]
	child := breakoutChildIndex.FindStringSubmatch(port)
	if !ok && child != nil {
		cap, ok = doc.Ports[child[1]]
	}
	if !ok {
		return "switch_port_not_in_model"
	}
	if child != nil && mode == "" {
		if len(cap.BreakoutModes) != 1 {
			return "breakout_mode_required"
		}
		mode = cap.BreakoutModes[0]
	}
	if mode != "" {
		allowed := false
		for _, value := range cap.BreakoutModes {
			if strings.EqualFold(value, mode) {
				allowed = true
				break
			}
		}
		if !allowed {
			return "breakout_mode_not_supported"
		}
		if child != nil {
			parts := breakoutModeShape.FindStringSubmatch(mode)
			if parts == nil {
				return "breakout_mode_invalid"
			}
			count, _ := strconv.Atoi(parts[1])
			index, _ := strconv.Atoi(child[2])
			if index >= count {
				return "breakout_child_out_of_range"
			}
		}
	}
	return ""
}
func switchPortSpeedIssue(port, speed, mode string, matrix json.RawMessage) string {
	if speed == "" {
		return ""
	}
	var doc struct {
		Ports map[string]bomSwitchPortCapability `json:"ports"`
	}
	if err := json.Unmarshal(matrix, &doc); err != nil {
		return "switch_capability_ports_missing"
	}
	cap, ok := doc.Ports[port]
	if !ok {
		if parts := breakoutChildIndex.FindStringSubmatch(port); parts != nil {
			cap, ok = doc.Ports[parts[1]]
		}
	}
	if !ok {
		return "switch_port_not_in_model"
	}
	if child := breakoutChildIndex.FindStringSubmatch(port); child != nil {
		if mode == "" && len(cap.BreakoutModes) == 1 {
			mode = cap.BreakoutModes[0]
		}
		if parts := breakoutModeShape.FindStringSubmatch(mode); parts != nil && !strings.EqualFold(parts[2], speed) {
			return "breakout_speed_mismatch"
		}
	}
	if strings.EqualFold(cap.Speed, speed) {
		return ""
	}
	for _, value := range cap.Speeds {
		if strings.EqualFold(value, speed) {
			return ""
		}
	}
	return "switch_port_speed_not_supported"
}

// validateSwitchPortCapabilities compares declared port facts against the
// immutable published switch version. Missing model capabilities block import
// rather than being interpreted as a successful engineering check.
func validateSwitchPortCapabilities(ctx context.Context, tx *sql.Tx, projectID string, pkg *BOMPackage) ([]BOMPackageIssue, error) {
	issues := []BOMPackageIssue{}
	switches := map[string]string{}
	for _, row := range pkg.Rows["fabric/switches.csv"] {
		switches[row.Values["sn"]] = row.Values["model_ref"]
		switches[strconv.Itoa(row.Line)] = row.Values["model_ref"]
	}
	matrixCache := map[string]json.RawMessage{}
	for _, row := range pkg.Rows["fabric/switch_ports.csv"] {
		ref := row.Values["parent_ref"]
		modelRef := switches[ref]
		if modelRef == "" {
			err := tx.QueryRowContext(ctx, `SELECT i.model_version_id FROM bom_items i WHERE i.category='switch' AND (i.item_uid=$1 OR i.serial=$1) AND EXISTS(SELECT 1 FROM bom_item_revisions r WHERE r.item_uid=i.item_uid AND r.project_id=$2)`, ref, projectID).Scan(&modelRef)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return nil, err
			}
			if err != nil {
				continue
			} // Reference validation reports the missing switch.
		}
		matrix, ok := matrixCache[modelRef]
		if !ok {
			if err := tx.QueryRowContext(ctx, `SELECT capability_matrix FROM device_model_versions WHERE id=$1`, modelRef).Scan(&matrix); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					continue
				} // Model validation reports it.
				return nil, err
			}
			matrixCache[modelRef] = matrix
		}
		port := row.Values["port_index"]
		if code := switchPortCapabilityIssue(port, row.Values["breakout_mode"], matrix); code != "" {
			issues = append(issues, BOMPackageIssue{Severity: "BLOCK", File: row.File, BOMLineNo: row.Line, Field: "breakout_mode", Code: code})
			continue
		}
		if code := switchPortSpeedIssue(port, row.Values["speed"], row.Values["breakout_mode"], matrix); code != "" {
			issues = append(issues, BOMPackageIssue{Severity: "BLOCK", File: row.File, BOMLineNo: row.Line, Field: "speed", Code: code})
		}
	}
	return issues, nil
}
