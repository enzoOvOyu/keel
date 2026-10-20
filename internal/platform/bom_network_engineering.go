package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"
)

type switchEngineeringModel struct {
	HwSpec struct {
		PSUCount  *int     `json:"psu_count"`
		FeedCount *int     `json:"feed_count"`
		MaxPowerW *float64 `json:"max_power_w"`
	} `json:"hw_spec"`
	Topology struct {
		RailCount *int `json:"rail_count"`
	} `json:"expected_topology"`
}

// All amounts are declared engineering facts. An absent model limit or plan is
// unknown, never an implicit PASS. Ratio limits need an approved baseline; the
// BOM alone may detect a contradiction but cannot certify the target ratio.
func validateNetworkEngineering(ctx context.Context, tx *sql.Tx, pkg *BOMPackage) ([]BOMPackageIssue, error) {
	issues := []BOMPackageIssue{}
	add := func(sev string, row BOMPackageRow, field, code string) {
		issues = append(issues, BOMPackageIssue{Severity: sev, File: row.File, BOMLineNo: row.Line, Field: field, Code: code})
	}
	switches := map[string]BOMPackageRow{}
	modelCache := map[string]switchEngineeringModel{}
	for _, row := range pkg.Rows["fabric/switches.csv"] {
		switches[row.Values["sn"]] = row
		switches[strconv.Itoa(row.Line)] = row
		var model switchEngineeringModel
		if cached, ok := modelCache[row.Values["model_ref"]]; ok {
			model = cached
		} else {
			var hw, topology []byte
			err := tx.QueryRowContext(ctx, `SELECT hw_spec,expected_topology FROM device_model_versions WHERE id=$1`, row.Values["model_ref"]).Scan(&hw, &topology)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			} // model reference issue is reported elsewhere
			if err != nil {
				return nil, err
			}
			if err = json.Unmarshal(hw, &model.HwSpec); err != nil {
				return nil, err
			}
			if err = json.Unmarshal(topology, &model.Topology); err != nil {
				return nil, err
			}
			modelCache[row.Values["model_ref"]] = model
		}
		for _, check := range []struct {
			field   string
			minimum *int
		}{{"planned_psu_count", model.HwSpec.PSUCount}, {"planned_feed_count", model.HwSpec.FeedCount}} {
			raw := row.Values[check.field]
			if raw == "" {
				add("WARN", row, check.field, "switch_power_unverified")
				continue
			}
			n, e := strconv.Atoi(raw)
			if e != nil || n < 1 {
				add("BLOCK", row, check.field, "invalid_power_count")
				continue
			}
			if check.minimum == nil || *check.minimum < 1 {
				add("WARN", row, check.field, "switch_power_unverified")
				continue
			}
			if n < *check.minimum {
				add("BLOCK", row, check.field, "switch_power_count_below_model")
			}
		}
		raw := row.Values["power_budget_w"]
		if raw == "" {
			add("WARN", row, "power_budget_w", "switch_power_budget_unverified")
		} else {
			n, e := strconv.ParseFloat(raw, 64)
			if e != nil || math.IsNaN(n) || math.IsInf(n, 0) || n <= 0 {
				add("BLOCK", row, "power_budget_w", "invalid_power_budget")
			} else if model.HwSpec.MaxPowerW == nil || *model.HwSpec.MaxPowerW <= 0 {
				add("WARN", row, "power_budget_w", "switch_power_budget_unverified")
			} else if n < *model.HwSpec.MaxPowerW {
				add("BLOCK", row, "power_budget_w", "switch_power_budget_below_model")
			}
		}
		if raw := row.Values["max_downlink_uplink_ratio"]; raw != "" {
			n, e := strconv.ParseFloat(raw, 64)
			if e != nil || math.IsNaN(n) || math.IsInf(n, 0) || n <= 0 {
				add("BLOCK", row, "max_downlink_uplink_ratio", "invalid_bandwidth_ratio_limit")
			}
		}
	}
	type capacity struct {
		up, down           float64
		upCount, downCount int
		unknown            bool
		rails              map[string]bool
	}
	caps := map[string]*capacity{}
	breakoutParents := map[string]bool{}
	for _, port := range pkg.Rows["fabric/switch_ports.csv"] {
		if child := breakoutChildIndex.FindStringSubmatch(port.Values["port_index"]); child != nil {
			breakoutParents[port.Values["parent_ref"]+"\x00"+child[1]] = true
		}
	}
	for _, port := range pkg.Rows["fabric/switch_ports.csv"] {
		sw, ok := switches[port.Values["parent_ref"]]
		if !ok {
			continue
		} // external switch and unresolved refs handled separately
		key := sw.Values["sn"]
		c := caps[key]
		if c == nil {
			c = &capacity{rails: map[string]bool{}}
			caps[key] = c
		}
		direction := port.Values["bandwidth_direction"]
		if direction != "" && direction != "uplink" && direction != "downlink" && direction != "peer" && direction != "not_applicable" {
			add("BLOCK", port, "bandwidth_direction", "invalid_bandwidth_direction")
			c.unknown = true
		} else if direction == "" && port.Values["port_role"] != "unused" {
			add("WARN", port, "bandwidth_direction", "bandwidth_direction_unverified")
			c.unknown = true
		}
		if port.Values["port_role"] == "compute" {
			rail := strings.TrimSpace(port.Values["rail"])
			if rail == "" {
				add("BLOCK", port, "rail", "switch_compute_rail_missing")
			} else {
				c.rails[rail] = true
			}
		}
		if direction != "uplink" && direction != "downlink" {
			continue
		}
		if breakoutParents[port.Values["parent_ref"]+"\x00"+port.Values["port_index"]] {
			continue
		}
		speed, ok := engineeringSpeedGbps(port.Values["speed"])
		if !ok {
			severity := "WARN"
			if port.Values["speed"] != "" {
				severity = "BLOCK"
			}
			add(severity, port, "speed", "bandwidth_speed_unverified")
			c.unknown = true
			continue
		}
		if direction == "uplink" {
			c.up += speed
			c.upCount++
		} else {
			c.down += speed
			c.downCount++
		}
	}
	for _, sw := range pkg.Rows["fabric/switches.csv"] {
		c := caps[sw.Values["sn"]]
		model := modelCache[sw.Values["model_ref"]]
		if sw.Values["role"] == "leaf" {
			if c == nil || len(c.rails) == 0 || model.Topology.RailCount == nil || *model.Topology.RailCount < 1 {
				add("WARN", sw, "rail", "switch_rail_scale_unverified")
			} else if len(c.rails) != *model.Topology.RailCount {
				add("BLOCK", sw, "rail", "switch_rail_count_mismatch")
			}
		}
		if c == nil || c.unknown || c.upCount == 0 || c.downCount == 0 {
			add("WARN", sw, "bandwidth_direction", "bandwidth_ratio_unverified")
			continue
		}
		limit, err := strconv.ParseFloat(sw.Values["max_downlink_uplink_ratio"], 64)
		if err != nil || limit <= 0 || math.IsNaN(limit) || math.IsInf(limit, 0) {
			add("WARN", sw, "max_downlink_uplink_ratio", "bandwidth_ratio_limit_unverified")
			continue
		}
		if c.down/c.up > limit+1e-9 {
			add("BLOCK", sw, "max_downlink_uplink_ratio", "bandwidth_ratio_exceeded")
		}
		// Approval of the limit is a separate baseline gate, not inferred from CSV.
		add("WARN", sw, "max_downlink_uplink_ratio", "bandwidth_ratio_limit_approval_unverified")
	}
	return issues, nil
}

func engineeringSpeedGbps(raw string) (float64, bool) {
	raw = strings.ToUpper(strings.TrimSpace(raw))
	if len(raw) < 2 {
		return 0, false
	}
	unit := raw[len(raw)-1]
	n, e := strconv.ParseFloat(raw[:len(raw)-1], 64)
	if e != nil || n <= 0 || math.IsNaN(n) || math.IsInf(n, 0) {
		return 0, false
	}
	switch unit {
	case 'G':
		return n, true
	case 'T':
		return n * 1000, true
	case 'M':
		return n / 1000, true
	}
	return 0, false
}
