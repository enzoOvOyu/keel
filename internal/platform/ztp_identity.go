package platform

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

type ZTPIdentityInput struct {
	Serial    string   `json:"serial"`
	MgmtMAC   string   `json:"mgmt_mac"`
	BaseMAC   string   `json:"base_mac"`
	OtherMACs []string `json:"other_macs"`
}

type ZTPIdentityResolution struct {
	Verdict          string   `json:"verdict"`
	SwitchID         string   `json:"switch_id,omitempty"`
	IdentityPriority string   `json:"identity_priority,omitempty"`
	Reasons          []string `json:"reasons"`
}

// ResolveZTPIdentity is the read-only SoT classifier shared by both planned
// bootstrap channels. It never authorizes production configuration or marks a
// switch managed. The 143 channel still requires IDevID/mTLS before calling it.
func (s *Store) ResolveZTPIdentity(ctx context.Context, a *AuthSession, project *ProjectSummary, input ZTPIdentityInput) (*ZTPIdentityResolution, error) {
	tx, err := s.beginControlRead(ctx, a)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = requireProjectMember(ctx, tx, a, project.ID); err != nil {
		return nil, err
	}
	input.Serial = strings.TrimSpace(input.Serial)
	if input.Serial != "" && !snPattern.MatchString(input.Serial) {
		return nil, apiError(400, "ZTP_IDENTITY_INVALID", "Invalid switch serial")
	}
	type claim struct{ kind, mac string }
	claims := []claim{}
	for _, entry := range []claim{{"mgmt", input.MgmtMAC}, {"base", input.BaseMAC}} {
		if strings.TrimSpace(entry.mac) == "" {
			continue
		}
		mac, ok := normalizeMAC(entry.mac)
		if !ok {
			return nil, apiError(400, "ZTP_IDENTITY_INVALID", "Invalid switch MAC")
		}
		claims = append(claims, claim{entry.kind, mac})
	}
	if len(input.OtherMACs) > 16 {
		return nil, apiError(400, "ZTP_IDENTITY_INVALID", "Too many other MACs")
	}
	for _, raw := range input.OtherMACs {
		mac, ok := normalizeMAC(raw)
		if !ok {
			return nil, apiError(400, "ZTP_IDENTITY_INVALID", "Invalid switch MAC")
		}
		claims = append(claims, claim{"other", mac})
	}
	if input.Serial == "" && len(claims) == 0 {
		return nil, apiError(400, "ZTP_IDENTITY_REQUIRED", "At least one switch identity is required")
	}
	out := &ZTPIdentityResolution{Verdict: "isolated", Reasons: []string{}}
	candidate := ""
	priority := ""
	if input.Serial != "" {
		err = tx.QueryRowContext(ctx, `SELECT s.id FROM switches s WHERE s.serial_number=$1 AND EXISTS(SELECT 1 FROM bom_item_revisions r WHERE r.item_uid=s.id AND r.project_id=$2)`, input.Serial, project.ID).Scan(&candidate)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		if err == nil {
			priority = "sn"
		}
	}
	serialMatched := priority == "sn"
	matched := map[string]string{}
	for _, claim := range claims {
		var id string
		err = tx.QueryRowContext(ctx, `SELECT b.switch_id FROM switch_mac_bindings b WHERE b.mac=$1 AND EXISTS(SELECT 1 FROM bom_item_revisions r WHERE r.item_uid=b.switch_id AND r.project_id=$2)`, claim.mac, project.ID).Scan(&id)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		if err == nil {
			matched[claim.kind+":"+claim.mac] = id
			if candidate == "" {
				candidate = id
				priority = claim.kind
			}
		}
	}
	if candidate == "" {
		out.Reasons = append(out.Reasons, "identity_not_in_sot")
		return out, tx.Commit()
	}
	if input.Serial != "" && !serialMatched {
		out.Verdict = "conflict"
		out.Reasons = append(out.Reasons, "serial_not_bound_to_matched_switch")
		return out, tx.Commit()
	}
	for _, id := range matched {
		if id != candidate {
			out.Verdict = "conflict"
			out.Reasons = append(out.Reasons, "identity_keys_disagree")
			return out, tx.Commit()
		}
	}
	expected := map[string]string{}
	rows, err := tx.QueryContext(ctx, `SELECT kind,mac FROM switch_mac_bindings WHERE switch_id=$1`, candidate)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var kind, mac string
		if err = rows.Scan(&kind, &mac); err != nil {
			rows.Close()
			return nil, err
		}
		if kind != "other" {
			expected[kind] = mac
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, claim := range claims {
		if claim.kind == "other" {
			continue
		}
		if want := expected[claim.kind]; want != "" && want != claim.mac {
			out.Verdict = "conflict"
			out.Reasons = append(out.Reasons, claim.kind+"_mac_mismatch")
			return out, tx.Commit()
		}
		if expected[claim.kind] == "" {
			out.Reasons = append(out.Reasons, claim.kind+"_mac_not_in_sot")
		}
	}
	if len(out.Reasons) > 0 {
		return out, tx.Commit()
	}
	out.Verdict = "matched"
	out.SwitchID = candidate
	out.IdentityPriority = priority
	return out, tx.Commit()
}
