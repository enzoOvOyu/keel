package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
)

type ProvisionKeyRepairBatchItem struct {
	InstallationID  string `json:"installation_id"`
	ExpectedVersion int64  `json:"expected_version"`
	RepairEventID   int64  `json:"repair_event_id,omitempty"`
	ConfirmSerial   string `json:"confirm_serial,omitempty"`
	RepairID        string `json:"repair_id,omitempty"`
}
type ProvisionKeyRepairBatchInput struct {
	Action string                        `json:"action"`
	Items  []ProvisionKeyRepairBatchItem `json:"items"`
	Reason string                        `json:"reason"`
}
type ProvisionKeyRepairBatch struct {
	Requests []ProvisionKeyRepair `json:"requests"`
}

func canonicalProvisionKeyRepairBatch(in ProvisionKeyRepairBatchInput) (ProvisionKeyRepairBatchInput, error) {
	if len(in.Items) < 1 || len(in.Items) > 32 || !validProvisionReason(in.Reason) || (in.Action != "create" && in.Action != "approve" && in.Action != "revoke" && in.Action != "query") {
		return in, apiError(400, "VALIDATION_ERROR", "Choose one action and 1-32 explicit repair targets with a reason")
	}
	in.Items = append([]ProvisionKeyRepairBatchItem(nil), in.Items...)
	sort.Slice(in.Items, func(i, j int) bool { return in.Items[i].InstallationID < in.Items[j].InstallationID })
	for i, item := range in.Items {
		if !modelIDPattern.MatchString(item.InstallationID) || item.ExpectedVersion < 1 || (i > 0 && item.InstallationID == in.Items[i-1].InstallationID) {
			return in, apiError(400, "VALIDATION_ERROR", "Unique installation IDs and exact versions required")
		}
		if in.Action == "create" {
			if item.RepairEventID < 1 || item.ConfirmSerial == "" || len(item.ConfirmSerial) > 256 || item.RepairID != "" {
				return in, apiError(400, "VALIDATION_ERROR", "Creation requires exact repair evidence and confirmed serial, with no existing repair ID")
			}
		} else if !modelIDPattern.MatchString(item.RepairID) || item.RepairEventID != 0 || item.ConfirmSerial != "" {
			return in, apiError(400, "VALIDATION_ERROR", "Actions require exact repair IDs and repair versions, without creation fields")
		}
	}
	return in, nil
}

// Explicit selection, shared single-target invariants and one transaction. A
// stale member or loss of any deadline rolls back the entire authorization.
// Workers still execute only independently approved original native tasks.
func (s *Store) RequestProvisionKeyRepairBatch(ctx context.Context, a *AuthSession, project string, in ProvisionKeyRepairBatchInput, key, trace string) (*ProvisionKeyRepairBatch, error) {
	if !s.ProvisionInstallEnabled {
		return nil, apiError(503, "PROVISION_INSTALL_DISABLED", "Installation runtime disabled")
	}
	var err error
	in, err = canonicalProvisionKeyRepairBatch(in)
	if err != nil {
		return nil, err
	}
	if len(key) < 1 || len(key) > 128 {
		return nil, apiError(400, "VALIDATION_ERROR", "Idempotency-Key required")
	}
	for _, r := range key {
		if r < 33 || r > 126 {
			return nil, apiError(400, "VALIDATION_ERROR", "Invalid Idempotency-Key")
		}
	}
	tx, err := s.beginPinnedRead(ctx, a)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = requireSSHTrustAuthority(ctx, tx, a, project, in.Action == "approve"); err != nil {
		return nil, err
	}
	idem := digest("provision.key-repair-batch:" + project + ":" + a.User.ID + ":" + key)
	hash := digest(string(provisionFirstSSHJSON(in)))
	if err = lockKey(ctx, tx, idem); err != nil {
		return nil, err
	}
	var oldHash string
	var raw []byte
	err = tx.QueryRowContext(ctx, `SELECT request_hash,response FROM idempotencies WHERE id=$1`, idem).Scan(&oldHash, &raw)
	if err == nil {
		if oldHash != hash {
			return nil, apiError(409, "IDEMPOTENCY_CONFLICT", "Batch repair input changed")
		}
		var prior ProvisionKeyRepairBatch
		if json.Unmarshal(raw, &prior) != nil {
			return nil, ErrProvisionBootDenied
		}
		return &prior, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	out := &ProvisionKeyRepairBatch{Requests: make([]ProvisionKeyRepair, 0, len(in.Items))}
	ids := make([]string, 0, len(in.Items))
	for _, item := range in.Items {
		var r *ProvisionKeyRepair
		if in.Action == "create" {
			r, err = s.requestProvisionKeyRepairTx(ctx, tx, a, project, item.InstallationID, ProvisionKeyRepairInput{ExpectedVersion: item.ExpectedVersion, RepairEventID: item.RepairEventID, ConfirmSerial: item.ConfirmSerial, Reason: in.Reason}, trace)
		} else {
			r, err = s.actProvisionKeyRepairTx(ctx, tx, a, project, item.InstallationID, item.RepairID, in.Action, ProvisionKeyRepairActionInput{ExpectedVersion: item.ExpectedVersion, Reason: in.Reason}, trace)
		}
		if err != nil {
			return nil, err
		}
		out.Requests = append(out.Requests, *r)
		ids = append(ids, item.InstallationID)
	}
	var live int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM provision_installations x JOIN exec_task_attempts a ON a.id=x.attempt_id JOIN exec_resource_leases r ON r.attempt_id=a.id AND r.resource_id='device:machine:'||x.machine_id WHERE x.project_id=$1 AND x.id=ANY($2::text[]) AND x.state='awaiting_first_boot' AND x.expires_at>clock_timestamp() AND a.state='running' AND a.lease_until>clock_timestamp() AND r.state='held' AND r.expires_at>clock_timestamp() AND r.fencing_token=x.fencing_token AND r.mode='exclusive'`, project, ids).Scan(&live); err != nil {
		return nil, err
	}
	if live != len(ids) {
		return nil, apiError(409, "PROVISION_KEY_REPAIR_LEASE_CHANGED", "Every selected original reservation must still be live")
	}
	if in.Action != "revoke" {
		repairIDs := make([]string, 0, len(out.Requests))
		for _, r := range out.Requests {
			repairIDs = append(repairIDs, r.ID)
		}
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM provision_key_repairs WHERE id=ANY($1::text[]) AND expires_at>clock_timestamp()`, repairIDs).Scan(&live); err != nil {
			return nil, err
		}
		if live != len(repairIDs) {
			return nil, apiError(409, "PROVISION_KEY_REPAIR_EXPIRED", "A selected authorization expired")
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO idempotencies(id,actor_id,request_hash,response,created_at) VALUES($1,$2,$3,$4,now())`, idem, a.User.ID, hash, provisionFirstSSHJSON(out)); err != nil {
		return nil, err
	}
	if err = appendAudit(ctx, tx, a.User.ID, project, "provision.key_repair.batch", idem, trace, map[string]any{"action": in.Action, "installation_ids": ids, "reason": in.Reason}); err != nil {
		return nil, err
	}
	return out, tx.Commit()
}
