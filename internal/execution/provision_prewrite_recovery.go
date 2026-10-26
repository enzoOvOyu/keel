package execution

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/enzoOvOyu/keel/internal/platform"
)

// ReconcileProvisionBeforeWrite closes only a quarantined original attempt for
// which the server never issued a write permit. Missing device receipts after
// any permit remain unknown. The authority, no-permit proof, native receipt and
// resource release are one transaction; no expired lease is revived.
func (s *Service) ReconcileProvisionBeforeWrite(ctx context.Context, a *Actor, project, id string, in platform.ProvisionRequestActionInput, key, trace string) (*Execution, error) {
	if s.ProvisionStore == nil || !s.ProvisionStore.ProvisionInstallEnabled {
		return nil, failure(503, "PROVISION_INSTALL_DISABLED", "Installation runtime disabled")
	}
	if !opaqueID.MatchString(id) || in.ExpectedVersion < 1 || len(in.Reason) > 500 || strings.TrimSpace(in.Reason) == "" || strings.ContainsAny(in.Reason, "\x00\r\n") {
		return nil, invalid("Exact installation version and bounded reason required")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = authorize(ctx, tx, a, project, "Admin", "DL", "SRE"); err != nil {
		return nil, err
	}
	idem, prior, err := idempotency(ctx, tx, a, "provision.prewrite-reconcile:"+project+":"+id, key, in)
	if err != nil {
		return nil, err
	}
	if prior != nil {
		out, e := decode[Execution](prior)
		return &out, e
	}
	var execution string
	if err = tx.QueryRowContext(ctx, `SELECT execution_id FROM provision_installations WHERE project_id=$1 AND id=$2`, project, id).Scan(&execution); err == sql.ErrNoRows {
		return nil, platform.ErrNotFound
	} else if err != nil {
		return nil, err
	}
	var mode, state string
	if err = tx.QueryRowContext(ctx, `SELECT mode,state FROM exec_executions WHERE id=$1 AND project_id=$2 FOR UPDATE`, execution, project).Scan(&mode, &state); err != nil {
		return nil, err
	}
	if mode != "provision" || state != "reconciling" {
		return nil, conflict("Only the original quarantined installation execution may be closed")
	}
	// Lock the native attempt before the installation, matching live permit and
	// completion order. Any racing permit issuance must finish before this proof.
	var attemptID sql.NullString
	if err = tx.QueryRowContext(ctx, `SELECT attempt_id FROM provision_installations WHERE id=$1`, id).Scan(&attemptID); err != nil {
		return nil, err
	}
	if !attemptID.Valid {
		return nil, conflict("Only a previously running quarantined attempt may be closed")
	}
	attempt, err := scanAttempt(tx.QueryRowContext(ctx, "SELECT "+attemptColumns+" FROM exec_task_attempts WHERE id=$1 AND execution_id=$2 FOR UPDATE", attemptID.String, execution))
	if err != nil {
		return nil, err
	}
	var locked string
	if err = tx.QueryRowContext(ctx, `SELECT id FROM provision_installations WHERE id=$1 AND project_id=$2 FOR UPDATE`, id, project).Scan(&locked); err != nil {
		return nil, err
	}
	x, err := platform.LoadProvisionInstallationBindingTx(ctx, tx, project, id)
	if err != nil {
		return nil, err
	}
	if x.State != "unknown" || x.StateVersion != in.ExpectedVersion || x.PermitIssuedAt != nil || x.AttemptID == nil || *x.AttemptID != attempt.ID || x.CommandID == nil || *x.CommandID != attempt.CommandID || x.FencingToken == nil || *x.FencingToken != attempt.FencingToken || attempt.State != "unknown" || attempt.TaskID != "provision-install" || attempt.StartedAt == nil {
		return nil, conflict("An unchanged unknown installation that never issued a write permit is required")
	}
	var issued bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM provision_installation_events WHERE installation_id=$1 AND event_type='write.permit_issued')`, id).Scan(&issued); err != nil {
		return nil, err
	}
	if issued {
		return nil, conflict("A write permit exists; missing target outcome cannot release resources")
	}
	var resource string
	if err = tx.QueryRowContext(ctx, `SELECT resource_id FROM exec_resource_leases WHERE resource_id=$1 AND attempt_id=$2 AND fencing_token=$3 AND mode='exclusive' AND state='reconciling' FOR UPDATE`, "device:machine:"+x.MachineID, attempt.ID, attempt.FencingToken).Scan(&resource); err == sql.ErrNoRows {
		return nil, conflict("The original exclusive quarantine claim is required")
	} else if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	const reason = "operator_prewrite_reconciled"
	if _, err = tx.ExecContext(ctx, `UPDATE provision_installations SET state='failed',state_version=state_version+1,failure_reason=$2,updated_at=$3 WHERE id=$1`, id, reason, now); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO provision_installation_events(installation_id,event_type,payload,created_at) VALUES($1,'installation.stopped_before_write',$2,$3)`, id, jsonBytes(map[string]any{"state": "failed", "reason": reason, "installation_performed": false, "write_permit_never_issued": true, "requested_by": a.User.ID, "attempt_id": attempt.ID, "command_id": attempt.CommandID, "fencing_token": attempt.FencingToken}), now); err != nil {
		return nil, err
	}
	receipt := Receipt{CommandID: attempt.CommandID, AttemptID: attempt.ID, FencingToken: attempt.FencingToken, Result: "failed", StartedAt: *attempt.StartedAt, FinishedAt: now, Output: map[string]any{"schema": "keel.provision-installation-proof/v1", "installation_id": x.ID, "request_id": x.RequestID, "snapshot_sha256": x.SnapshotSHA256, "reason": reason, "installation_performed": false}}
	raw := jsonBytes(receipt)
	if _, err = tx.ExecContext(ctx, `INSERT INTO exec_runner_receipts(command_id,attempt_id,fencing_token,result,payload,digest,observed_at) VALUES($1,$2,$3,'failed',$4,$5,$6)`, receipt.CommandID, receipt.AttemptID, receipt.FencingToken, raw, hash(raw), now); err != nil {
		return nil, err
	}
	if err = s.applyProvisionReceipt(ctx, tx, attempt, receipt); err != nil {
		return nil, err
	}
	if err = applyReceipt(ctx, tx, attempt, receipt, false); err != nil {
		return nil, err
	}
	if err = settle(ctx, tx, execution); err != nil {
		return nil, err
	}
	if err = audit(ctx, tx, a.User.ID, project, "provision.prewrite.reconciled", id, trace, map[string]any{"reason": in.Reason, "execution_id": execution, "attempt_id": attempt.ID, "command_id": attempt.CommandID, "fencing_token": attempt.FencingToken, "write_permit_never_issued": true}); err != nil {
		return nil, err
	}
	out, err := scanExecution(tx.QueryRowContext(ctx, "SELECT "+executionColumns+" FROM exec_executions WHERE id=$1", execution))
	if err != nil {
		return nil, err
	}
	if err = remember(ctx, tx, idem, in, &out); err != nil {
		return nil, err
	}
	return &out, tx.Commit()
}
