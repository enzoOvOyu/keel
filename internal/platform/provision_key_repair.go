package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/enzoOvOyu/keel/internal/taskrunner"
)

type ProvisionKeyRepairInput struct {
	ExpectedVersion int64  `json:"expected_version"`
	RepairEventID   int64  `json:"repair_event_id"`
	ConfirmSerial   string `json:"confirm_serial"`
	Reason          string `json:"reason"`
}
type ProvisionKeyRepairActionInput struct {
	ExpectedVersion int64  `json:"expected_version"`
	Reason          string `json:"reason"`
}
type ProvisionKeyRepairSnapshot struct {
	InstallationVersion int64                         `json:"installation_version"`
	RepairEventID       int64                         `json:"repair_event_id"`
	NativeCommandID     string                        `json:"native_command_id"`
	AttemptID           string                        `json:"attempt_id"`
	FencingToken        int64                         `json:"fencing_token"`
	ReportSHA256        string                        `json:"report_sha256"`
	BindingSHA256       string                        `json:"binding_sha256"`
	HostKey             string                        `json:"host_key"`
	Serial              string                        `json:"serial_number"`
	Credential          taskrunner.InstallCredential  `json:"credential"`
	Bootstrap           taskrunner.ProvisionBootstrap `json:"bootstrap"`
	Reason              string                        `json:"reason"`
}
type ProvisionKeyRepair struct {
	ID             string                     `json:"id"`
	InstallationID string                     `json:"installation_id"`
	ProjectID      string                     `json:"project_id"`
	Snapshot       ProvisionKeyRepairSnapshot `json:"snapshot"`
	CreatedBy      string                     `json:"created_by"`
	ApprovedBy     string                     `json:"approved_by,omitempty"`
	CreatedAt      time.Time                  `json:"created_at"`
	ExpiresAt      time.Time                  `json:"expires_at"`
	State          string                     `json:"state"`
	StateVersion   int64                      `json:"state_version"`
}

const provisionKeyRepairColumns = `id,installation_id,project_id,snapshot,created_by,COALESCE(approved_by,''),created_at,expires_at,state,state_version`

func scanProvisionKeyRepair(row sshTrustScanner) (*ProvisionKeyRepair, error) {
	out := &ProvisionKeyRepair{}
	var raw []byte
	err := row.Scan(&out.ID, &out.InstallationID, &out.ProjectID, &raw, &out.CreatedBy, &out.ApprovedBy, &out.CreatedAt, &out.ExpiresAt, &out.State, &out.StateVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if json.Unmarshal(raw, &out.Snapshot) != nil {
		return nil, ErrProvisionBootDenied
	}
	return out, nil
}
func readProvisionKeyRepairTx(ctx context.Context, tx *sql.Tx, x *ProvisionInstallation) (*ProvisionKeyRepair, error) {
	out, err := scanProvisionKeyRepair(tx.QueryRowContext(ctx, "SELECT "+provisionKeyRepairColumns+" FROM provision_key_repairs WHERE project_id=$1 AND installation_id=$2 ORDER BY created_at DESC,id DESC LIMIT 1", x.ProjectID, x.ID))
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	return out, err
}
func validateProvisionKeyRepairActorsTx(ctx context.Context, tx *sql.Tx, r *ProvisionKeyRepair, approved bool) error {
	if !r.ExpiresAt.After(time.Now()) {
		return apiError(409, "PROVISION_KEY_REPAIR_EXPIRED", "Repair approval expired; retain the original reservation")
	}
	if err := validateProvisionSSHRetryActorTx(ctx, tx, r.ProjectID, r.CreatedBy); err != nil {
		return err
	}
	if approved {
		if r.ApprovedBy == "" || r.ApprovedBy == r.CreatedBy {
			return ErrForbidden
		}
		var enabled, admin bool
		if err := tx.QueryRowContext(ctx, `SELECT enabled,is_admin FROM users WHERE id=$1 FOR SHARE`, r.ApprovedBy).Scan(&enabled, &admin); err != nil {
			return err
		}
		if !enabled {
			return ErrForbidden
		}
		if !admin {
			var role string
			if err := tx.QueryRowContext(ctx, `SELECT role FROM memberships WHERE project_id=$1 AND user_id=$2 FOR SHARE`, r.ProjectID, r.ApprovedBy).Scan(&role); err != nil {
				return err
			}
			if role != "Admin" && role != "DL" {
				return ErrForbidden
			}
		}
	}
	return nil
}
func (s *Store) validateProvisionKeyRepairTx(ctx context.Context, tx *sql.Tx, x *ProvisionInstallation, request *ProvisionRequest, scope *ProvisionBootScope, until time.Time, r *ProvisionKeyRepair, approved bool) (*ProvisionFirstSSHWork, error) {
	if s.SSHRunner == nil || x.State != "awaiting_first_boot" || x.ID != r.InstallationID || x.ProjectID != r.ProjectID || x.StateVersion != r.Snapshot.InstallationVersion || x.AttemptID == nil || *x.AttemptID != r.Snapshot.AttemptID || x.CommandID == nil || *x.CommandID != r.Snapshot.NativeCommandID || x.FencingToken == nil || *x.FencingToken != r.Snapshot.FencingToken {
		return nil, ErrProvisionBootDenied
	}
	if err := validateProvisionKeyRepairActorsTx(ctx, tx, r, approved); err != nil {
		return nil, err
	}
	work, err := provisionFirstSSHPlanTx(ctx, tx, x, request, scope, until)
	if err != nil || work == nil {
		return nil, ErrProvisionBootDenied
	}
	if work.ReportSHA256 != r.Snapshot.ReportSHA256 || work.Plan.BindingSHA256 != r.Snapshot.BindingSHA256 || work.Plan.HostKey != r.Snapshot.HostKey || work.Plan.Credential != r.Snapshot.Credential || request.Snapshot.Target.Serial != r.Snapshot.Serial {
		return nil, ErrProvisionBootDenied
	}
	if r.ExpiresAt.Before(work.Plan.ExpiresAt) {
		work.Plan.ExpiresAt = r.ExpiresAt
	}
	bootstrap, err := s.SSHRunner.ProvisionRepairBootstrap(x.ProjectID, x.MachineID, work.Plan)
	if err != nil || string(provisionFirstSSHJSON(bootstrap)) != string(provisionFirstSSHJSON(r.Snapshot.Bootstrap)) {
		return nil, apiError(409, "PROVISION_KEY_REPAIR_BINDING_CHANGED", "Explicit temporary SSH binding must match the authenticated new host and frozen approval")
	}
	return work, nil
}

// Request creates only an independently approvable intent. It cannot start SSH,
// select another domain key, acquire another claim or change the OS request.
func (s *Store) RequestProvisionKeyRepair(ctx context.Context, a *AuthSession, project, id string, in ProvisionKeyRepairInput, key, trace string) (*ProvisionKeyRepair, error) {
	if !s.ProvisionInstallEnabled {
		return nil, apiError(503, "PROVISION_INSTALL_DISABLED", "Installation runtime disabled")
	}
	if in.ExpectedVersion < 1 || in.RepairEventID < 1 || !validProvisionReason(in.Reason) || !modelIDPattern.MatchString(id) {
		return nil, apiError(400, "VALIDATION_ERROR", "Current installation, repair evidence, serial and reason required")
	}
	tx, err := s.beginPinnedRead(ctx, a)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = requireSSHTrustAuthority(ctx, tx, a, project, false); err != nil {
		return nil, err
	}
	idem, hash, prior, err := provisionKeyRepairIdempotency(ctx, tx, a, project+":"+id+":create", key, in)
	if err != nil {
		return nil, err
	}
	if prior != nil {
		return prior, tx.Commit()
	}
	r, err := s.requestProvisionKeyRepairTx(ctx, tx, a, project, id, in, trace)
	if err != nil {
		return nil, err
	}
	if err = saveProvisionKeyRepairIdempotency(ctx, tx, a, idem, hash, r); err != nil {
		return nil, err
	}
	return r, tx.Commit()
}

func (s *Store) requestProvisionKeyRepairTx(ctx context.Context, tx *sql.Tx, a *AuthSession, project, id string, in ProvisionKeyRepairInput, trace string) (*ProvisionKeyRepair, error) {
	x, request, scope, until, err := installationMutationTx(ctx, tx, project, id)
	if err != nil {
		return nil, err
	}
	repair, err := readProvisionSSHRepairTx(ctx, tx, x)
	if err != nil {
		return nil, err
	}
	if x.State != "awaiting_first_boot" || x.StateVersion != in.ExpectedVersion || repair == nil || repair.EventID != in.RepairEventID || repair.Status != "available" || in.ConfirmSerial != request.Snapshot.Target.Serial {
		return nil, ErrProvisionBootDenied
	}
	pending, err := provisionPendingFirstSSHWorkTx(ctx, tx, x.ID)
	if err != nil {
		return nil, err
	}
	if pending {
		return nil, apiError(409, "PROVISION_SSH_WORK_PENDING", "Await the current read-only recovery outcome")
	}
	var active bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM provision_key_repairs WHERE installation_id=$1 AND state IN('pending','approved','consumed','unknown'))`, x.ID).Scan(&active); err != nil {
		return nil, err
	}
	if active {
		return nil, apiError(409, "PROVISION_KEY_REPAIR_ACTIVE", "Resolve or revoke the existing approval; unknown mutations remain query-only")
	}
	work, err := provisionFirstSSHPlanTx(ctx, tx, x, request, scope, until)
	if err != nil || work == nil || s.SSHRunner == nil {
		return nil, ErrProvisionBootDenied
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	expiry := now.Add(5 * time.Minute)
	for _, limit := range []time.Time{x.ExpiresAt, work.Plan.Credential.ExpiresAt} {
		if limit.Before(expiry) {
			expiry = limit
		}
	}
	// Approval lifetime is not a lease. Actual held-claim deadlines are checked
	// again before dispatch and commit; no renewal occurs in this operation.
	bootstrap, err := s.SSHRunner.ProvisionRepairBootstrap(project, x.MachineID, work.Plan)
	if err != nil || bootstrap.Credential.ExpiresAt == nil {
		return nil, apiError(409, "PROVISION_KEY_REPAIR_BOOTSTRAP_REQUIRED", "Configure an explicit temporary credential pinned to the signed first-boot host")
	}
	if bootstrap.Credential.ExpiresAt.Before(expiry) {
		expiry = *bootstrap.Credential.ExpiresAt
	}
	if !expiry.After(now.Add(15 * time.Second)) {
		return nil, ErrProvisionBootDenied
	}
	r := &ProvisionKeyRepair{ID: RandomID(), InstallationID: x.ID, ProjectID: project, CreatedBy: a.User.ID, CreatedAt: now, ExpiresAt: expiry, State: "pending", StateVersion: 1, Snapshot: ProvisionKeyRepairSnapshot{InstallationVersion: x.StateVersion, RepairEventID: in.RepairEventID, NativeCommandID: *x.CommandID, AttemptID: *x.AttemptID, FencingToken: *x.FencingToken, ReportSHA256: work.ReportSHA256, BindingSHA256: work.Plan.BindingSHA256, HostKey: work.Plan.HostKey, Serial: in.ConfirmSerial, Credential: work.Plan.Credential, Bootstrap: bootstrap, Reason: in.Reason}}
	if _, err = s.validateProvisionKeyRepairTx(ctx, tx, x, request, scope, until, r, false); err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO provision_key_repairs(id,installation_id,project_id,snapshot,created_by,created_at,expires_at,state) VALUES($1,$2,$3,$4,$5,$6,$7,'pending')`, r.ID, x.ID, project, provisionFirstSSHJSON(r.Snapshot), r.CreatedBy, now, expiry)
	if err != nil {
		return nil, err
	}
	if err = appendAudit(ctx, tx, a.User.ID, project, "provision.key_repair.requested", r.ID, trace, map[string]any{"installation_id": id, "snapshot": r.Snapshot, "expires_at": expiry}); err != nil {
		return nil, err
	}
	return r, nil
}

func provisionKeyRepairIdempotency(ctx context.Context, tx *sql.Tx, a *AuthSession, scope, key string, input any) (string, string, *ProvisionKeyRepair, error) {
	if len(key) < 1 || len(key) > 128 {
		return "", "", nil, apiError(400, "VALIDATION_ERROR", "Idempotency-Key required")
	}
	for _, r := range key {
		if r < 33 || r > 126 {
			return "", "", nil, apiError(400, "VALIDATION_ERROR", "Invalid Idempotency-Key")
		}
	}
	idem := digest("provision.key-repair:" + scope + ":" + a.User.ID + ":" + key)
	hash := digest(string(provisionFirstSSHJSON(input)))
	if err := lockKey(ctx, tx, idem); err != nil {
		return "", "", nil, err
	}
	var oldHash string
	var raw []byte
	err := tx.QueryRowContext(ctx, `SELECT request_hash,response FROM idempotencies WHERE id=$1`, idem).Scan(&oldHash, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return idem, hash, nil, nil
	}
	if err != nil {
		return "", "", nil, err
	}
	if hash != oldHash {
		return "", "", nil, apiError(409, "IDEMPOTENCY_CONFLICT", "Repair authorization input changed")
	}
	var prior ProvisionKeyRepair
	if json.Unmarshal(raw, &prior) != nil {
		return "", "", nil, ErrProvisionBootDenied
	}
	return idem, hash, &prior, nil
}
func saveProvisionKeyRepairIdempotency(ctx context.Context, tx *sql.Tx, a *AuthSession, idem, hash string, r *ProvisionKeyRepair) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO idempotencies(id,actor_id,request_hash,response,created_at) VALUES($1,$2,$3,$4,now())`, idem, a.User.ID, hash, provisionFirstSSHJSON(r))
	return err
}

func (s *Store) ActProvisionKeyRepair(ctx context.Context, a *AuthSession, project, installation, id, action string, in ProvisionKeyRepairActionInput, key, trace string) (*ProvisionKeyRepair, error) {
	if !s.ProvisionInstallEnabled {
		return nil, apiError(503, "PROVISION_INSTALL_DISABLED", "Installation runtime disabled")
	}
	if !modelIDPattern.MatchString(id) || in.ExpectedVersion < 1 || !validProvisionReason(in.Reason) || (action != "approve" && action != "revoke" && action != "query") {
		return nil, apiError(400, "VALIDATION_ERROR", "Exact repair version, action and reason required")
	}
	tx, err := s.beginPinnedRead(ctx, a)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = requireSSHTrustAuthority(ctx, tx, a, project, action == "approve"); err != nil {
		return nil, err
	}
	idem, hash, prior, err := provisionKeyRepairIdempotency(ctx, tx, a, project+":"+installation+":"+id+":"+action, key, in)
	if err != nil {
		return nil, err
	}
	if prior != nil {
		return prior, tx.Commit()
	}
	r, err := s.actProvisionKeyRepairTx(ctx, tx, a, project, installation, id, action, in, trace)
	if err != nil {
		return nil, err
	}
	if err = saveProvisionKeyRepairIdempotency(ctx, tx, a, idem, hash, r); err != nil {
		return nil, err
	}
	return r, tx.Commit()
}

func (s *Store) actProvisionKeyRepairTx(ctx context.Context, tx *sql.Tx, a *AuthSession, project, installation, id, action string, in ProvisionKeyRepairActionInput, trace string) (*ProvisionKeyRepair, error) {
	// Keep original installation mutation/claim lock ordering before request locks.
	x, request, scope, until, err := installationMutationTx(ctx, tx, project, installation)
	if err != nil {
		return nil, err
	}
	r, err := scanProvisionKeyRepair(tx.QueryRowContext(ctx, "SELECT "+provisionKeyRepairColumns+" FROM provision_key_repairs WHERE project_id=$1 AND installation_id=$2 AND id=$3 FOR UPDATE", project, installation, id))
	if err != nil {
		return nil, err
	}
	if r.StateVersion != in.ExpectedVersion {
		return nil, errVersion
	}
	switch action {
	case "approve":
		if r.State != "pending" || r.CreatedBy == a.User.ID {
			return nil, apiError(403, "PROVISION_KEY_REPAIR_INDEPENDENT_APPROVAL", "Another authorized person must approve a pending repair")
		}
		r.ApprovedBy = a.User.ID
		if _, err = s.validateProvisionKeyRepairTx(ctx, tx, x, request, scope, until, r, true); err != nil {
			return nil, err
		}
		r.State = "approved"
	case "revoke":
		if r.State != "pending" && r.State != "approved" {
			return nil, apiError(409, "PROVISION_KEY_REPAIR_QUERY_ONLY", "Consumed or unknown mutations cannot be revoked or replaced; query the original receipt")
		}
		r.State = "revoked"
	case "query":
		if r.State != "unknown" && r.State != "consumed" {
			return nil, apiError(409, "PROVISION_KEY_REPAIR_QUERY_ONLY", "Only an uncertain original mutation can be queried")
		}
		if _, err = s.validateProvisionKeyRepairTx(ctx, tx, x, request, scope, until, r, true); err != nil {
			return nil, err
		}
		if r.State == "consumed" {
			var raw []byte
			if err = tx.QueryRowContext(ctx, `SELECT payload FROM provision_installation_events WHERE installation_id=$1 AND event_type='first_boot.key_repair_consumed' AND payload->>'repair_id'=$2 ORDER BY id DESC LIMIT 1`, installation, id).Scan(&raw); err != nil {
				return nil, err
			}
			var current ProvisionKeyRepairWork
			if json.Unmarshal(raw, &current) != nil {
				return nil, ErrProvisionBootDenied
			}
			if current.Plan.ExpiresAt.After(time.Now()) {
				return nil, apiError(409, "PROVISION_KEY_REPAIR_IN_FLIGHT", "Wait for the dispatched operation deadline before querying an orphan outcome")
			}
		}
		var pending bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM provision_installation_events q WHERE q.installation_id=$1 AND q.event_type='first_boot.key_repair_query_requested' AND q.payload->>'repair_id'=$2 AND NOT EXISTS(SELECT 1 FROM provision_installation_events c WHERE c.installation_id=q.installation_id AND c.event_type='first_boot.key_repair_consumed' AND c.payload->>'query_id'=q.payload->>'query_id'))`, installation, id).Scan(&pending); err != nil {
			return nil, err
		}
		if pending {
			return nil, apiError(409, "PROVISION_KEY_REPAIR_QUERY_PENDING", "Await the explicitly authorized query")
		}
		// Consumed with no outcome may be an orphan dispatch after a crash. A query
		// must reuse its command; it never makes a new installation authorization.
		if err = appendProvisionEventTx(ctx, tx, x, "first_boot.key_repair_query_requested", map[string]any{"repair_id": r.ID, "query_id": RandomID(), "requested_by": a.User.ID, "reason": in.Reason}); err != nil {
			return nil, err
		}
		// Query authorization is append-only; keep version unchanged until worker
		// consumes it. Durable actor/project idempotency still binds this request.
	}
	if action != "query" {
		if _, err = tx.ExecContext(ctx, `UPDATE provision_key_repairs SET state=$2,approved_by=NULLIF($3,''),state_version=state_version+1 WHERE id=$1`, r.ID, r.State, r.ApprovedBy); err != nil {
			return nil, err
		}
		r.StateVersion++
	}
	if err = appendAudit(ctx, tx, a.User.ID, project, "provision.key_repair."+action, r.ID, trace, map[string]any{"installation_id": installation, "reason": in.Reason, "state": r.State, "version": r.StateVersion}); err != nil {
		return nil, err
	}
	return r, nil
}

type ProvisionKeyRepairWork struct {
	RepairID  string                        `json:"repair_id"`
	QueryID   string                        `json:"query_id,omitempty"`
	Mode      string                        `json:"mode"`
	Command   taskrunner.TaskCommand        `json:"command"`
	Plan      taskrunner.ProvisionSSHPlan   `json:"plan"`
	Bootstrap taskrunner.ProvisionBootstrap `json:"bootstrap"`
}

func (s *Store) PrepareProvisionKeyRepair(ctx context.Context, project, id, attempt, command string, fence int64) (*ProvisionKeyRepairWork, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	x, request, scope, until, err := installationMutationTx(ctx, tx, project, id)
	if err != nil {
		return nil, err
	}
	if x.AttemptID == nil || *x.AttemptID != attempt || x.CommandID == nil || *x.CommandID != command || x.FencingToken == nil || *x.FencingToken != fence {
		return nil, ErrProvisionBootDenied
	}
	r, err := scanProvisionKeyRepair(tx.QueryRowContext(ctx, "SELECT "+provisionKeyRepairColumns+" FROM provision_key_repairs WHERE installation_id=$1 AND state IN('approved','consumed','unknown') ORDER BY created_at,id LIMIT 1 FOR UPDATE", id))
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var queryID, actor string
	mode := "run"
	if r.State != "approved" {
		mode = "query"
		err = tx.QueryRowContext(ctx, `SELECT q.payload->>'query_id',q.payload->>'requested_by' FROM provision_installation_events q WHERE q.installation_id=$1 AND q.event_type='first_boot.key_repair_query_requested' AND q.payload->>'repair_id'=$2 AND NOT EXISTS(SELECT 1 FROM provision_installation_events c WHERE c.installation_id=q.installation_id AND c.event_type='first_boot.key_repair_consumed' AND c.payload->>'query_id'=q.payload->>'query_id') ORDER BY q.id LIMIT 1`, id, r.ID).Scan(&queryID, &actor)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		if !modelIDPattern.MatchString(queryID) {
			return nil, ErrProvisionBootDenied
		}
		if err = validateProvisionSSHRetryActorTx(ctx, tx, project, actor); err != nil {
			return nil, err
		}
	}
	first, err := s.validateProvisionKeyRepairTx(ctx, tx, x, request, scope, until, r, true)
	if err != nil {
		return nil, err
	}
	work := &ProvisionKeyRepairWork{RepairID: r.ID, QueryID: queryID, Mode: mode, Plan: first.Plan, Bootstrap: r.Snapshot.Bootstrap}
	if mode == "run" {
		work.Command = taskrunner.TaskCommand{APIVersion: taskrunner.APIVersion, ProjectID: project, MachineID: x.MachineID, CommandID: RandomID(), AttemptID: attempt, FencingToken: fence, Operation: "trust.install", TimeoutMS: 5000, PublicKey: r.Snapshot.Credential.PublicKey, AuthorizationExpiresAt: r.ExpiresAt.UnixMicro()}
	} else {
		var raw []byte
		if err = tx.QueryRowContext(ctx, `SELECT payload FROM provision_installation_events WHERE installation_id=$1 AND event_type='first_boot.key_repair_consumed' AND payload->>'repair_id'=$2 AND payload->>'mode'='run' ORDER BY id LIMIT 1`, id, r.ID).Scan(&raw); err != nil {
			return nil, err
		}
		var original ProvisionKeyRepairWork
		if json.Unmarshal(raw, &original) != nil || original.Command.Validate() != nil {
			return nil, ErrProvisionBootDenied
		}
		work.Command = original.Command
	}
	if work.Command.Validate() != nil || work.Command.Operation != "trust.install" || work.Command.ProjectID != project || work.Command.MachineID != x.MachineID || work.Command.CommandID == command || work.Command.AttemptID != attempt || work.Command.FencingToken != fence || work.Command.PublicKey != r.Snapshot.Credential.PublicKey || work.Command.AuthorizationExpiresAt != r.ExpiresAt.UnixMicro() {
		return nil, ErrProvisionBootDenied
	}
	if !until.After(time.Now()) || !work.Plan.ExpiresAt.After(time.Now()) {
		return nil, ErrProvisionBootDenied
	}
	if err = appendProvisionEventTx(ctx, tx, x, "first_boot.key_repair_consumed", work); err != nil {
		return nil, err
	}
	if r.State != "consumed" {
		if _, err = tx.ExecContext(ctx, `UPDATE provision_key_repairs SET state='consumed',state_version=state_version+1 WHERE id=$1`, r.ID); err != nil {
			return nil, err
		}
	}
	return work, tx.Commit()
}

func (s *Store) FinishProvisionKeyRepair(ctx context.Context, project, id string, work ProvisionKeyRepairWork, proof *taskrunner.ProvisionKeyRepairProof) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	x, request, scope, until, err := installationMutationTx(ctx, tx, project, id)
	if err != nil {
		return err
	}
	r, err := scanProvisionKeyRepair(tx.QueryRowContext(ctx, "SELECT "+provisionKeyRepairColumns+" FROM provision_key_repairs WHERE project_id=$1 AND installation_id=$2 AND id=$3 FOR UPDATE", project, id, work.RepairID))
	if err != nil {
		return err
	}
	if r.State != "consumed" {
		return ErrProvisionBootDenied
	}
	var raw []byte
	if err = tx.QueryRowContext(ctx, `SELECT payload FROM provision_installation_events WHERE installation_id=$1 AND event_type='first_boot.key_repair_consumed' AND payload->>'repair_id'=$2 ORDER BY id DESC LIMIT 1`, id, r.ID).Scan(&raw); err != nil {
		return err
	}
	var consumed ProvisionKeyRepairWork
	if json.Unmarshal(raw, &consumed) != nil || string(provisionFirstSSHJSON(consumed)) != string(provisionFirstSSHJSON(work)) {
		return ErrProvisionBootDenied
	}
	state := "unknown"
	if proof != nil {
		current, e := s.validateProvisionKeyRepairTx(ctx, tx, x, request, scope, until, r, true)
		if e != nil {
			return e
		}
		current.Plan.ExpiresAt = work.Plan.ExpiresAt
		if string(provisionFirstSSHJSON(current.Plan)) != string(provisionFirstSSHJSON(work.Plan)) || !until.After(time.Now()) || proof.Validate(work.Command, work.Plan, time.Now()) != nil {
			return ErrProvisionBootDenied
		}
		state = "succeeded"
		if err = appendProvisionEventTx(ctx, tx, x, "first_boot.key_repair_verified", map[string]any{"repair_id": r.ID, "command": work.Command, "observation": proof.Observation(), "binding_sha256": work.Plan.BindingSHA256, "verified_credential_id": work.Plan.Credential.ID, "new_key_handshake": true}); err != nil {
			return err
		}
		// A separate explicitly approved read-only identity probe must still prove
		// hostname/OS/arch through the existing first-SSH finalization path.
		if err = appendProvisionEventTx(ctx, tx, x, "first_boot.ssh_verification_requested", map[string]any{"verification_id": r.ID, "key_repair_id": r.ID, "repair_event_id": r.Snapshot.RepairEventID, "native_command_id": *x.CommandID, "attempt_id": *x.AttemptID, "fencing_token": *x.FencingToken, "requested_by": r.CreatedBy, "mode": "run"}); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE provision_key_repairs SET state=$2,state_version=state_version+1 WHERE id=$1`, r.ID, state); err != nil {
		return err
	}
	if err = appendProvisionEventTx(ctx, tx, x, "first_boot.key_repair_result", map[string]any{"repair_id": r.ID, "query_id": work.QueryID, "state": state, "command_id": work.Command.CommandID, "resource_retained": true}); err != nil {
		return err
	}
	if err = appendAudit(ctx, tx, r.CreatedBy, project, "provision.key_repair.result", r.ID, "worker", map[string]any{"installation_id": id, "state": state, "command_id": work.Command.CommandID, "new_key_verified": state == "succeeded"}); err != nil {
		return err
	}
	return tx.Commit()
}

func validateProvisionRepairedProofTx(ctx context.Context, tx *sql.Tx, x *ProvisionInstallation, id string) (*ProvisionKeyRepair, error) {
	r, err := scanProvisionKeyRepair(tx.QueryRowContext(ctx, "SELECT "+provisionKeyRepairColumns+" FROM provision_key_repairs WHERE project_id=$1 AND installation_id=$2 AND id=$3 FOR SHARE", x.ProjectID, x.ID, id))
	if err != nil {
		return nil, err
	}
	if r.State != "succeeded" || x.AttemptID == nil || r.Snapshot.AttemptID != *x.AttemptID || x.CommandID == nil || r.Snapshot.NativeCommandID != *x.CommandID || x.FencingToken == nil || r.Snapshot.FencingToken != *x.FencingToken || x.StateVersion != r.Snapshot.InstallationVersion {
		return nil, ErrProvisionBootDenied
	}
	if err = validateProvisionKeyRepairActorsTx(ctx, tx, r, true); err != nil {
		return nil, err
	}
	var raw []byte
	if err = tx.QueryRowContext(ctx, `SELECT payload FROM provision_installation_events WHERE installation_id=$1 AND event_type='first_boot.key_repair_verified' AND payload->>'repair_id'=$2 ORDER BY id LIMIT 1`, x.ID, id).Scan(&raw); err != nil {
		return nil, err
	}
	var verified struct {
		Command         taskrunner.TaskCommand `json:"command"`
		Observation     taskrunner.Observation `json:"observation"`
		BindingSHA256   string                 `json:"binding_sha256"`
		CredentialID    string                 `json:"verified_credential_id"`
		NewKeyHandshake bool                   `json:"new_key_handshake"`
	}
	if json.Unmarshal(raw, &verified) != nil || !verified.NewKeyHandshake || verified.CredentialID != r.Snapshot.Credential.ID || verified.BindingSHA256 != r.Snapshot.BindingSHA256 || verified.Command.Validate() != nil || verified.Command.Operation != "trust.install" || verified.Command.ProjectID != x.ProjectID || verified.Command.MachineID != x.MachineID || verified.Command.AttemptID != *x.AttemptID || verified.Command.FencingToken != *x.FencingToken || verified.Command.PublicKey != r.Snapshot.Credential.PublicKey || verified.Command.AuthorizationExpiresAt != r.ExpiresAt.UnixMicro() || verified.Observation.Validate(verified.Command) != nil || verified.Observation.State != "success" || verified.Observation.Output["key_installed"] != true {
		return nil, ErrProvisionBootDenied
	}
	return r, nil
}
func (s *Store) prepareProvisionRepairedIdentityTx(ctx context.Context, tx *sql.Tx, x *ProvisionInstallation, request *ProvisionRequest, scope *ProvisionBootScope, until time.Time, id string) (*ProvisionFirstSSHWork, error) {
	r, err := validateProvisionRepairedProofTx(ctx, tx, x, id)
	if err != nil {
		return nil, err
	}
	work, err := s.validateProvisionKeyRepairTx(ctx, tx, x, request, scope, until, r, true)
	if err != nil {
		return nil, err
	}
	work.Command.CommandID = RandomID()
	work.VerificationID = r.ID
	work.Mode = "run"
	if err = appendProvisionEventTx(ctx, tx, x, "first_boot.ssh_verification_consumed", map[string]any{"native_command_id": *x.CommandID, "verification_id": r.ID, "key_repair_id": r.ID, "verification_command_id": work.Command.CommandID, "attempt_id": *x.AttemptID, "fencing_token": *x.FencingToken, "command": work.Command, "plan": work.Plan, "report_sha256": work.ReportSHA256, "mode": "run"}); err != nil {
		return nil, err
	}
	return work, tx.Commit()
}
