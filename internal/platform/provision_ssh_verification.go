package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/enzoOvOyu/keel/internal/taskrunner"
)

// A new read-only probe is allowed only after a fresh pinned query proved the
// current helper command terminal failed. Unknown/missing receipts stay query-
// only. Neither the original native command nor its held lease is replaced.
type ProvisionSSHVerificationInput struct {
	ExpectedVersion int64  `json:"expected_version"`
	RepairEventID   int64  `json:"repair_event_id"`
	FailedEventID   int64  `json:"failed_event_id"`
	Reason          string `json:"reason"`
}
type ProvisionSSHVerification struct {
	ID             string `json:"id"`
	InstallationID string `json:"installation_id"`
	RepairEventID  int64  `json:"repair_event_id"`
	FailedEventID  int64  `json:"failed_event_id"`
	Status         string `json:"status"`
	Mode           string `json:"mode"`
}

type provisionSSHFailedEvidence struct {
	Command         taskrunner.TaskCommand `json:"command"`
	Observation     taskrunner.Observation `json:"observation"`
	NativeCommandID string                 `json:"native_command_id"`
	BindingSHA256   string                 `json:"binding_sha256"`
	Queried         bool                   `json:"queried"`
}

func (s *Store) RequestProvisionSSHVerification(ctx context.Context, a *AuthSession, project, id string, in ProvisionSSHVerificationInput, trace string) (*ProvisionSSHVerification, error) {
	if !s.ProvisionInstallEnabled {
		return nil, apiError(503, "PROVISION_INSTALL_DISABLED", "Installation runtime disabled")
	}
	if in.ExpectedVersion < 1 || in.RepairEventID < 1 || in.FailedEventID < 1 || !validProvisionReason(in.Reason) {
		return nil, apiError(400, "PROVISION_SSH_VERIFICATION_INVALID", "Current installation version, exact failed-query/repair evidence and reason required")
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
	x, request, scope, until, err := installationMutationTx(ctx, tx, project, id)
	if err != nil {
		return nil, err
	}
	if x.State != "awaiting_first_boot" || x.StateVersion != in.ExpectedVersion {
		return nil, ErrProvisionBootDenied
	}
	repair, err := readProvisionSSHRepairTx(ctx, tx, x)
	if err != nil {
		return nil, err
	}
	if repair == nil || repair.EventID != in.RepairEventID {
		return nil, apiError(409, "PROVISION_SSH_REPAIR_CHANGED", "Refresh the latest repair evidence")
	}
	active, err := provisionActiveFirstSSHCommandTx(ctx, tx, x)
	if err != nil {
		return nil, err
	}
	var raw []byte
	var failedID int64
	if err = tx.QueryRowContext(ctx, `SELECT id,payload FROM provision_installation_events WHERE installation_id=$1 AND event_type='first_boot.ssh_terminal_failed' ORDER BY id DESC LIMIT 1`, x.ID).Scan(&failedID, &raw); err != nil {
		return nil, ErrProvisionBootDenied
	}
	var failed provisionSSHFailedEvidence
	if failedID >= repair.EventID || failedID != in.FailedEventID || json.Unmarshal(raw, &failed) != nil || !failed.Queried || failed.NativeCommandID != *x.CommandID || failed.Command != active.Command || failed.Observation.Validate(failed.Command) != nil || failed.Observation.State != "failed" || failed.BindingSHA256 != active.Plan.BindingSHA256 {
		return nil, apiError(409, "PROVISION_SSH_KNOWN_FAILURE_REQUIRED", "A pinned query must prove the current immutable helper receipt terminal failed")
	}
	work, err := provisionFirstSSHPlanTx(ctx, tx, x, request, scope, until)
	if err != nil || work == nil {
		return nil, ErrProvisionBootDenied
	}
	if work.Plan.BindingSHA256 != failed.BindingSHA256 {
		return nil, ErrProvisionBootDenied
	}
	var prior string
	var consumed bool
	err = tx.QueryRowContext(ctx, `SELECT r.payload->>'verification_id',EXISTS(SELECT 1 FROM provision_installation_events c WHERE c.installation_id=r.installation_id AND c.event_type='first_boot.ssh_verification_consumed' AND c.payload->>'verification_id'=r.payload->>'verification_id') FROM provision_installation_events r WHERE r.installation_id=$1 AND r.event_type='first_boot.ssh_verification_requested' AND r.payload->>'failed_event_id'=$2 ORDER BY r.id DESC LIMIT 1`, x.ID, strconv.FormatInt(failedID, 10)).Scan(&prior, &consumed)
	if err == nil {
		if consumed {
			return nil, apiError(409, "PROVISION_SSH_VERIFICATION_CONSUMED", "This failed receipt already authorized a replacement probe")
		}
		return &ProvisionSSHVerification{ID: prior, InstallationID: x.ID, RepairEventID: in.RepairEventID, FailedEventID: failedID, Status: "pending", Mode: "run"}, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if repair.Status != "available" {
		return nil, apiError(409, "PROVISION_SSH_WORK_PENDING", "Await the current recovery outcome before authorizing another probe")
	}
	pending, err := provisionPendingFirstSSHWorkTx(ctx, tx, x.ID)
	if err != nil {
		return nil, err
	}
	if pending {
		return nil, apiError(409, "PROVISION_SSH_WORK_PENDING", "A read-only recovery operation is already pending")
	}
	out := &ProvisionSSHVerification{ID: RandomID(), InstallationID: x.ID, RepairEventID: in.RepairEventID, FailedEventID: failedID, Status: "pending", Mode: "run"}
	evidence := map[string]any{"verification_id": out.ID, "repair_event_id": in.RepairEventID, "failed_event_id": failedID, "native_command_id": *x.CommandID, "previous_command_id": failed.Command.CommandID, "attempt_id": *x.AttemptID, "fencing_token": *x.FencingToken, "requested_by": a.User.ID, "reason": in.Reason, "mode": "run"}
	if err = appendProvisionEventTx(ctx, tx, x, "first_boot.ssh_verification_requested", evidence); err != nil {
		return nil, err
	}
	if err = appendAudit(ctx, tx, a.User.ID, project, "provision.first_boot.ssh_verification_requested", id, trace, evidence); err != nil {
		return nil, err
	}
	return out, tx.Commit()
}

func provisionPendingFirstSSHWorkTx(ctx context.Context, tx *sql.Tx, id string) (bool, error) {
	var pending bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM provision_installation_events r WHERE r.installation_id=$1 AND ((r.event_type='first_boot.ssh_retry_requested' AND NOT EXISTS(SELECT 1 FROM provision_installation_events c WHERE c.installation_id=r.installation_id AND c.event_type='first_boot.ssh_retry_consumed' AND c.payload->>'retry_id'=r.payload->>'retry_id')) OR (r.event_type='first_boot.ssh_verification_requested' AND NOT EXISTS(SELECT 1 FROM provision_installation_events c WHERE c.installation_id=r.installation_id AND c.event_type='first_boot.ssh_verification_consumed' AND c.payload->>'verification_id'=r.payload->>'verification_id'))))`, id).Scan(&pending)
	return pending, err
}

func provisionActiveFirstSSHCommandTx(ctx context.Context, tx *sql.Tx, x *ProvisionInstallation) (*ProvisionFirstSSHWork, error) {
	if x.AttemptID == nil || x.CommandID == nil || x.FencingToken == nil {
		return nil, ErrProvisionBootDenied
	}
	var raw []byte
	if err := tx.QueryRowContext(ctx, `SELECT payload FROM provision_installation_events WHERE installation_id=$1 AND event_type IN('first_boot.ssh_started','first_boot.ssh_verification_consumed') ORDER BY id DESC LIMIT 1`, x.ID).Scan(&raw); err != nil {
		return nil, err
	}
	var work ProvisionFirstSSHWork
	if json.Unmarshal(raw, &work) != nil || work.Command.Validate() != nil || work.Command.ProjectID != x.ProjectID || work.Command.MachineID != x.MachineID || work.Command.AttemptID != *x.AttemptID || work.Command.FencingToken != *x.FencingToken || work.Command.Operation != "host.identity" || work.Command.TimeoutMS != 5000 {
		return nil, ErrProvisionBootDenied
	}
	// Replacement probe dispatches carry a separate append-only native binding;
	// the immutable installation command itself is never replaced.
	if work.Command.CommandID != *x.CommandID {
		var binding struct {
			NativeCommandID       string `json:"native_command_id"`
			VerificationID        string `json:"verification_id"`
			VerificationCommandID string `json:"verification_command_id"`
		}
		if json.Unmarshal(raw, &binding) != nil || binding.NativeCommandID != *x.CommandID || binding.VerificationCommandID != work.Command.CommandID || !modelIDPattern.MatchString(binding.VerificationID) {
			return nil, ErrProvisionBootDenied
		}
	}
	return &work, nil
}

func provisionCurrentFirstSSHWorkTx(ctx context.Context, tx *sql.Tx, x *ProvisionInstallation, request *ProvisionRequest, scope *ProvisionBootScope, until time.Time) (*ProvisionFirstSSHWork, error) {
	current, err := provisionFirstSSHPlanTx(ctx, tx, x, request, scope, until)
	if err != nil || current == nil {
		return nil, ErrProvisionBootDenied
	}
	var raw []byte
	if err = tx.QueryRowContext(ctx, `SELECT payload FROM provision_installation_events WHERE installation_id=$1 AND event_type IN('first_boot.ssh_started','first_boot.ssh_retry_consumed','first_boot.ssh_verification_consumed') ORDER BY id DESC LIMIT 1`, x.ID).Scan(&raw); err != nil {
		return nil, err
	}
	var work ProvisionFirstSSHWork
	if json.Unmarshal(raw, &work) != nil || (work.Mode != "run" && work.Mode != "query") || work.NativeCommandID != *x.CommandID {
		return nil, ErrProvisionBootDenied
	}
	active, err := provisionActiveFirstSSHCommandTx(ctx, tx, x)
	if err != nil || active.Command != work.Command || active.VerificationID != work.VerificationID {
		return nil, ErrProvisionBootDenied
	}
	current.Command = active.Command
	current.Mode = work.Mode
	current.RetryID = work.RetryID
	current.VerificationID = work.VerificationID
	current.Plan.ExpiresAt = work.Plan.ExpiresAt
	if string(provisionFirstSSHJSON(current)) != string(provisionFirstSSHJSON(work)) {
		return nil, ErrProvisionBootDenied
	}
	for _, auth := range []struct{ ID, Kind, Field string }{{work.RetryID, "first_boot.ssh_retry_requested", "retry_id"}, {work.VerificationID, "first_boot.ssh_verification_requested", "verification_id"}} {
		if auth.ID == "" {
			continue
		}
		if !modelIDPattern.MatchString(auth.ID) {
			return nil, ErrProvisionBootDenied
		}
		var actor string
		if err = tx.QueryRowContext(ctx, `SELECT payload->>'requested_by' FROM provision_installation_events WHERE installation_id=$1 AND event_type=$2 AND payload->>$3=$4 ORDER BY id DESC LIMIT 1`, x.ID, auth.Kind, auth.Field, auth.ID).Scan(&actor); err != nil {
			return nil, err
		}
		if err = validateProvisionSSHRetryActorTx(ctx, tx, x.ProjectID, actor); err != nil {
			return nil, err
		}
	}
	return &work, nil
}

func (s *Store) RecordProvisionFirstSSHFailure(ctx context.Context, project, id string, proof taskrunner.ProvisionSSHProof) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	x, request, scope, until, err := installationMutationTx(ctx, tx, project, id)
	if err != nil {
		return err
	}
	work, err := provisionCurrentFirstSSHWorkTx(ctx, tx, x, request, scope, until)
	if err != nil {
		return err
	}
	if work.Mode != "query" || proof.ValidateQueriedFailure(work.Command, work.Plan, time.Now()) != nil {
		return ErrProvisionBootDenied
	}
	var recorded bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM provision_installation_events WHERE installation_id=$1 AND event_type='first_boot.ssh_terminal_failed' AND payload->>'retry_id'=$2 AND payload->'command'->>'command_id'=$3)`, x.ID, work.RetryID, work.Command.CommandID).Scan(&recorded); err != nil {
		return err
	}
	if recorded {
		return tx.Commit()
	}
	// A query establishes the immutable failed receipt as a fact. Network loss,
	// missing records and a fabricated state flag can never enter this event.
	if err = appendProvisionEventTx(ctx, tx, x, "first_boot.ssh_terminal_failed", map[string]any{"command": work.Command, "observation": proof.Observation(), "native_command_id": *x.CommandID, "binding_sha256": work.Plan.BindingSHA256, "queried": true, "retry_id": work.RetryID, "verification_id": work.VerificationID}); err != nil {
		return err
	}
	return tx.Commit()
}

func prepareProvisionFirstSSHVerificationTx(ctx context.Context, tx *sql.Tx, x *ProvisionInstallation, request *ProvisionRequest, scope *ProvisionBootScope, until time.Time) (*ProvisionFirstSSHWork, error) {
	var raw []byte
	err := tx.QueryRowContext(ctx, `SELECT r.payload FROM provision_installation_events r WHERE r.installation_id=$1 AND r.event_type='first_boot.ssh_verification_requested' AND NOT EXISTS(SELECT 1 FROM provision_installation_events c WHERE c.installation_id=r.installation_id AND c.event_type='first_boot.ssh_verification_consumed' AND c.payload->>'verification_id'=r.payload->>'verification_id') ORDER BY r.id LIMIT 1`, x.ID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var in struct {
		ID                string `json:"verification_id"`
		RepairEventID     int64  `json:"repair_event_id"`
		FailedEventID     int64  `json:"failed_event_id"`
		NativeCommandID   string `json:"native_command_id"`
		PreviousCommandID string `json:"previous_command_id"`
		AttemptID         string `json:"attempt_id"`
		FencingToken      int64  `json:"fencing_token"`
		RequestedBy       string `json:"requested_by"`
		Mode              string `json:"mode"`
	}
	if json.Unmarshal(raw, &in) != nil || !modelIDPattern.MatchString(in.ID) || in.Mode != "run" || in.NativeCommandID != *x.CommandID || in.AttemptID != *x.AttemptID || in.FencingToken != *x.FencingToken {
		return nil, ErrProvisionBootDenied
	}
	if err = validateProvisionSSHRetryActorTx(ctx, tx, x.ProjectID, in.RequestedBy); err != nil {
		return nil, err
	}
	var repairID, failedID int64
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(max(id) FILTER(WHERE event_type='first_boot.ssh_repair_required'),0),COALESCE(max(id) FILTER(WHERE event_type='first_boot.ssh_terminal_failed'),0) FROM provision_installation_events WHERE installation_id=$1`, x.ID).Scan(&repairID, &failedID); err != nil {
		return nil, err
	}
	if failedID >= repairID || repairID != in.RepairEventID || failedID != in.FailedEventID {
		return nil, ErrProvisionBootDenied
	}
	if err = tx.QueryRowContext(ctx, `SELECT payload FROM provision_installation_events WHERE installation_id=$1 AND id=$2 AND event_type='first_boot.ssh_terminal_failed'`, x.ID, failedID).Scan(&raw); err != nil {
		return nil, err
	}
	var failed provisionSSHFailedEvidence
	active, err := provisionActiveFirstSSHCommandTx(ctx, tx, x)
	if err != nil {
		return nil, err
	}
	if json.Unmarshal(raw, &failed) != nil || !failed.Queried || failed.NativeCommandID != *x.CommandID || failed.Command != active.Command || failed.Command.CommandID != in.PreviousCommandID || failed.Observation.Validate(failed.Command) != nil || failed.Observation.State != "failed" || failed.BindingSHA256 != active.Plan.BindingSHA256 {
		return nil, ErrProvisionBootDenied
	}
	work, err := provisionFirstSSHPlanTx(ctx, tx, x, request, scope, until)
	if err != nil || work == nil {
		return nil, ErrProvisionBootDenied
	}
	work.Command.CommandID = RandomID()
	work.VerificationID = in.ID
	work.Mode = "run"
	payload := map[string]any{"native_command_id": *x.CommandID, "verification_id": in.ID, "verification_command_id": work.Command.CommandID, "previous_command_id": active.Command.CommandID, "failed_event_id": failedID, "repair_event_id": repairID, "attempt_id": *x.AttemptID, "fencing_token": *x.FencingToken, "command": work.Command, "plan": work.Plan, "report_sha256": work.ReportSHA256, "mode": "run"}
	if err = appendProvisionEventTx(ctx, tx, x, "first_boot.ssh_verification_consumed", payload); err != nil {
		return nil, err
	}
	return work, tx.Commit()
}
