package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
)

type ProvisionSSHBatchItem struct {
	InstallationID  string `json:"installation_id"`
	ExpectedVersion int64  `json:"expected_version"`
	RepairEventID   int64  `json:"repair_event_id"`
	FailedEventID   int64  `json:"failed_event_id"`
}
type ProvisionSSHBatchInput struct {
	Items  []ProvisionSSHBatchItem `json:"items"`
	Reason string                  `json:"reason"`
}
type ProvisionSSHBatch struct {
	Verifications []ProvisionSSHVerification `json:"verifications"`
}

// RequestProvisionSSHBatch authorizes only explicitly selected read-only probes
// backed by queried terminal failure. One stale/unknown member rolls back all.
// Original attempts, commands, fencing tokens and resource claims are retained.
func (s *Store) RequestProvisionSSHBatch(ctx context.Context, a *AuthSession, project string, in ProvisionSSHBatchInput, key, trace string) (*ProvisionSSHBatch, error) {
	if !s.ProvisionInstallEnabled {
		return nil, apiError(503, "PROVISION_INSTALL_DISABLED", "Installation runtime disabled")
	}
	if len(in.Items) < 1 || len(in.Items) > 32 || !validProvisionReason(in.Reason) || len(key) < 1 || len(key) > 128 {
		return nil, apiError(400, "VALIDATION_ERROR", "1-32 explicit installations, exact repair/failure evidence, reason and idempotency key required")
	}
	for _, r := range key {
		if r < 33 || r > 126 {
			return nil, apiError(400, "VALIDATION_ERROR", "Invalid idempotency key")
		}
	}
	in.Items = append([]ProvisionSSHBatchItem(nil), in.Items...)
	sort.Slice(in.Items, func(i, j int) bool { return in.Items[i].InstallationID < in.Items[j].InstallationID })
	ids := make([]string, 0, len(in.Items))
	for i, item := range in.Items {
		if !modelIDPattern.MatchString(item.InstallationID) || item.ExpectedVersion < 1 || item.RepairEventID < 1 || item.FailedEventID < 1 || (i > 0 && item.InstallationID == in.Items[i-1].InstallationID) {
			return nil, apiError(400, "VALIDATION_ERROR", "Unique installations with exact versions and repair/failure evidence required")
		}
		ids = append(ids, item.InstallationID)
	}
	tx, err := s.beginPinnedRead(ctx, a)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = requireProvisionAuthority(ctx, tx, a, project, false); err != nil {
		return nil, err
	}
	if err = validateProvisionSSHRetryActorTx(ctx, tx, project, a.User.ID); err != nil {
		return nil, err
	}
	idem := digest("provision.ssh-batch:" + project + ":" + a.User.ID + ":" + key)
	raw, _ := json.Marshal(in)
	hash := digest(string(raw))
	if err = lockKey(ctx, tx, idem); err != nil {
		return nil, err
	}
	var priorHash string
	var prior []byte
	err = tx.QueryRowContext(ctx, `SELECT request_hash,response FROM idempotencies WHERE id=$1`, idem).Scan(&priorHash, &prior)
	if err == nil {
		if priorHash != hash {
			return nil, apiError(409, "IDEMPOTENCY_CONFLICT", "Idempotency key input changed")
		}
		var out ProvisionSSHBatch
		if err = json.Unmarshal(prior, &out); err != nil {
			return nil, err
		}
		return &out, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	out := &ProvisionSSHBatch{Verifications: make([]ProvisionSSHVerification, 0, len(in.Items))}
	for _, item := range in.Items {
		result, e := requestProvisionSSHVerificationTx(ctx, tx, a, project, item.InstallationID, ProvisionSSHVerificationInput{ExpectedVersion: item.ExpectedVersion, RepairEventID: item.RepairEventID, FailedEventID: item.FailedEventID, Reason: in.Reason}, trace)
		if e != nil {
			return nil, e
		}
		out.Verifications = append(out.Verifications, *result)
	}
	// The earlier selected claims must still be live after validating the last
	// member. clock_timestamp avoids PostgreSQL's transaction-start now().
	var live int
	err = tx.QueryRowContext(ctx, `SELECT count(*) FROM provision_installations x JOIN exec_task_attempts a ON a.id=x.attempt_id JOIN exec_resource_leases r ON r.attempt_id=a.id AND r.resource_id='device:machine:'||x.machine_id
 WHERE x.project_id=$1 AND x.id=ANY($2::text[]) AND x.state='awaiting_first_boot' AND x.expires_at>clock_timestamp()
 AND a.state='running' AND a.lease_until>clock_timestamp() AND r.state='held' AND r.expires_at>clock_timestamp() AND r.fencing_token=x.fencing_token AND r.mode='exclusive'`, project, ids).Scan(&live)
	if err != nil {
		return nil, err
	}
	if live != len(ids) {
		return nil, ErrProvisionBootDenied
	}
	raw, _ = json.Marshal(out)
	if _, err = tx.ExecContext(ctx, `INSERT INTO idempotencies(id,actor_id,request_hash,response,created_at) VALUES($1,$2,$3,$4,now())`, idem, a.User.ID, hash, raw); err != nil {
		return nil, err
	}
	return out, tx.Commit()
}
