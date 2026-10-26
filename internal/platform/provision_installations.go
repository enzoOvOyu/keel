package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/enzoOvOyu/keel/internal/provisionboot"
	"github.com/enzoOvOyu/keel/internal/provisionpreflight"
	"github.com/enzoOvOyu/keel/internal/provisionstorage"
)

var ErrProvisionBootDenied = apiError(409, "PROVISION_BOOT_DENIED", "Installation boot identity or current authority unavailable")

// LockProvisionMachineTx shares the exact advisory-key encoding with source
// request admission. The execution package uses a different general hash.
func LockProvisionMachineTx(ctx context.Context, tx *sql.Tx, machine string) error {
	if !modelIDPattern.MatchString(machine) {
		return ErrProvisionBootDenied
	}
	return lockKey(ctx, tx, "provision.machine:"+machine)
}

type ProvisionInstallation struct {
	ID                 string                     `json:"id"`
	ProjectID          string                     `json:"project_id"`
	RequestID          string                     `json:"request_id"`
	MachineID          string                     `json:"machine_id"`
	ExecutionID        string                     `json:"execution_id"`
	ScopeID            string                     `json:"scope_id"`
	ScopeSHA256        string                     `json:"scope_sha256"`
	SnapshotSHA256     string                     `json:"snapshot_sha256"`
	GrantID            string                     `json:"grant_id"`
	CreatedBy          string                     `json:"created_by"`
	Reason             string                     `json:"reason"`
	CreatedAt          time.Time                  `json:"created_at"`
	ExpiresAt          time.Time                  `json:"expires_at"`
	State              string                     `json:"state"`
	StateVersion       int64                      `json:"state_version"`
	AttemptID          *string                    `json:"attempt_id,omitempty"`
	CommandID          *string                    `json:"command_id,omitempty"`
	FencingToken       *int64                     `json:"fencing_token,omitempty"`
	Spec               *provisionpreflight.Spec   `json:"spec,omitempty"`
	BootID             *string                    `json:"boot_id,omitempty"`
	BootPublicKey      []byte                     `json:"-"`
	CallbackSequence   int64                      `json:"callback_sequence"`
	LatestReport       []byte                     `json:"-"`
	Preflight          *provisionpreflight.Report `json:"preflight,omitempty"`
	LatestReportSHA256 *string                    `json:"latest_report_sha256,omitempty"`
	StoragePlan        *provisionstorage.Plan     `json:"storage_plan,omitempty"`
	StoragePlanSHA256  *string                    `json:"storage_plan_sha256,omitempty"`
	StorageRequestedBy *string                    `json:"storage_requested_by,omitempty"`
	StorageRequestedAt *time.Time                 `json:"storage_requested_at,omitempty"`
	StorageApprovedBy  *string                    `json:"storage_approved_by,omitempty"`
	StorageApprovedAt  *time.Time                 `json:"storage_approved_at,omitempty"`
	PermitIssuedAt     *time.Time                 `json:"permit_issued_at,omitempty"`
	FailureReason      *string                    `json:"failure_reason,omitempty"`
	UpdatedAt          time.Time                  `json:"updated_at"`
}

const provisionInstallationColumns = `id,project_id,request_id,machine_id,execution_id,scope_id,scope_sha256,snapshot_sha256,grant_id,created_by,reason,created_at,expires_at,state,state_version,attempt_id,command_id,fencing_token,spec,boot_id,boot_public_key,callback_sequence,latest_report,latest_report_sha256,storage_plan,storage_plan_sha256,storage_requested_by,storage_requested_at,storage_approved_by,storage_approved_at,permit_issued_at,failure_reason,updated_at`

func scanProvisionInstallation(row interface{ Scan(...any) error }) (*ProvisionInstallation, error) {
	x := &ProvisionInstallation{}
	var spec, plan []byte
	err := row.Scan(&x.ID, &x.ProjectID, &x.RequestID, &x.MachineID, &x.ExecutionID, &x.ScopeID, &x.ScopeSHA256, &x.SnapshotSHA256, &x.GrantID, &x.CreatedBy, &x.Reason, &x.CreatedAt, &x.ExpiresAt, &x.State, &x.StateVersion, &x.AttemptID, &x.CommandID, &x.FencingToken, &spec, &x.BootID, &x.BootPublicKey, &x.CallbackSequence, &x.LatestReport, &x.LatestReportSHA256, &plan, &x.StoragePlanSHA256, &x.StorageRequestedBy, &x.StorageRequestedAt, &x.StorageApprovedBy, &x.StorageApprovedAt, &x.PermitIssuedAt, &x.FailureReason, &x.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if spec != nil {
		x.Spec = &provisionpreflight.Spec{}
		if json.Unmarshal(spec, x.Spec) != nil {
			return nil, ErrProvisionBootDenied
		}
	}
	if plan != nil {
		x.StoragePlan = &provisionstorage.Plan{}
		if json.Unmarshal(plan, x.StoragePlan) != nil {
			return nil, ErrProvisionBootDenied
		}
	}
	if len(x.LatestReport) > 0 && (x.LatestReportSHA256 == nil || digest(string(x.LatestReport)) != *x.LatestReportSHA256) {
		return nil, ErrProvisionBootDenied
	}
	if len(x.LatestReport) > 0 {
		x.Preflight = &provisionpreflight.Report{}
		if json.Unmarshal(x.LatestReport, x.Preflight) != nil {
			return nil, ErrProvisionBootDenied
		}
	}
	return x, nil
}
func LoadProvisionInstallationTx(ctx context.Context, tx *sql.Tx, project, id string) (*ProvisionInstallation, error) {
	return scanProvisionInstallation(tx.QueryRowContext(ctx, "SELECT "+provisionInstallationColumns+" FROM provision_installations WHERE id=$1 AND project_id=$2", id, project))
}

const provisionInstallationBindingColumns = `id,project_id,request_id,machine_id,execution_id,scope_id,scope_sha256,snapshot_sha256,grant_id,created_by,expires_at,state,attempt_id,command_id,fencing_token,spec,permit_issued_at`

func scanProvisionInstallationBinding(row interface{ Scan(...any) error }) (*ProvisionInstallation, error) {
	x := &ProvisionInstallation{}
	var spec []byte
	err := row.Scan(&x.ID, &x.ProjectID, &x.RequestID, &x.MachineID, &x.ExecutionID, &x.ScopeID, &x.ScopeSHA256, &x.SnapshotSHA256, &x.GrantID, &x.CreatedBy, &x.ExpiresAt, &x.State, &x.AttemptID, &x.CommandID, &x.FencingToken, &spec, &x.PermitIssuedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if spec != nil {
		x.Spec = &provisionpreflight.Spec{}
		if json.Unmarshal(spec, x.Spec) != nil {
			return nil, ErrProvisionBootDenied
		}
	}
	return x, nil
}

// LoadProvisionInstallationBindingTx omits potentially multi-MiB historical
// reports and storage plans from worker/transport liveness checks. It is an
// internal binding view, never an API installation representation.
func LoadProvisionInstallationBindingTx(ctx context.Context, tx *sql.Tx, project, id string) (*ProvisionInstallation, error) {
	return scanProvisionInstallationBinding(tx.QueryRowContext(ctx, "SELECT "+provisionInstallationBindingColumns+" FROM provision_installations WHERE project_id=$1 AND id=$2", project, id))
}
func appendProvisionEventTx(ctx context.Context, tx *sql.Tx, x *ProvisionInstallation, kind string, payload map[string]any) error {
	raw, err := json.Marshal(payload)
	if err != nil || len(raw) > 12<<10 {
		return ErrProvisionBootDenied
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO provision_installation_events(installation_id,event_type,payload,created_at)VALUES($1,$2,$3,now())`, x.ID, kind, raw)
	return err
}

// ValidateProvisionInstallationTx pins the current running attempt and shared
// machine resource before any installation row mutation. Execution is locked
// first, matching completion/control/reaping; a join's row-mark order alone
// cannot guarantee that ordering.
func ValidateProvisionInstallationTx(ctx context.Context, tx *sql.Tx, x *ProvisionInstallation, now time.Time) (*ProvisionRequest, *ProvisionBootScope, time.Time, error) {
	if x == nil || x.AttemptID == nil || x.CommandID == nil || x.FencingToken == nil || x.Spec == nil || !x.ExpiresAt.After(now) || x.Spec.Validate(now) != nil {
		return nil, nil, time.Time{}, ErrProvisionBootDenied
	}
	var deadline time.Time
	var actorID string
	var executionID string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM exec_executions WHERE id=$1 AND project_id=$2 FOR SHARE`, x.ExecutionID, x.ProjectID).Scan(&executionID); err != nil {
		return nil, nil, time.Time{}, err
	}
	err := tx.QueryRowContext(ctx, `SELECT LEAST(a.lease_until,r.expires_at),e.created_by FROM exec_task_attempts a JOIN exec_executions e ON e.id=a.execution_id JOIN exec_resource_leases r ON r.attempt_id=a.id WHERE a.id=$1 AND a.execution_id=$2 AND a.command_id=$3 AND a.fencing_token=$4 AND a.runner='provision' AND a.task_id='provision-install' AND a.state='running' AND a.lease_until>$6 AND e.project_id=$5 AND e.mode='provision' AND e.desired_state='running' AND e.state IN('dispatching','running') AND r.resource_id=$7 AND r.mode='exclusive' AND r.state='held' AND r.fencing_token=a.fencing_token AND r.expires_at>$6 FOR SHARE OF a,e,r`, *x.AttemptID, x.ExecutionID, *x.CommandID, *x.FencingToken, x.ProjectID, now, "device:machine:"+x.MachineID).Scan(&deadline, &actorID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			err = ErrProvisionBootDenied
		}
		return nil, nil, time.Time{}, err
	}
	if !deadline.After(time.Now()) || !x.ExpiresAt.After(time.Now()) {
		return nil, nil, time.Time{}, ErrProvisionBootDenied
	}
	if actorID != x.CreatedBy {
		return nil, nil, time.Time{}, ErrProvisionBootDenied
	}
	var enabled, admin bool
	if err = tx.QueryRowContext(ctx, "SELECT enabled,is_admin FROM users WHERE id=$1 FOR SHARE", actorID).Scan(&enabled, &admin); err != nil {
		return nil, nil, time.Time{}, err
	}
	if !enabled {
		return nil, nil, time.Time{}, ErrProvisionBootDenied
	}
	actor := &AuthSession{User: UserSummary{ID: actorID, IsAdmin: admin}}
	if err = requireProvisionAuthority(ctx, tx, actor, x.ProjectID, false); err != nil {
		return nil, nil, time.Time{}, ErrProvisionBootDenied
	}
	request, err := LoadProvisionRequestTx(ctx, tx, x.ProjectID, x.RequestID)
	if err != nil {
		return nil, nil, time.Time{}, err
	}
	if request.State != "prepared" || request.MachineID != x.MachineID || request.SnapshotSHA256 != x.SnapshotSHA256 || !ProvisionSnapshotValid(request) {
		return nil, nil, time.Time{}, ErrProvisionBootDenied
	}
	if err = validateProvisionInstallationMetadataTx(ctx, tx, x, request, now); err != nil {
		return nil, nil, time.Time{}, err
	}
	if x.Spec.ProjectID != x.ProjectID || x.Spec.RequestID != x.RequestID || x.Spec.MachineID != x.MachineID || x.Spec.AttemptID != *x.AttemptID || x.Spec.CommandID != *x.CommandID || x.Spec.FencingToken != *x.FencingToken || x.Spec.SnapshotSHA256 != x.SnapshotSHA256 || !x.Spec.ExpiresAt.Equal(x.ExpiresAt) || x.Spec.Expected.Serial != request.Snapshot.Target.Serial || x.Spec.Expected.BootMAC != request.Snapshot.BootPort.MAC || x.Spec.Expected.Arch != request.Snapshot.Target.Arch {
		return nil, nil, time.Time{}, ErrProvisionBootDenied
	}
	scope, err := LoadApprovedProvisionScopeTx(ctx, tx, x.ProjectID, x.ScopeID)
	if err != nil || scope.ScopeSHA256 != x.ScopeSHA256 {
		if err == nil {
			err = ErrProvisionBootDenied
		}
		return nil, nil, time.Time{}, err
	}
	return request, scope, deadline, nil
}

func installationMutationTx(ctx context.Context, tx *sql.Tx, project, id string) (*ProvisionInstallation, *ProvisionRequest, *ProvisionBootScope, time.Time, error) {
	x, err := LoadProvisionInstallationTx(ctx, tx, project, id)
	if err != nil {
		return nil, nil, nil, time.Time{}, err
	}
	request, scope, until, err := ValidateProvisionInstallationTx(ctx, tx, x, time.Now())
	if err != nil {
		return nil, nil, nil, time.Time{}, err
	}
	locked, err := scanProvisionInstallation(tx.QueryRowContext(ctx, "SELECT "+provisionInstallationColumns+" FROM provision_installations WHERE id=$1 AND project_id=$2 FOR UPDATE", id, project))
	if err != nil {
		return nil, nil, nil, time.Time{}, err
	}
	if locked.AttemptID == nil || *locked.AttemptID != *x.AttemptID || locked.FencingToken == nil || *locked.FencingToken != *x.FencingToken {
		return nil, nil, nil, time.Time{}, ErrProvisionBootDenied
	}
	return locked, request, scope, until, nil
}

func (s *Store) ReadProvisionInstallation(ctx context.Context, a *AuthSession, project, id string) (*ProvisionInstallation, error) {
	tx, err := s.beginPinnedRead(ctx, a)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = requireProvisionReadAuthority(ctx, tx, a, project, false); err != nil {
		return nil, err
	}
	x, err := LoadProvisionInstallationTx(ctx, tx, project, id)
	if err != nil {
		return nil, err
	}
	return x, tx.Commit()
}
func (s *Store) ListProvisionInstallations(ctx context.Context, a *AuthSession, project string, p Pagination) (Page[ProvisionInstallation], error) {
	out := NewPage[ProvisionInstallation](p)
	if err := p.Validate(); err != nil {
		return out, err
	}
	tx, err := s.beginPinnedRead(ctx, a)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if err = requireProvisionReadAuthority(ctx, tx, a, project, false); err != nil {
		return out, err
	}
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM provision_installations WHERE project_id=$1", project).Scan(&out.Total); err != nil {
		return out, err
	}
	// Polling the board must not repeatedly load multi-MiB hardware reports.
	// The scoped detail endpoint supplies the exact evidence for an explicit review.
	columns := strings.Replace(provisionInstallationColumns, "latest_report,", "NULL::bytea AS latest_report,", 1)
	rows, err := tx.QueryContext(ctx, "SELECT "+columns+" FROM provision_installations WHERE project_id=$1 ORDER BY created_at DESC,id LIMIT $2 OFFSET $3", project, p.PageSize, p.Offset())
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		x, e := scanProvisionInstallation(rows)
		if e != nil {
			return out, e
		}
		out.Items = append(out.Items, *x)
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	return out, tx.Commit()
}

type SelectProvisionStorageInput struct {
	ExpectedVersion int64                   `json:"expected_version"`
	Intent          provisionstorage.Intent `json:"intent"`
	Reason          string                  `json:"reason"`
}

func validProvisionReason(value string) bool {
	return strings.TrimSpace(value) != "" && len(value) <= 500 && !strings.ContainsAny(value, "\x00\r\n")
}

func (s *Store) SelectProvisionStorage(ctx context.Context, a *AuthSession, project, id string, in SelectProvisionStorageInput, trace string) (*ProvisionInstallation, error) {
	if !s.ProvisionInstallEnabled {
		return nil, apiError(503, "PROVISION_INSTALL_DISABLED", "Installation runtime disabled")
	}
	if in.ExpectedVersion < 1 || in.Intent.Validate() != nil || !validProvisionReason(in.Reason) {
		return nil, apiError(400, "PROVISION_STORAGE_INVALID", "Explicit target disk/layout, erase consent, exact version and reason required")
	}
	tx, err := s.beginPinnedRead(ctx, a)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = requireProvisionAuthority(ctx, tx, a, project, false); err != nil {
		return nil, err
	}
	x, _, _, _, err := installationMutationTx(ctx, tx, project, id)
	if err != nil {
		return nil, err
	}
	if x.State != "preflight_ready" || x.StateVersion != in.ExpectedVersion || x.StoragePlan != nil {
		return nil, apiError(409, "PROVISION_STORAGE_STATE_CHANGED", "Fresh authenticated preflight and exact current version required")
	}
	pending, err := pendingProvisionRefreshTx(ctx, tx, x)
	if err != nil {
		return nil, err
	}
	if pending != "" {
		return nil, apiError(409, "PROVISION_PREFLIGHT_REFRESH_PENDING", "Wait for the requested signed fresh report before disk selection")
	}
	plan, err := provisionstorage.Build(*x.Spec, x.LatestReport, in.Intent, time.Now())
	if err != nil {
		return nil, apiError(409, "PROVISION_STORAGE_FACTS_REQUIRED", "Selected disk requires fresh matching verified preflight facts; refresh in the boot environment")
	}
	raw, err := json.Marshal(plan)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if _, err = tx.ExecContext(ctx, `UPDATE provision_installations SET state='awaiting_storage_review',state_version=state_version+1,storage_plan=$2,storage_plan_sha256=$3,storage_requested_by=$4,storage_requested_at=$5,updated_at=$5 WHERE id=$1`, id, raw, plan.SHA256, a.User.ID, now); err != nil {
		return nil, err
	}
	if err = appendAudit(ctx, tx, a.User.ID, project, "provision.storage.selected", id, trace, map[string]any{"storage_plan_sha256": plan.SHA256, "reason": in.Reason}); err != nil {
		return nil, err
	}
	if err = appendProvisionEventTx(ctx, tx, x, "storage.selected", map[string]any{"storage_plan_sha256": plan.SHA256}); err != nil {
		return nil, err
	}
	x, err = LoadProvisionInstallationTx(ctx, tx, project, id)
	if err != nil {
		return nil, err
	}
	return x, tx.Commit()
}
func (s *Store) ApproveProvisionStorage(ctx context.Context, a *AuthSession, project, id string, in ProvisionRequestActionInput, trace string) (*ProvisionInstallation, error) {
	if !s.ProvisionInstallEnabled {
		return nil, apiError(503, "PROVISION_INSTALL_DISABLED", "Installation runtime disabled")
	}
	if in.ExpectedVersion < 1 || !validProvisionReason(in.Reason) {
		return nil, apiError(400, "PROVISION_STORAGE_INVALID", "Exact version and approval reason required")
	}
	tx, err := s.beginPinnedRead(ctx, a)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = requireProvisionAuthority(ctx, tx, a, project, true); err != nil {
		return nil, err
	}
	x, _, _, _, err := installationMutationTx(ctx, tx, project, id)
	if err != nil {
		return nil, err
	}
	if x.State != "awaiting_storage_review" || x.StateVersion != in.ExpectedVersion || x.StorageRequestedBy == nil || *x.StorageRequestedBy == a.User.ID || x.CreatedBy == a.User.ID || x.StoragePlan == nil || x.StoragePlanSHA256 == nil || x.StoragePlan.SHA256 != *x.StoragePlanSHA256 || x.StoragePlan.Validate(time.Now()) != nil {
		return nil, apiError(409, "PROVISION_INDEPENDENT_APPROVAL_REQUIRED", "Another current Admin/DL must approve the exact unexpired immutable disk plan")
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if _, err = tx.ExecContext(ctx, `UPDATE provision_installations SET state='storage_approved',state_version=state_version+1,storage_approved_by=$2,storage_approved_at=$3,updated_at=$3 WHERE id=$1`, id, a.User.ID, now); err != nil {
		return nil, err
	}
	if err = appendAudit(ctx, tx, a.User.ID, project, "provision.storage.approved", id, trace, map[string]any{"storage_plan_sha256": *x.StoragePlanSHA256, "reason": in.Reason}); err != nil {
		return nil, err
	}
	if err = appendProvisionEventTx(ctx, tx, x, "storage.approved", map[string]any{"storage_plan_sha256": *x.StoragePlanSHA256}); err != nil {
		return nil, err
	}
	x, err = LoadProvisionInstallationTx(ctx, tx, project, id)
	if err != nil {
		return nil, err
	}
	return x, tx.Commit()
}

func (x *ProvisionInstallation) BootGrant() (provisionboot.Grant, error) {
	if x.Spec == nil || x.AttemptID == nil || x.CommandID == nil || x.FencingToken == nil {
		return provisionboot.Grant{}, ErrProvisionBootDenied
	}
	return provisionboot.Grant{ScopeID: x.ScopeID, ID: x.GrantID, ProjectID: x.ProjectID, RequestID: x.RequestID, MachineID: x.MachineID, AttemptID: *x.AttemptID, CommandID: *x.CommandID, FencingToken: *x.FencingToken, SnapshotSHA256: x.SnapshotSHA256, MAC: x.Spec.Expected.BootMAC, Arch: x.Spec.Expected.Arch, ExpiresAt: x.ExpiresAt}, nil
}
