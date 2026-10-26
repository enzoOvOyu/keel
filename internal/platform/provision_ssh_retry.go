package platform

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"
)

type ProvisionSSHRetryInput struct {
	ExpectedVersion int64  `json:"expected_version"`
	RepairEventID   int64  `json:"repair_event_id"`
	Reason          string `json:"reason"`
}
type ProvisionSSHRetry struct {
	ID             string `json:"id"`
	InstallationID string `json:"installation_id"`
	RepairEventID  int64  `json:"repair_event_id"`
	Status         string `json:"status"`
	Mode           string `json:"mode"`
}
type ProvisionSSHRepair struct {
	EventID int64     `json:"event_id"`
	Reason  string    `json:"reason"`
	At      time.Time `json:"at"`
	Status  string    `json:"status"`
	RetryID string    `json:"retry_id,omitempty"`
}

func readProvisionSSHRepairTx(ctx context.Context, tx *sql.Tx, x *ProvisionInstallation) (*ProvisionSSHRepair, error) {
	out := &ProvisionSSHRepair{Status: "available"}
	err := tx.QueryRowContext(ctx, `SELECT id,COALESCE(payload->>'reason','verification_unknown'),created_at FROM provision_installation_events WHERE installation_id=$1 AND event_type='first_boot.ssh_repair_required' ORDER BY id DESC LIMIT 1`, x.ID).Scan(&out.EventID, &out.Reason, &out.At)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	switch out.Reason {
	case "runner_unavailable", "verification_unknown", "finalization_rejected":
	default:
		out.Reason = "verification_unknown"
	}
	var consumed bool
	err = tx.QueryRowContext(ctx, `SELECT r.payload->>'retry_id',EXISTS(SELECT 1 FROM provision_installation_events c WHERE c.installation_id=r.installation_id AND c.event_type='first_boot.ssh_retry_consumed' AND c.payload->>'retry_id'=r.payload->>'retry_id') FROM provision_installation_events r WHERE r.installation_id=$1 AND r.event_type='first_boot.ssh_retry_requested' AND r.payload->>'repair_event_id'=$2 ORDER BY r.id DESC LIMIT 1`, x.ID, strconv.FormatInt(out.EventID, 10)).Scan(&out.RetryID, &consumed)
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	if !modelIDPattern.MatchString(out.RetryID) {
		return nil, ErrProvisionBootDenied
	}
	out.Status = "pending"
	if consumed {
		out.Status = "consumed"
	}
	return out, nil
}

// This queues one read-only query of the original immutable helper receipt.
// It never creates a new command, renews a lease, reinstalls or retries run.
func (s *Store) RequestProvisionSSHRetry(ctx context.Context, a *AuthSession, project, id string, in ProvisionSSHRetryInput, trace string) (*ProvisionSSHRetry, error) {
	if !s.ProvisionInstallEnabled {
		return nil, apiError(503, "PROVISION_INSTALL_DISABLED", "Installation runtime disabled")
	}
	if in.ExpectedVersion < 1 || in.RepairEventID < 1 || !validProvisionReason(in.Reason) {
		return nil, apiError(400, "PROVISION_SSH_RETRY_INVALID", "Current version, repair event and reason required")
	}
	tx, err := s.beginPinnedRead(ctx, a)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = requireProvisionAuthority(ctx, tx, a, project, false); err != nil {
		return nil, err
	}
	if !a.User.IsAdmin {
		var role string
		if err = tx.QueryRowContext(ctx, "SELECT role FROM memberships WHERE project_id=$1 AND user_id=$2 FOR SHARE", project, a.User.ID).Scan(&role); err != nil {
			return nil, err
		}
		if role != "Admin" && role != "DL" && role != "SRE" {
			return nil, ErrForbidden
		}
	}
	x, request, scope, until, err := installationMutationTx(ctx, tx, project, id)
	if err != nil {
		return nil, err
	}
	if x.State != "awaiting_first_boot" || x.StateVersion != in.ExpectedVersion || x.AttemptID == nil || x.CommandID == nil || x.FencingToken == nil {
		return nil, apiError(409, "PROVISION_SSH_RETRY_STATE_CHANGED", "A current first-boot repair and live native attempt are required")
	}
	repair, err := readProvisionSSHRepairTx(ctx, tx, x)
	if err != nil {
		return nil, err
	}
	if repair == nil || repair.EventID != in.RepairEventID {
		return nil, apiError(409, "PROVISION_SSH_REPAIR_CHANGED", "Refresh the latest SSH repair evidence")
	}
	out := &ProvisionSSHRetry{ID: repair.RetryID, InstallationID: x.ID, RepairEventID: repair.EventID, Status: repair.Status, Mode: "query"}
	if repair.Status == "pending" {
		return out, tx.Commit()
	}
	if repair.Status == "consumed" {
		return nil, apiError(409, "PROVISION_SSH_QUERY_CONSUMED", "This repair was already consumed; await its outcome before requesting another query")
	}
	// Current domain credentials, pinned report, original binding and approved
	// sources must still validate. The worker repeats these checks before query.
	work, err := provisionFirstSSHPlanTx(ctx, tx, x, request, scope, until)
	if err != nil {
		return nil, err
	}
	if work == nil {
		return nil, apiError(409, "PROVISION_SSH_REPAIR_UNAVAILABLE", "Pinned SSH credentials and first-boot evidence require reconciliation")
	}
	var started bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM provision_installation_events WHERE installation_id=$1 AND event_type='first_boot.ssh_started')`, x.ID).Scan(&started); err != nil {
		return nil, err
	}
	if !started {
		return nil, apiError(409, "PROVISION_SSH_QUERY_NO_ORIGINAL", "No original immutable SSH command exists to query")
	}
	out.ID = RandomID()
	out.Status = "pending"
	evidence := map[string]any{"retry_id": out.ID, "repair_event_id": repair.EventID, "attempt_id": *x.AttemptID, "command_id": *x.CommandID, "fencing_token": *x.FencingToken, "requested_by": a.User.ID, "reason": in.Reason, "mode": "query"}
	if err = appendProvisionEventTx(ctx, tx, x, "first_boot.ssh_retry_requested", evidence); err != nil {
		return nil, err
	}
	if err = appendAudit(ctx, tx, a.User.ID, project, "provision.first_boot.ssh_retry_requested", id, trace, evidence); err != nil {
		return nil, err
	}
	return out, tx.Commit()
}
