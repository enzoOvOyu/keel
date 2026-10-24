package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
)

// Server hardware is checked against the exact published version in this
// package, never the latest version or a component's user supplied sub_type.
func validateServerModelHardware(ctx context.Context, tx *sql.Tx, projectID string, pkg *BOMPackage) ([]BOMPackageIssue, error) {
	issues := []BOMPackageIssue{}
	components := map[string][]BOMPackageRow{}
	for _, row := range pkg.Rows["server/server_components.csv"] {
		if row.Values["category"] == "nic" {
			components[row.Values["parent_sn"]] = append(components[row.Values["parent_sn"]], row)
		}
	}
	type expectation struct {
		raw   json.RawMessage
		slots []ModelSlot
		found bool
	}
	cache := map[string]expectation{}
	names := map[string]string{}
	for _, server := range pkg.Rows["server/servers.csv"] {
		ref := server.Values["model_ref"]
		fact, cached := cache[ref]
		if !cached {
			err := tx.QueryRowContext(ctx, `SELECT v.bom_expect FROM device_model_versions v JOIN device_models m ON m.id=v.model_id WHERE v.id=$1 AND m.kind='server' AND (m.scope_key='global' OR m.scope_key=$2)`, ref, "project:"+projectID).Scan(&fact.raw)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return nil, err
			}
			fact.found = err == nil
			if fact.found {
				rows, err := tx.QueryContext(ctx, `SELECT slot_no,pcie_slot,expect_nic_model FROM device_model_slots WHERE model_version_id=$1 ORDER BY slot_no`, ref)
				if err != nil {
					return nil, err
				}
				for rows.Next() {
					var slot ModelSlot
					if err = rows.Scan(&slot.SlotNo, &slot.PcieSlot, &slot.ExpectNicModel); err != nil {
						rows.Close()
						return nil, err
					}
					fact.slots = append(fact.slots, slot)
				}
				err = rows.Err()
				rows.Close()
				if err != nil {
					return nil, err
				}
			}
			cache[ref] = fact
		}
		if !fact.found {
			continue
		} // Existing model validation reports inadmissible references.
		nics := append([]BOMPackageRow{}, components[server.Values["sn"]]...)
		if line := strconv.Itoa(server.Line); line != server.Values["sn"] {
			nics = append(nics, components[line]...)
		}
		// The existing resolver also accepts an already registered machine UID.
		var uid string
		err := tx.QueryRowContext(ctx, `SELECT id FROM machines WHERE serial_number=$1`, server.Values["sn"]).Scan(&uid)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		if uid != "" && uid != server.Values["sn"] && uid != strconv.Itoa(server.Line) {
			nics = append(nics, components[uid]...)
		}
		for _, nic := range nics {
			modelRef := nic.Values["model_ref"]
			if _, ok := names[modelRef]; ok {
				continue
			}
			var name string
			err = tx.QueryRowContext(ctx, `SELECT m.name FROM device_model_versions v JOIN device_models m ON m.id=v.model_id WHERE v.id=$1 AND m.kind='nic' AND (m.scope_key='global' OR m.scope_key=$2)`, modelRef, "project:"+projectID).Scan(&name)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return nil, err
			}
			names[modelRef] = name
		}
		issues = append(issues, compareServerModelHardware(server, nics, fact.raw, fact.slots, names)...)
	}
	return issues, nil
}

func compareServerModelHardware(server BOMPackageRow, nics []BOMPackageRow, raw json.RawMessage, slots []ModelSlot, names map[string]string) []BOMPackageIssue {
	issues := []BOMPackageIssue{}
	add := func(row BOMPackageRow, field, code, expected, actual string) {
		issues = append(issues, BOMPackageIssue{Severity: "BLOCK", File: row.File, BOMLineNo: row.Line, Field: field, Code: code, Expected: expected, Actual: actual, ModelVersionRef: server.Values["model_ref"]})
	}
	var expect struct {
		NICCount *int `json:"nic_count"`
	}
	if err := json.Unmarshal(raw, &expect); err != nil || (expect.NICCount != nil && *expect.NICCount < 0) {
		add(server, "model_ref", "model_nic_expectation_invalid", "nonnegative integer nic_count", string(raw))
		return issues
	}
	if server.File == "legacy/wide.csv" && (expect.NICCount != nil || len(slots) > 0) {
		add(server, "model_ref", "legacy_nic_inventory_unverifiable", "entity BOM with physical NIC model and slot", "MAC rows cannot establish physical NIC inventory")
		return issues
	}
	if expect.NICCount == nil {
		issues = append(issues, BOMPackageIssue{Severity: "WARN", File: server.File, BOMLineNo: server.Line, Field: "model_ref", Code: "model_nic_count_unverified"})
	}
	if expect.NICCount != nil && len(nics) != *expect.NICCount {
		add(server, "model_ref", "model_nic_count_mismatch", strconv.Itoa(*expect.NICCount), strconv.Itoa(len(nics)))
	}
	if len(slots) == 0 {
		issues = append(issues, BOMPackageIssue{Severity: "WARN", File: server.File, BOMLineNo: server.Line, Field: "model_ref", Code: "model_nic_slots_unverified"})
		return issues
	} // No slot facts means unknown, not an inferred layout.
	occupied := map[int]bool{}
	for _, nic := range nics {
		actual := nic.Values["slot"]
		matches := []ModelSlot{}
		for _, slot := range slots {
			if actual == strconv.Itoa(slot.SlotNo) || actual == slot.PcieSlot {
				matches = append(matches, slot)
			}
		}
		if len(matches) != 1 {
			add(nic, "slot", "model_nic_slot_invalid", "one declared slot number or pcie_slot", actual)
			continue
		}
		slot := matches[0]
		if occupied[slot.SlotNo] {
			add(nic, "slot", "model_nic_slot_duplicate", strconv.Itoa(slot.SlotNo), actual)
		}
		occupied[slot.SlotNo] = true
		if name := names[nic.Values["model_ref"]]; name != "" && name != slot.ExpectNicModel {
			add(nic, "model_ref", "model_nic_type_mismatch", slot.ExpectNicModel, name)
		}
	}
	for _, slot := range slots {
		if !occupied[slot.SlotNo] {
			add(server, "model_ref", "model_nic_slot_missing", strconv.Itoa(slot.SlotNo), "absent")
		}
	}
	return issues
}
