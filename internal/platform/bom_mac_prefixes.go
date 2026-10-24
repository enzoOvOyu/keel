package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var modelMACPrefixPattern = regexp.MustCompile(`^[0-9a-fA-F]{2}(:[0-9a-fA-F]{2}){2}$`)

// An omitted rule is unknown. An explicit rule must be a nonempty OUI allowlist.
func modelMACPrefixes(raw json.RawMessage) ([]string, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, err
	}
	value, present := object["mac_prefixes"]
	if !present {
		return nil, nil
	}
	var prefixes []string
	if err := json.Unmarshal(value, &prefixes); err != nil {
		return nil, err
	}
	if len(prefixes) == 0 || len(prefixes) > 256 {
		return nil, fmt.Errorf("mac_prefixes must contain 1-256 colon-separated three-octet prefixes")
	}
	seen := map[string]bool{}
	for i, prefix := range prefixes {
		prefix = strings.ToLower(prefix)
		if !modelMACPrefixPattern.MatchString(prefix) || seen[prefix] {
			return nil, fmt.Errorf("mac_prefixes must contain distinct colon-separated three-octet prefixes")
		}
		seen[prefix] = true
		prefixes[i] = prefix
	}
	return prefixes, nil
}

// Consume the same resolved machine identities as the material writer. SN, line,
// UID and external component references must not change which server rule applies.
func validateBOMMACPrefixes(ctx context.Context, tx *sql.Tx, materials []*bomMaterial) ([]BOMPackageIssue, error) {
	issues := []BOMPackageIssue{}
	byMachine := map[string][]*bomMaterial{}
	for _, material := range materials {
		if material.category == "nic" || material.category == "port" {
			byMachine[material.machineUID] = append(byMachine[material.machineUID], material)
		}
	}
	for _, server := range materials {
		if server.category != "server" {
			continue
		}
		var raw json.RawMessage
		ref := server.row.Values["model_ref"]
		if err := tx.QueryRowContext(ctx, `SELECT bom_expect FROM device_model_versions WHERE id=$1`, ref).Scan(&raw); err != nil {
			return nil, err
		}
		prefixes, err := modelMACPrefixes(raw)
		if err != nil {
			issues = append(issues, macPrefixIssue(server.row, ref, "model_mac_expectation_invalid", "1-256 distinct three-octet prefixes", string(raw)))
			continue
		}
		if len(prefixes) == 0 {
			continue
		} // Legacy models have no implicit vendor rule.
		observed := false
		for _, material := range byMachine[server.machineUID] {
			for _, supplied := range strings.Split(material.row.Values["mac"], ",") {
				if strings.TrimSpace(supplied) == "" {
					continue
				}
				observed = true
				mac, valid := normalizeMAC(supplied)
				matches := false
				for _, prefix := range prefixes {
					if valid && strings.HasPrefix(mac, prefix+":") {
						matches = true
						break
					}
				}
				if !matches {
					issues = append(issues, macPrefixIssue(material.row, ref, "model_mac_prefix_mismatch", strings.Join(prefixes, ","), supplied))
				}
			}
		}
		if !observed {
			issue := macPrefixIssue(server.row, ref, "model_mac_prefix_unverified", strings.Join(prefixes, ","), "no permanent MAC supplied")
			issue.Severity = "WARN"
			issues = append(issues, issue)
		}
	}
	return issues, nil
}

func macPrefixIssue(row BOMPackageRow, ref, code, expected, actual string) BOMPackageIssue {
	line := row.Line
	if row.File == "legacy/wide.csv" {
		if original, err := strconv.Atoi(row.Values["legacy_source_line"]); err == nil {
			line = original
		}
	}
	return BOMPackageIssue{Severity: "BLOCK", File: row.File, BOMLineNo: line, Field: "mac", Code: code, Expected: expected, Actual: actual, ModelVersionRef: ref}
}
