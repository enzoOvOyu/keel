package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

type ProvisionRequest struct {
	ID             string           `json:"id"`
	ProjectID      string           `json:"project_id"`
	MachineID      string           `json:"machine_id"`
	AssetID        string           `json:"asset_id"`
	CreatedBy      string           `json:"created_by"`
	Reason         string           `json:"reason"`
	Reinstall      bool             `json:"reinstall"`
	Snapshot       ProvisionPreview `json:"snapshot"`
	SnapshotSHA256 string           `json:"snapshot_sha256"`
	State          string           `json:"state"`
	StateVersion   int64            `json:"state_version"`
	ApprovedBy     *string          `json:"approved_by,omitempty"`
	ApprovedAt     *time.Time       `json:"approved_at,omitempty"`
	ExecutionID    *string          `json:"execution_id,omitempty"`
	CreatedAt      time.Time        `json:"created_at"`
	ExpiresAt      time.Time        `json:"expires_at"`
	UpdatedAt      time.Time        `json:"updated_at"`
}
type CreateProvisionRequestInput struct {
	ProvisionPreviewInput
	Reason        string `json:"reason"`
	Reinstall     bool   `json:"reinstall"`
	ConfirmSerial string `json:"confirm_serial"`
}
type ProvisionRequestActionInput struct {
	ExpectedVersion int64  `json:"expected_version"`
	Reason          string `json:"reason"`
}

const provisionRequestColumns = `id,project_id,machine_id,asset_id,created_by,reason,reinstall,snapshot,snapshot_sha256,state,state_version,approved_by,approved_at,execution_id,created_at,expires_at,updated_at`

func scanProvisionRequest(row interface{ Scan(...any) error }) (*ProvisionRequest, error) {
	x := &ProvisionRequest{}
	var raw []byte
	err := row.Scan(&x.ID, &x.ProjectID, &x.MachineID, &x.AssetID, &x.CreatedBy, &x.Reason, &x.Reinstall, &raw, &x.SnapshotSHA256, &x.State, &x.StateVersion, &x.ApprovedBy, &x.ApprovedAt, &x.ExecutionID, &x.CreatedAt, &x.ExpiresAt, &x.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(raw, &x.Snapshot); err != nil {
		return nil, err
	}
	return x, nil
}
func requireProvisionAuthority(ctx context.Context, tx *sql.Tx, a *AuthSession, project string, approve bool) error {
	if err := requireProvisionReadAuthority(ctx, tx, a, project, approve); err != nil {
		return err
	}
	var state string
	if err := tx.QueryRowContext(ctx, "SELECT state FROM projects WHERE id=$1 FOR SHARE", project).Scan(&state); err != nil {
		return err
	}
	if state == "archived" || state == "delivered" {
		return apiError(409, "PROVISION_PROJECT_CLOSED", "Installation preparation requires an open project")
	}
	return nil
}
func requireProvisionReadAuthority(ctx context.Context, tx *sql.Tx, a *AuthSession, project string, approve bool) error {
	if err := requireProjectMember(ctx, tx, a, project); err != nil {
		return err
	}
	if a.User.IsAdmin {
		return nil
	}
	var role string
	if err := tx.QueryRowContext(ctx, "SELECT role FROM memberships WHERE project_id=$1 AND user_id=$2 FOR SHARE", project, a.User.ID).Scan(&role); err != nil {
		return err
	}
	if role == "Admin" || role == "DL" || (!approve && (role == "DE" || role == "SRE")) {
		return nil
	}
	return ErrForbidden
}
func provisionMutation(ctx context.Context, tx *sql.Tx, a *AuthSession, scope, key string, input any) (string, string, *ProvisionRequest, error) {
	if len(key) < 1 || len(key) > 128 {
		return "", "", nil, apiError(400, "VALIDATION_ERROR", "Idempotency-Key required")
	}
	for _, r := range key {
		if r < 33 || r > 126 {
			return "", "", nil, apiError(400, "VALIDATION_ERROR", "Invalid idempotency key")
		}
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return "", "", nil, err
	}
	hash := digest(string(raw))
	id := digest("provision:" + scope + ":" + a.User.ID + ":" + key)
	if err = lockKey(ctx, tx, id); err != nil {
		return "", "", nil, err
	}
	var priorHash string
	var prior []byte
	err = tx.QueryRowContext(ctx, "SELECT request_hash,response FROM idempotencies WHERE id=$1", id).Scan(&priorHash, &prior)
	if errors.Is(err, sql.ErrNoRows) {
		return id, hash, nil, nil
	}
	if err != nil {
		return "", "", nil, err
	}
	if priorHash != hash {
		return "", "", nil, apiError(409, "IDEMPOTENCY_CONFLICT", "Key belongs to a different request")
	}
	var x ProvisionRequest
	err = json.Unmarshal(prior, &x)
	return id, hash, &x, err
}
func saveProvisionMutation(ctx context.Context, tx *sql.Tx, a *AuthSession, id, hash string, x *ProvisionRequest) error {
	raw, err := json.Marshal(x)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO idempotencies(id,actor_id,request_hash,response,created_at)VALUES($1,$2,$3,$4,now())", id, a.User.ID, hash, raw)
	return err
}
func (s *Store) CreateProvisionRequest(ctx context.Context, a *AuthSession, project *ProjectSummary, asset string, in CreateProvisionRequestInput, key, trace string) (out *ProvisionRequest, resultErr error) {
	in.Reason = strings.TrimSpace(in.Reason)
	if project == nil || !modelIDPattern.MatchString(asset) || in.Reason == "" || utf8.RuneCountInString(in.Reason) > 500 {
		return nil, apiError(400, "VALIDATION_ERROR", "Exact asset and bounded reason required")
	}
	tx, err := s.beginPinnedRead(ctx, a)
	if err != nil {
		return nil, err
	}
	defer func() { tx.Rollback(); resultErr = s.RecordMediaIntegrityFailure(ctx, resultErr, a.User.ID, trace) }()
	if err = requireProvisionAuthority(ctx, tx, a, project.ID, false); err != nil {
		return nil, err
	}
	scope := project.ID + ":" + asset
	idem, hash, prior, err := provisionMutation(ctx, tx, a, scope, key, in)
	if err != nil {
		return nil, err
	}
	if prior != nil {
		return prior, nil
	}
	target, err := provisionTargetTx(ctx, tx, a, project, asset)
	if err != nil {
		return nil, err
	}
	if err = lockKey(ctx, tx, "provision.machine:"+target.MachineID); err != nil {
		return nil, err
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	// No running request is expired automatically: its command may still need reconciliation.
	expired, err := tx.QueryContext(ctx, "UPDATE provision_requests SET state='expired',state_version=state_version+1,updated_at=$2 WHERE machine_id=$1 AND state IN('requested','approved','prepared') AND expires_at<=$2 RETURNING id,project_id", target.MachineID, now)
	if err != nil {
		return nil, err
	}
	type expiredRequest struct{ id, project string }
	expiredIDs := []expiredRequest{}
	for expired.Next() {
		var expiredRequest expiredRequest
		if err = expired.Scan(&expiredRequest.id, &expiredRequest.project); err != nil {
			expired.Close()
			return nil, err
		}
		expiredIDs = append(expiredIDs, expiredRequest)
	}
	err = expired.Err()
	expired.Close()
	if err != nil {
		return nil, err
	}
	for _, expired := range expiredIDs {
		if err = appendAudit(ctx, tx, "", expired.project, "provision.request.expired", expired.id, trace, map[string]any{"source": "passive_expiry"}); err != nil {
			return nil, err
		}
	}
	var active bool
	if err = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM provision_requests WHERE machine_id=$1 AND state IN('requested','approved','preparing','prepared'))", target.MachineID).Scan(&active); err != nil {
		return nil, err
	}
	if active {
		return nil, apiError(409, "PROVISION_REQUEST_ACTIVE", "Machine already has an active installation preparation request")
	}
	if target.State == "provisioning" {
		return nil, apiError(409, "PROVISION_ALREADY_RUNNING", "Asset is already provisioning")
	}
	var trust string
	if err = tx.QueryRowContext(ctx, "SELECT ssh_trust FROM machines WHERE id=$1 FOR SHARE", target.MachineID).Scan(&trust); err != nil {
		return nil, err
	}
	if (target.State == "provisioned" || trust == "established") && !in.Reinstall {
		return nil, apiError(409, "PROVISION_REINSTALL_CONFIRM_REQUIRED", "Existing installation requires explicit reinstall intent")
	}
	if in.Reinstall && in.ConfirmSerial != target.Serial {
		return nil, apiError(422, "PROVISION_SERIAL_CONFIRM_REQUIRED", "Confirm the exact BOM serial for reinstall intent")
	}
	out = &ProvisionRequest{ID: RandomID(), ProjectID: project.ID, MachineID: target.MachineID, AssetID: asset, CreatedBy: a.User.ID, Reason: in.Reason, Reinstall: in.Reinstall, State: "requested", StateVersion: 1, CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(30 * time.Minute)}
	frozen, err := s.FreezeProvisionTx(ctx, tx, a, project, asset, in.ProvisionPreviewInput, out.ID)
	if err != nil {
		return nil, err
	}
	out.Snapshot = *frozen
	out.SnapshotSHA256 = frozen.SnapshotSHA256
	raw, err := json.Marshal(frozen)
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO provision_requests(id,project_id,machine_id,asset_id,created_by,reason,reinstall,snapshot,snapshot_sha256,created_at,expires_at,updated_at)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$10)`, out.ID, out.ProjectID, out.MachineID, out.AssetID, out.CreatedBy, out.Reason, out.Reinstall, raw, out.SnapshotSHA256, now, out.ExpiresAt)
	if err != nil {
		return nil, err
	}
	if err = appendAudit(ctx, tx, a.User.ID, project.ID, "provision.request.created", out.ID, trace, map[string]any{"machine_id": out.MachineID, "snapshot_sha256": out.SnapshotSHA256, "reinstall": out.Reinstall}); err != nil {
		return nil, err
	}
	if err = saveProvisionMutation(ctx, tx, a, idem, hash, out); err != nil {
		return nil, err
	}
	return out, tx.Commit()
}
func (s *Store) ReadProvisionRequest(ctx context.Context, a *AuthSession, project, id string) (*ProvisionRequest, error) {
	if !modelIDPattern.MatchString(id) {
		return nil, ErrNotFound
	}
	tx, err := s.beginPinnedRead(ctx, a)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = requireProvisionReadAuthority(ctx, tx, a, project, false); err != nil {
		return nil, err
	}
	x, err := scanProvisionRequest(tx.QueryRowContext(ctx, "SELECT "+provisionRequestColumns+" FROM provision_requests WHERE id=$1 AND project_id=$2", id, project))
	if err != nil {
		return nil, err
	}
	return x, tx.Commit()
}
func (s *Store) ListProvisionRequests(ctx context.Context, a *AuthSession, project string, p Pagination) (Page[ProvisionRequest], error) {
	out := NewPage[ProvisionRequest](p)
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
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM provision_requests WHERE project_id=$1", project).Scan(&out.Total); err != nil {
		return out, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT "+provisionRequestColumns+" FROM provision_requests WHERE project_id=$1 ORDER BY created_at DESC,id LIMIT $2 OFFSET $3", project, p.PageSize, p.Offset())
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		x, e := scanProvisionRequest(rows)
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
func (s *Store) ChangeProvisionRequest(ctx context.Context, a *AuthSession, project, id, action string, in ProvisionRequestActionInput, key, trace string) (*ProvisionRequest, error) {
	if !modelIDPattern.MatchString(id) || in.ExpectedVersion < 1 || (action != "approve" && action != "cancel") {
		return nil, apiError(400, "VALIDATION_ERROR", "Exact request, version and action required")
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if in.Reason == "" || utf8.RuneCountInString(in.Reason) > 500 {
		return nil, apiError(400, "VALIDATION_ERROR", "Bounded action reason required")
	}
	tx, err := s.beginPinnedRead(ctx, a)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = requireProvisionAuthority(ctx, tx, a, project, action == "approve"); err != nil {
		return nil, err
	}
	idem, hash, prior, err := provisionMutation(ctx, tx, a, project+":"+id+":"+action, key, in)
	if err != nil {
		return nil, err
	}
	if prior != nil {
		return prior, nil
	}
	x, err := scanProvisionRequest(tx.QueryRowContext(ctx, "SELECT "+provisionRequestColumns+" FROM provision_requests WHERE project_id=$1 AND id=$2 FOR UPDATE", project, id))
	if err != nil {
		return nil, err
	}
	if x.StateVersion != in.ExpectedVersion {
		return nil, errVersion
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if action == "approve" {
		if x.State != "requested" || !x.ExpiresAt.After(now) {
			return nil, apiError(409, "PROVISION_REQUEST_NOT_APPROVABLE", "Current unexpired request required")
		}
		if x.CreatedBy == a.User.ID {
			return nil, apiError(409, "PROVISION_INDEPENDENT_APPROVAL_REQUIRED", "Creator cannot approve their own installation preparation")
		}
		// Approval freezes consent only. Worker revalidates actual source bytes under the device lease.
		if _, err = ReadSSHInstallCredential(ctx, tx, project, x.Snapshot.DomainKey, now.Add(time.Minute)); err != nil {
			return nil, err
		}
		x.State = "approved"
		user := a.User.ID
		x.ApprovedBy = &user
		x.ApprovedAt = &now
	} else {
		if x.State != "requested" && x.State != "approved" && x.State != "prepared" {
			return nil, apiError(409, "PROVISION_REQUEST_NOT_CANCELABLE", "Running preparation must be stopped/reconciled through its execution")
		}
		x.State = "canceled"
	}
	x.StateVersion++
	x.UpdatedAt = now
	_, err = tx.ExecContext(ctx, "UPDATE provision_requests SET state=$3,state_version=$4,approved_by=$5,approved_at=$6,updated_at=$7 WHERE project_id=$1 AND id=$2", project, id, x.State, x.StateVersion, x.ApprovedBy, x.ApprovedAt, now)
	if err != nil {
		return nil, err
	}
	if err = appendAudit(ctx, tx, a.User.ID, project, "provision.request."+action, id, trace, map[string]any{"reason": in.Reason, "state_version": x.StateVersion, "snapshot_sha256": x.SnapshotSHA256}); err != nil {
		return nil, err
	}
	if err = saveProvisionMutation(ctx, tx, a, idem, hash, x); err != nil {
		return nil, err
	}
	return x, tx.Commit()
}

// ProvisionPublicKeys grants deployment engineers candidate public identities
// without access to SSH credentials, trust approvals or key administration.
func (s *Store) ProvisionPublicKeys(ctx context.Context, a *AuthSession, project string, p Pagination) (Page[SSHDomainKey], error) {
	out := NewPage[SSHDomainKey](p)
	if err := p.Validate(); err != nil {
		return out, err
	}
	tx, err := s.beginPinnedRead(ctx, a)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if err = requireProvisionAuthority(ctx, tx, a, project, false); err != nil {
		return out, err
	}
	until := time.Now().Add(time.Hour)
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM ssh_domain_keys WHERE project_id=$1 AND revoked_at IS NULL AND expires_at>$2`, project, until).Scan(&out.Total); err != nil {
		return out, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT `+sshDomainColumns+` FROM ssh_domain_keys WHERE project_id=$1 AND revoked_at IS NULL AND expires_at>$2 ORDER BY created_at DESC,id LIMIT $3 OFFSET $4`, project, until, p.PageSize, p.Offset())
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		x, e := scanSSHDomainKey(rows)
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
