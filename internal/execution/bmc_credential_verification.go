package execution

import (
	"context"
	"strings"
	"time"

	"github.com/enzoOvOyu/keel/internal/platform"
)

type BMCCredentialVerificationInput struct {
	ExpectedCredentialVersion string `json:"expected_credential_version"`
	ExpectedScopeHash         string `json:"expected_scope_hash"`
	Reason                    string `json:"reason"`
}

// This is atomic full-scope admission into the existing Execution scheduler.
// Neither request bodies nor individual child results can select a subset.
func (s *Service) CreateBMCCredentialVerification(ctx context.Context, a *Actor, project, batch string, in BMCCredentialVerificationInput, key, trace string) (*platform.BMCCredentialVerification, error) {
	if s.BMCStore == nil || !s.BMCStore.BMCInventoryEnabled {
		return nil, failure(409, "BMC_INVENTORY_DISABLED", "BMC inventory admission is disabled")
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if in.ExpectedCredentialVersion == "" || len(in.ExpectedScopeHash) != 64 || len(in.Reason) < 3 || len(in.Reason) > 1000 {
		return nil, invalid("Exact preview hash, credential version and reason required")
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = advisory(ctx, tx, "keel-admin-initialization"); e != nil {
		return nil, e
	}
	if e = authorize(ctx, tx, a, project, "Admin", "DE", "DL", "SRE"); e != nil {
		return nil, e
	}
	idem, prior, e := idempotency(ctx, tx, a, "bmc.credential.verify:"+project+":"+batch, key, in)
	if e != nil {
		return nil, e
	}
	if prior != nil {
		out, e := decode[platform.BMCCredentialVerification](prior)
		return &out, e
	}
	scope, e := s.BMCStore.BMCCredentialVerificationScopeTx(ctx, tx, project, batch, true)
	if e != nil {
		return nil, e
	}
	if !scope.Ready {
		return nil, failure(409, scope.Code, "The complete current credential batch is not ready for verification")
	}
	if scope.ScopeHash != in.ExpectedScopeHash || scope.CredentialVersion != in.ExpectedCredentialVersion {
		return nil, failure(409, "BMC_VERIFICATION_SCOPE_CHANGED", "Full batch scope or credential version changed; preview again")
	}
	var active bool
	if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM bmc_credential_verifications q WHERE q.project_id=$1 AND q.credential_batch_ref=$2 AND q.credential_version=$3 AND q.scope_hash=$4 AND q.expires_at>statement_timestamp() AND NOT EXISTS(SELECT 1 FROM bmc_credential_verification_results r WHERE r.verification_id=q.id))`, project, batch, scope.CredentialVersion, scope.ScopeHash).Scan(&active); e != nil {
		return nil, e
	}
	if active {
		return nil, failure(409, "BMC_VERIFICATION_ACTIVE", "A full verification for this exact batch scope is already active")
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	deadline := now.Add(2 * time.Hour)
	if scope.CredentialExpiresAt.Before(deadline) {
		deadline = scope.CredentialExpiresAt
	}
	if !deadline.After(now) {
		return nil, failure(409, "BMC_CREDENTIAL_UNAVAILABLE", "Credential expired before admission")
	}
	id := platform.RandomID()
	if _, e = tx.ExecContext(ctx, `INSERT INTO bmc_credential_verifications(id,project_id,credential_batch_ref,credential_version,scope_hash,scope,target_count,created_by,reason,created_at,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, id, project, batch, scope.CredentialVersion, scope.ScopeHash, jsonBytes(scope), scope.TargetCount, a.User.ID, in.Reason, now, deadline); e != nil {
		return nil, e
	}
	for _, target := range scope.Targets {
		child, e := s.createBMCCollectionTx(ctx, tx, a, project, target.AssetID, platform.BMCCollectionInput{Level: "L1", ExpectedAssetVersion: target.AssetVersion, Reason: in.Reason}, trace, nil, id)
		if e != nil {
			return nil, e
		}
		collection, ok := child.Parameters["bmc_collection_id"].(string)
		if !ok || collection == "" {
			return nil, conflict("Verification child lacks its frozen BMC request")
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO bmc_credential_verification_targets(verification_id,asset_id,machine_id,snapshot,execution_id,collection_id) VALUES($1,$2,$3,$4,$5,$6)`, id, target.AssetID, target.MachineID, jsonBytes(target), child.ID, collection); e != nil {
			return nil, e
		}
	}
	if e = audit(ctx, tx, a.User.ID, project, "bmc.credential.verify.request", id, trace, map[string]any{"credential_batch_ref": batch, "credential_version": scope.CredentialVersion, "scope_hash": scope.ScopeHash, "target_count": scope.TargetCount, "reason": in.Reason}); e != nil {
		return nil, e
	}
	out, e := s.BMCStore.BMCCredentialVerificationTx(ctx, tx, project, batch, id)
	if e != nil {
		return nil, e
	}
	if e = remember(ctx, tx, idem, in, out); e != nil {
		return nil, e
	}
	return out, tx.Commit()
}
