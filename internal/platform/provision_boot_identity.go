package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/netip"
	"os"
	"time"

	"github.com/enzoOvOyu/keel/internal/provisionboot"
	"github.com/enzoOvOyu/keel/internal/provisionpreflight"
)

type ProvisionBootToken = provisionboot.BootstrapBinding
type ProvisionBootIdentity = provisionboot.BootAcknowledgment

func provisionTokenHash(raw string) string { return digest("keel-provision-boot-token-v1:" + raw) }
func requireProvisionTokenOwnerTx(ctx context.Context, tx *sql.Tx) error {
	var owner bool
	err := tx.QueryRowContext(ctx, `SELECT pg_get_userbyid(c.relowner)=current_user OR COALESCE((SELECT rolsuper FROM pg_roles WHERE rolname=current_user),false) FROM pg_class c WHERE c.oid='provision_boot_tokens'::regclass`).Scan(&owner)
	if err != nil {
		return err
	}
	if !owner {
		return ErrForbidden
	}
	return nil
}

// IssueProvisionBootToken is an owner-only protected-file provisioning action.
// API/runtime roles cannot mint it, and no public boot resource carries it.
// Delivery to the intended machine is an explicit out-of-band responsibility;
// a MAC address or DHCP fingerprint cannot request or retrieve this secret.
func (s *Store) IssueProvisionBootToken(ctx context.Context, project, id, reason string, ttl time.Duration) (*ProvisionBootToken, string, error) {
	if !modelIDPattern.MatchString(project) || !modelIDPattern.MatchString(id) || !validProvisionReason(reason) || ttl < time.Minute || ttl > 10*time.Minute {
		return nil, "", ErrProvisionBootDenied
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, "", err
	}
	defer tx.Rollback()
	if err = requireProvisionTokenOwnerTx(ctx, tx); err != nil {
		return nil, "", err
	}
	x, _, _, _, err := installationMutationTx(ctx, tx, project, id)
	if err != nil {
		return nil, "", err
	}
	if x.State != "booting" || x.BootID != nil {
		return nil, "", ErrProvisionBootDenied
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	until := now.Add(ttl)
	if x.ExpiresAt.Before(until) {
		until = x.ExpiresAt
	}
	if !until.After(now.Add(30 * time.Second)) {
		return nil, "", ErrProvisionBootDenied
	}
	token, err := randomToken()
	if err != nil {
		return nil, "", err
	}
	out := &ProvisionBootToken{ID: RandomID(), InstallationID: id, ProjectID: project, GrantID: x.GrantID, Spec: *x.Spec, ExpiresAt: until}
	if _, err = tx.ExecContext(ctx, `INSERT INTO provision_boot_tokens(id,installation_id,token_hash,reason,created_at,expires_at)VALUES($1,$2,$3,$4,$5,$6)`, out.ID, id, provisionTokenHash(token), reason, now, until); err != nil {
		return nil, "", err
	}
	if err = appendAudit(ctx, tx, "", project, "provision.boot_token.issued", id, "cli", map[string]any{"token_id": out.ID, "expires_at": until, "reason": reason, "operator_uid": os.Geteuid()}); err != nil {
		return nil, "", err
	}
	if err = tx.Commit(); err != nil {
		return nil, "", err
	}
	return out, token, nil
}
func (s *Store) RevokeProvisionBootToken(ctx context.Context, project, id, tokenID, reason string) error {
	if !modelIDPattern.MatchString(project) || !modelIDPattern.MatchString(id) || !modelIDPattern.MatchString(tokenID) || !validProvisionReason(reason) {
		return ErrProvisionBootDenied
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = requireProvisionTokenOwnerTx(ctx, tx); err != nil {
		return err
	}
	// Revocation remains possible after cancellation/expiry; no live write
	// admission is needed. Domain row still precedes token row in lock order.
	var installation string
	if err = tx.QueryRowContext(ctx, "SELECT id FROM provision_installations WHERE id=$1 AND project_id=$2 FOR UPDATE", id, project).Scan(&installation); err != nil {
		return ErrProvisionBootDenied
	}
	var consumed, revoked sql.NullTime
	if err = tx.QueryRowContext(ctx, "SELECT consumed_at,revoked_at FROM provision_boot_tokens WHERE id=$1 AND installation_id=$2 FOR UPDATE", tokenID, id).Scan(&consumed, &revoked); err != nil || consumed.Valid {
		return ErrProvisionBootDenied
	}
	if revoked.Valid {
		return tx.Commit()
	}
	if _, err = tx.ExecContext(ctx, "UPDATE provision_boot_tokens SET revoked_at=now() WHERE id=$1", tokenID); err != nil {
		return err
	}
	if err = appendAudit(ctx, tx, "", project, "provision.boot_token.revoked", id, "cli", map[string]any{"token_id": tokenID, "reason": reason, "operator_uid": os.Geteuid()}); err != nil {
		return err
	}
	return tx.Commit()
}

func bootIdentity(x *ProvisionInstallation) *ProvisionBootIdentity {
	return &ProvisionBootIdentity{InstallationID: x.ID, BootID: *x.BootID, Sequence: x.CallbackSequence, State: x.State, ExpiresAt: x.ExpiresAt}
}
func (s *Store) EnrollProvisionBoot(ctx context.Context, project, grant, token string, enrollment provisionboot.BootEnrollment, proof provisionboot.BootProof) (*ProvisionBootIdentity, error) {
	return s.enrollProvisionBoot(ctx, project, grant, token, enrollment, proof, nil)
}

type provisionCallbackPeer struct {
	scope provisionboot.Scope
	peer  netip.Addr
}

func checkProvisionCallbackPeer(want *provisionCallbackPeer, current *ProvisionBootScope) error {
	if want == nil {
		return nil
	}
	raw, err := json.Marshal(want.scope)
	if err != nil || current == nil || current.ID != want.scope.ID || current.ProjectID != want.scope.ProjectID || digest(string(raw)) != current.ScopeSHA256 || !current.Scope.CheckPeer(want.peer) {
		return ErrProvisionBootDenied
	}
	return nil
}
func (s *Store) enrollProvisionBoot(ctx context.Context, project, grant, token string, enrollment provisionboot.BootEnrollment, proof provisionboot.BootProof, peer *provisionCallbackPeer) (*ProvisionBootIdentity, error) {
	if !s.ProvisionInstallEnabled || !validToken(token) || !modelIDPattern.MatchString(project) || !modelIDPattern.MatchString(grant) {
		return nil, ErrProvisionBootDenied
	}
	key, err := enrollment.Key()
	if err != nil {
		return nil, ErrProvisionBootDenied
	}
	payload, err := json.Marshal(enrollment)
	if err != nil {
		return nil, ErrProvisionBootDenied
	}
	if provisionboot.VerifyBootProof(key, enrollment.InstallationID, enrollment.BootID, 1, "enroll", payload, proof) != nil {
		return nil, ErrProvisionBootDenied
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	x, _, scope, _, err := installationMutationTx(ctx, tx, project, enrollment.InstallationID)
	if err != nil {
		return nil, err
	}
	if err = checkProvisionCallbackPeer(peer, scope); err != nil {
		return nil, err
	}
	if x.GrantID != grant || x.State != "booting" || x.BootID != nil || x.CallbackSequence != 0 {
		return nil, ErrProvisionBootDenied
	}
	var tokenID string
	var until time.Time
	var consumed, revoked sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT id,expires_at,consumed_at,revoked_at FROM provision_boot_tokens WHERE installation_id=$1 AND token_hash=$2 FOR UPDATE`, x.ID, provisionTokenHash(token)).Scan(&tokenID, &until, &consumed, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrProvisionBootDenied
	}
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if consumed.Valid || revoked.Valid || !until.After(now) {
		return nil, ErrProvisionBootDenied
	}
	if _, err = tx.ExecContext(ctx, `UPDATE provision_boot_tokens SET consumed_at=$2 WHERE id=$1`, tokenID, now); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE provision_installations SET boot_id=$2,boot_public_key=$3,callback_sequence=1,state_version=state_version+1,updated_at=$4 WHERE id=$1`, x.ID, enrollment.BootID, []byte(key), now); err != nil {
		return nil, err
	}
	if err = appendProvisionEventTx(ctx, tx, x, "boot.enrolled", map[string]any{"boot_id": enrollment.BootID, "token_id": tokenID}); err != nil {
		return nil, err
	}
	if err = appendAudit(ctx, tx, "", project, "provision.boot.enrolled", x.ID, "boot", map[string]any{"boot_id": enrollment.BootID, "token_id": tokenID, "attempt_id": x.AttemptID, "fencing_token": x.FencingToken}); err != nil {
		return nil, err
	}
	x.BootID = &enrollment.BootID
	x.CallbackSequence = 1
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return bootIdentity(x), nil
}

// SubmitProvisionPreflight recomputes hardware facts after verifying the
// enrolled private-key proof, exact next sequence and live execution authority.
// It never projects installation success or accepts an unauthenticated PASS.
func (s *Store) SubmitProvisionPreflight(ctx context.Context, project, grant string, raw []byte, proof provisionboot.BootProof) (*ProvisionBootIdentity, error) {
	return s.submitProvisionPreflight(ctx, project, grant, raw, proof, nil)
}
func (s *Store) submitProvisionPreflight(ctx context.Context, project, grant string, raw []byte, proof provisionboot.BootProof, peer *provisionCallbackPeer) (*ProvisionBootIdentity, error) {
	if !s.ProvisionInstallEnabled || len(raw) == 0 || len(raw) > 2<<20 {
		return nil, ErrProvisionBootDenied
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	x, _, scope, _, err := installationMutationTx(ctx, tx, project, proof.InstallationID)
	if err != nil {
		return nil, err
	}
	if err = checkProvisionCallbackPeer(peer, scope); err != nil {
		return nil, err
	}
	if x.GrantID != grant || x.BootID == nil || (x.State != "booting" && x.State != "preflight_ready") || x.StoragePlan != nil || x.CallbackSequence > 32 || provisionboot.VerifyBootProof(x.BootPublicKey, x.ID, *x.BootID, x.CallbackSequence+1, "preflight", raw, proof) != nil {
		return nil, ErrProvisionBootDenied
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	report, err := provisionpreflight.EvaluateReport(*x.Spec, raw, now)
	if err != nil {
		return nil, ErrProvisionBootDenied
	}
	state := "preflight_ready"
	var reason *string
	if report.Status != "PASS" {
		state = "failed"
		v := "preflight_" + report.Status
		reason = &v
	}
	hash := digest(string(raw))
	if _, err = tx.ExecContext(ctx, `UPDATE provision_installations SET state=$2,state_version=state_version+1,callback_sequence=callback_sequence+1,latest_report=$3,latest_report_sha256=$4,failure_reason=$5,updated_at=$6 WHERE id=$1`, x.ID, state, raw, hash, reason, now); err != nil {
		return nil, err
	}
	if err = appendProvisionEventTx(ctx, tx, x, "preflight.reported", map[string]any{"status": report.Status, "report_sha256": hash, "sequence": proof.Sequence, "installation_performed": false}); err != nil {
		return nil, err
	}
	x.State = state
	x.CallbackSequence++
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return bootIdentity(x), nil
}
