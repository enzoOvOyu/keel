package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/enzoOvOyu/keel/internal/fabricboot"
	"github.com/enzoOvOyu/keel/internal/netbaseline"
)

// Revalidation may bypass its own link fault, never the management identity and
// current plan/credential binding. Fixture admission is kept in its own origin.
func (s *Store) issueVerificationManagementHashTx(ctx context.Context, tx *sql.Tx, scope *IssueVerificationScope) (string, error) {
	if scope.EvidenceOrigin == "protocol_fixture" {
		fingerprint := "not-configured"
		if s.FabricProbes != nil {
			fingerprint = s.FabricProbes.Fingerprint
		}
		return netbaseline.ContentHash([]string{"protocol_fixture", fingerprint}), nil
	}
	facts := []any{}
	for _, target := range scope.Targets {
		var session, check, batch, hostname, address, credential, probe string
		var revision int64
		var raw, credentialFacts []byte
		e := tx.QueryRowContext(ctx, `SELECT s.id,s.state_version,c.id,s.batch_ref,c.request,c.probe_hash,a.hostname,host(a.management_ip),coalesce(b.credential_batch_ref,''),to_jsonb(cb) FROM ztp_sessions s JOIN ztp_manageability_checks c ON c.session_id=s.id AND c.project_id=s.project_id AND c.switch_id=s.switch_id AND c.evidence_source='device' AND c.code='ready' JOIN ztp_management_assignments a ON a.project_id=s.project_id AND a.switch_id=s.switch_id AND a.batch_ref=s.batch_ref JOIN bom_items b ON b.item_uid=s.switch_id AND b.status<>'retired' JOIN bom_credential_batches cb ON cb.project_id=s.project_id AND cb.credential_batch_ref=b.credential_batch_ref AND cb.scope='switch' WHERE s.project_id=$1 AND s.switch_id=$2 AND s.mode='device' AND s.state='managed' AND NOT EXISTS(SELECT 1 FROM ztp_manageability_checks later WHERE later.project_id=s.project_id AND later.switch_id=s.switch_id AND (later.checked_at,later.id)>(c.checked_at,c.id)) ORDER BY c.checked_at DESC,c.id DESC LIMIT 1`, scope.ProjectID, target.SwitchID).Scan(&session, &revision, &check, &batch, &raw, &probe, &hostname, &address, &credential, &credentialFacts)
		if errors.Is(e, sql.ErrNoRows) {
			return "", apiError(409, "ISSUE_VERIFICATION_MANAGEMENT_REQUIRED", "Current authenticated switch management and credential binding required")
		}
		if e != nil {
			return "", e
		}
		var request fabricboot.ManageabilityRequest
		if e = json.Unmarshal(raw, &request); e != nil {
			return "", e
		}
		plan, e := currentManagementPlanTx(ctx, tx, scope.ProjectID, batch)
		if e != nil {
			return "", e
		}
		if request.ProjectID != scope.ProjectID || request.SwitchID != target.SwitchID || request.Serial != target.Serial || request.CredentialBatchRef != credential || request.Hostname != hostname || request.Address != address || request.PlanHash != plan.ContentHash || s.FabricProbes == nil || probe != s.FabricProbes.Fingerprint {
			return "", apiError(409, "ISSUE_VERIFICATION_MANAGEMENT_CHANGED", "Management identity, plan, collector or credentials changed")
		}
		facts = append(facts, []any{session, revision, check, request, plan.ContentHash, probe, json.RawMessage(credentialFacts)})
	}
	return netbaseline.ContentHash(facts), nil
}
