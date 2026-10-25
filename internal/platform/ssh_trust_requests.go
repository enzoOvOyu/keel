package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/enzoOvOyu/keel/internal/taskrunner"
)

type SSHTrustRequest struct {
	InstallKey          *taskrunner.InstallCredential `json:"install_key,omitempty"`
	ID                  string                        `json:"id"`
	ProjectID           string                        `json:"project_id"`
	MachineID           string                        `json:"machine_id"`
	Action              string                        `json:"action"`
	AssetVersion        int64                         `json:"asset_version"`
	ExpectedTrust       string                        `json:"expected_trust"`
	HostKey             string                        `json:"host_key"`
	ConfigHash          string                        `json:"config_hash"`
	CredentialRef       string                        `json:"credential_ref"`
	CredentialVersion   string                        `json:"credential_version"`
	CredentialExpiresAt time.Time                     `json:"credential_expires_at"`
	Reason              string                        `json:"reason"`
	CreatedBy           string                        `json:"created_by"`
	ApprovedBy          string                        `json:"approved_by"`
	CreatedAt           time.Time                     `json:"created_at"`
	ExpiresAt           time.Time                     `json:"expires_at"`
	State               string                        `json:"state"`
	StateVersion        int64                         `json:"state_version"`
	ExecutionID         string                        `json:"execution_id"`
}
type CreateSSHTrustRequestInput struct {
	InstallKeyID         string `json:"install_key_id,omitempty"`
	MachineID            string `json:"machine_id"`
	Action               string `json:"action"`
	ExpectedAssetVersion int64  `json:"expected_asset_version"`
	HostKey              string `json:"host_key"`
	Reason               string `json:"reason"`
	ExpiresInSeconds     int    `json:"expires_in_seconds"`
}
type SSHTrustRequestActionInput struct {
	ExpectedVersion int64  `json:"expected_version"`
	Reason          string `json:"reason,omitempty"`
}

var sshTrustIDPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

const sshTrustColumns = `id,project_id,machine_id,action,asset_version,expected_trust,host_key,config_hash,credential_ref,credential_version,credential_expires_at,reason,created_by,coalesce(approved_by,''),created_at,expires_at,state,state_version,coalesce(execution_id,''),install_key_snapshot`

type sshTrustScanner interface{ Scan(...any) error }

func scanSSHTrustRequest(row sshTrustScanner) (*SSHTrustRequest, error) {
	x := &SSHTrustRequest{}
	var install []byte
	e := row.Scan(&x.ID, &x.ProjectID, &x.MachineID, &x.Action, &x.AssetVersion, &x.ExpectedTrust, &x.HostKey, &x.ConfigHash, &x.CredentialRef, &x.CredentialVersion, &x.CredentialExpiresAt, &x.Reason, &x.CreatedBy, &x.ApprovedBy, &x.CreatedAt, &x.ExpiresAt, &x.State, &x.StateVersion, &x.ExecutionID, &install)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if e == nil {
		e = json.Unmarshal(install, &x.InstallKey)
	}
	return x, e
}
func requireSSHTrustAuthority(ctx context.Context, tx *sql.Tx, a *AuthSession, project string, approve bool) error {
	if e := requireProjectMember(ctx, tx, a, project); e != nil {
		return e
	}
	var state string
	if e := tx.QueryRowContext(ctx, `SELECT state FROM projects WHERE id=$1 FOR SHARE`, project).Scan(&state); errors.Is(e, sql.ErrNoRows) {
		return ErrNotFound
	} else if e != nil {
		return e
	}
	if state == "archived" || state == "delivered" {
		return apiError(409, "SSH_TRUST_PROJECT_CLOSED", "SSH trust changes are not permitted for a closed project")
	}
	if a.User.IsAdmin {
		return nil
	}
	var role string
	if e := tx.QueryRowContext(ctx, `SELECT role FROM memberships WHERE project_id=$1 AND user_id=$2 FOR SHARE`, project, a.User.ID).Scan(&role); e != nil {
		return e
	}
	if role == "Admin" || role == "DL" || (!approve && role == "SRE") {
		return nil
	}
	return ErrForbidden
}

// The authorization freezes public credential identity and trusted host identity;
// it does not perform a handshake or install any key.
func (s *Store) validateSSHTrustTarget(ctx context.Context, tx *sql.Tx, x *SSHTrustRequest) error {
	if s.SSHRunner == nil {
		return apiError(503, "SSH_DISABLED", "SSH execution is not configured")
	}
	if x.InstallKey == nil {
		if err := EnforceSSHDomainBinding(ctx, tx, s.SSHRunner, x.ProjectID, x.MachineID); err != nil {
			return err
		}
	}
	binding, e := s.SSHRunner.Binding(x.ProjectID, x.MachineID)
	if e != nil {
		return apiError(409, "SSH_TRUST_BINDING_INVALID", "No valid configured SSH binding is available")
	}
	meta, e := s.SSHRunner.CredentialMetadata(x.ProjectID, x.MachineID)
	if e != nil || meta.Version == "" || meta.ExpiresAt == nil || meta.ExpiresAt.Before(x.ExpiresAt) {
		return apiError(409, "SSH_TRUST_CREDENTIAL_INVALID", "A versioned credential valid through the authorization expiry is required")
	}
	var version int64
	var state, trust, source string
	e = tx.QueryRowContext(ctx, `SELECT a.version,a.state,m.ssh_trust,m.ssh_trust_source FROM project_assets a JOIN machines m ON m.id=a.machine_id WHERE a.project_id=$1 AND a.machine_id=$2 FOR SHARE OF a,m`, x.ProjectID, x.MachineID).Scan(&version, &state, &trust, &source)
	if errors.Is(e, sql.ErrNoRows) {
		return ErrNotFound
	}
	if e != nil {
		return e
	}
	if version != x.AssetVersion {
		return errVersion
	}
	// Legacy/simulated established flags need an approved first real proof;
	// this does not downgrade or silently treat that flag as host evidence.
	if x.ConfigHash == "" && x.Action == "trust.repair" && trust == "established" && source != "ssh" {
		x.ExpectedTrust = "established"
	}
	if state != "provisioned" || trust != x.ExpectedTrust || (trust == "established" && source == "ssh") {
		return apiError(409, "SSH_TRUST_TARGET_CHANGED", "The provisioned target or expected trust state changed")
	}
	if x.HostKey != binding.HostKey {
		return apiError(409, "SSH_TRUST_HOST_KEY_MISMATCH", "The independently verified host key must match the configured binding")
	}
	if x.ConfigHash != "" && (x.ConfigHash != binding.ConfigHash || x.CredentialRef != meta.Ref || x.CredentialVersion != meta.Version || !x.CredentialExpiresAt.Equal(*meta.ExpiresAt)) {
		return apiError(409, "SSH_TRUST_BINDING_CHANGED", "The frozen SSH binding or credential version changed")
	}
	x.ConfigHash = binding.ConfigHash
	x.CredentialRef = meta.Ref
	x.CredentialVersion = meta.Version
	x.CredentialExpiresAt = *meta.ExpiresAt
	if x.InstallKey != nil {
		key, err := ReadSSHInstallCredential(ctx, tx, x.ProjectID, *x.InstallKey, x.ExpiresAt)
		if err != nil {
			return err
		}
		if err = s.SSHRunner.CheckInstallCredential(x.ProjectID, x.MachineID, *key); err != nil {
			return apiError(409, "SSH_INSTALL_CREDENTIAL_UNAVAILABLE", "The selected project domain key is not available to the configured provider")
		}
		x.InstallKey = key
	}
	return nil
}
func trustRequestIdempotency(ctx context.Context, tx *sql.Tx, a *AuthSession, scope, key string, input any) (string, string, *SSHTrustRequest, error) {
	if len(key) < 1 || len(key) > 128 {
		return "", "", nil, apiError(400, "VALIDATION_ERROR", "Idempotency-Key is required (1..128 characters)")
	}
	for _, r := range key {
		if r < 33 || r > 126 {
			return "", "", nil, apiError(400, "VALIDATION_ERROR", "Invalid Idempotency-Key")
		}
	}
	raw, _ := json.Marshal(input)
	hash := digest(string(raw))
	id := digest("ssh.trust:" + scope + ":" + a.User.ID + ":" + key)
	if e := lockKey(ctx, tx, id); e != nil {
		return "", "", nil, e
	}
	var priorHash string
	var prior []byte
	e := tx.QueryRowContext(ctx, `SELECT request_hash,response FROM idempotencies WHERE id=$1`, id).Scan(&priorHash, &prior)
	if errors.Is(e, sql.ErrNoRows) {
		return id, hash, nil, nil
	}
	if e != nil {
		return "", "", nil, e
	}
	if priorHash != hash {
		return "", "", nil, apiError(409, "IDEMPOTENCY_CONFLICT", "The idempotency key belongs to a different request")
	}
	var result SSHTrustRequest
	e = json.Unmarshal(prior, &result)
	return id, hash, &result, e
}
func saveTrustRequestIdempotency(ctx context.Context, tx *sql.Tx, a *AuthSession, id, hash string, x *SSHTrustRequest) error {
	raw, e := json.Marshal(x)
	if e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO idempotencies(id,actor_id,request_hash,response,created_at)VALUES($1,$2,$3,$4,$5)`, id, a.User.ID, hash, raw, time.Now().UTC())
	return e
}
func (s *Store) CreateSSHTrustRequest(ctx context.Context, a *AuthSession, project *ProjectSummary, in CreateSSHTrustRequestInput, key, trace string) (*SSHTrustRequest, error) {
	if in.InstallKeyID != "" && !sshTrustIDPattern.MatchString(in.InstallKeyID) {
		return nil, apiError(400, "VALIDATION_ERROR", "Invalid project domain key id")
	}
	if in.ExpiresInSeconds == 0 {
		in.ExpiresInSeconds = 900
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if !sshTrustIDPattern.MatchString(in.MachineID) || in.ExpectedAssetVersion < 1 || !taskrunner.IsValidHostKey(in.HostKey) || in.ExpiresInSeconds < 300 || in.ExpiresInSeconds > 3600 || utf8.RuneCountInString(in.Reason) < 1 || utf8.RuneCountInString(in.Reason) > 500 || strings.ContainsAny(in.Reason, "\x00\r\n") {
		return nil, apiError(400, "VALIDATION_ERROR", "Invalid SSH trust target, host key, expiry or review reason")
	}
	expected := "unverified"
	if in.Action == "trust.repair" {
		expected = "broken"
	} else if in.Action != "trust.bootstrap" {
		return nil, apiError(400, "VALIDATION_ERROR", "Choose trust.bootstrap or trust.repair")
	}
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireSSHTrustAuthority(ctx, tx, a, project.ID, false); e != nil {
		return nil, e
	}
	idem, hash, prior, e := trustRequestIdempotency(ctx, tx, a, project.ID+":create", key, in)
	if e != nil {
		return nil, e
	}
	if prior != nil {
		return prior, nil
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	x := &SSHTrustRequest{ID: RandomID(), ProjectID: project.ID, MachineID: in.MachineID, Action: in.Action, AssetVersion: in.ExpectedAssetVersion, ExpectedTrust: expected, HostKey: in.HostKey, Reason: in.Reason, CreatedBy: a.User.ID, CreatedAt: now, ExpiresAt: now.Add(time.Duration(in.ExpiresInSeconds) * time.Second), State: "pending", StateVersion: 1}
	if in.InstallKeyID != "" {
		x.InstallKey = &taskrunner.InstallCredential{ID: in.InstallKeyID}
	}
	if e = s.validateSSHTrustTarget(ctx, tx, x); e != nil {
		return nil, e
	}
	install, _ := json.Marshal(x.InstallKey)
	_, e = tx.ExecContext(ctx, `INSERT INTO ssh_trust_requests(id,project_id,machine_id,action,asset_version,expected_trust,host_key,config_hash,credential_ref,credential_version,credential_expires_at,reason,created_by,created_at,expires_at,state,state_version,install_key_snapshot)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,'pending',1,$16)`, x.ID, x.ProjectID, x.MachineID, x.Action, x.AssetVersion, x.ExpectedTrust, x.HostKey, x.ConfigHash, x.CredentialRef, x.CredentialVersion, x.CredentialExpiresAt, x.Reason, x.CreatedBy, x.CreatedAt, x.ExpiresAt, install)
	if e != nil {
		return nil, e
	}
	if e = appendAudit(ctx, tx, a.User.ID, project.ID, "ssh.trust.request", x.ID, trace, map[string]any{"machine_id": x.MachineID, "action": x.Action, "credential_ref": x.CredentialRef, "credential_version": x.CredentialVersion, "expires_at": x.ExpiresAt, "source": "ssh"}); e != nil {
		return nil, e
	}
	if e = saveTrustRequestIdempotency(ctx, tx, a, idem, hash, x); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return x, nil
}
func (s *Store) ActSSHTrustRequest(ctx context.Context, a *AuthSession, project *ProjectSummary, id, action string, in SSHTrustRequestActionInput, key, trace string) (*SSHTrustRequest, error) {
	if !sshTrustIDPattern.MatchString(id) || in.ExpectedVersion < 1 || (action != "approve" && action != "revoke") {
		return nil, apiError(400, "VALIDATION_ERROR", "Invalid SSH trust action or expected_version")
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if action == "revoke" && (utf8.RuneCountInString(in.Reason) < 1 || utf8.RuneCountInString(in.Reason) > 500 || strings.ContainsAny(in.Reason, "\x00\r\n")) {
		return nil, apiError(400, "VALIDATION_ERROR", "A bounded revocation reason is required")
	}
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireSSHTrustAuthority(ctx, tx, a, project.ID, action == "approve"); e != nil {
		return nil, e
	}
	idem, hash, prior, e := trustRequestIdempotency(ctx, tx, a, project.ID+":"+id+":"+action, key, in)
	if e != nil {
		return nil, e
	}
	if prior != nil {
		return prior, nil
	}
	x, e := scanSSHTrustRequest(tx.QueryRowContext(ctx, `SELECT `+sshTrustColumns+` FROM ssh_trust_requests WHERE project_id=$1 AND id=$2 FOR UPDATE`, project.ID, id))
	if e != nil {
		return nil, e
	}
	if x.StateVersion != in.ExpectedVersion {
		return nil, errVersion
	}
	if x.State != "pending" && x.State != "approved" {
		return nil, apiError(409, "SSH_TRUST_STATE_CONFLICT", "Only pending or approved authorizations can change")
	}
	if action == "approve" {
		if x.State != "pending" {
			return nil, apiError(409, "SSH_TRUST_STATE_CONFLICT", "Only pending authorizations can be approved")
		}
		if x.CreatedBy == a.User.ID {
			return nil, apiError(403, "SSH_TRUST_INDEPENDENT_APPROVAL_REQUIRED", "Another authorized person must approve this request")
		}
		if !time.Now().Before(x.ExpiresAt) {
			return nil, apiError(409, "SSH_TRUST_EXPIRED", "The authorization expired")
		}
		var enabled, admin bool
		if e = tx.QueryRowContext(ctx, `SELECT enabled,is_admin FROM users WHERE id=$1 FOR SHARE`, x.CreatedBy).Scan(&enabled, &admin); e != nil {
			return nil, e
		}
		if !enabled {
			return nil, apiError(403, "SSH_TRUST_CREATOR_REVOKED", "The requester no longer has permission")
		}
		if !admin {
			var role string
			e = tx.QueryRowContext(ctx, `SELECT role FROM memberships WHERE project_id=$1 AND user_id=$2 FOR SHARE`, project.ID, x.CreatedBy).Scan(&role)
			if errors.Is(e, sql.ErrNoRows) {
				return nil, apiError(403, "SSH_TRUST_CREATOR_REVOKED", "The requester no longer has permission")
			}
			if e != nil {
				return nil, e
			}
			if role != "Admin" && role != "DL" && role != "SRE" {
				return nil, apiError(403, "SSH_TRUST_CREATOR_REVOKED", "The requester no longer has permission")
			}
		}
		if e = s.validateSSHTrustTarget(ctx, tx, x); e != nil {
			return nil, e
		}
		x.ApprovedBy = a.User.ID
		x.State = "approved"
	} else {
		x.State = "revoked"
	}
	_, e = tx.ExecContext(ctx, `UPDATE ssh_trust_requests SET state=$3,approved_by=nullif($4,''),state_version=state_version+1 WHERE project_id=$1 AND id=$2`, project.ID, id, x.State, x.ApprovedBy)
	if e != nil {
		return nil, e
	}
	x.StateVersion++
	if e = appendAudit(ctx, tx, a.User.ID, project.ID, "ssh.trust."+action, id, trace, map[string]any{"machine_id": x.MachineID, "state_version": x.StateVersion, "reason": in.Reason, "source": "ssh"}); e != nil {
		return nil, e
	}
	if e = saveTrustRequestIdempotency(ctx, tx, a, idem, hash, x); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return x, nil
}
func (s *Store) ListSSHTrustRequests(ctx context.Context, a *AuthSession, project *ProjectSummary, p Pagination) (Page[SSHTrustRequest], error) {
	out := NewPage[SSHTrustRequest](p)
	if e := p.Validate(); e != nil {
		return out, e
	}
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	if e = requireProjectMember(ctx, tx, a, project.ID); e != nil {
		return out, e
	}
	if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM ssh_trust_requests WHERE project_id=$1`, project.ID).Scan(&out.Total); e != nil {
		return out, e
	}
	rows, e := tx.QueryContext(ctx, `SELECT `+sshTrustColumns+` FROM ssh_trust_requests WHERE project_id=$1 ORDER BY created_at DESC,id LIMIT $2 OFFSET $3`, project.ID, p.PageSize, p.Offset())
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		x, err := scanSSHTrustRequest(rows)
		if err != nil {
			return out, err
		}
		out.Items = append(out.Items, *x)
	}
	return out, rows.Err()
}
