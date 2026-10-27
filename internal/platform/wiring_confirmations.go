package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/enzoOvOyu/keel/internal/wiring"
)

type WiringConfirmationInput struct {
	AssetID string `json:"asset_id"`
	Step3ID string `json:"step3_id"`
	Reason  string `json:"reason"`
}
type WiringConfirmation struct {
	ID                 string                `json:"id"`
	ProjectID          string                `json:"project_id"`
	BatchID            string                `json:"batch_id"`
	AssetID            string                `json:"asset_id"`
	Step3ID            string                `json:"step3_id"`
	Step4ID            string                `json:"step4_id"`
	CommandID          string                `json:"command_id"`
	SourceHash         string                `json:"source_hash"`
	ScopeHash          string                `json:"scope_hash"`
	ValidUntil         time.Time             `json:"valid_until"`
	CreatedBy          string                `json:"created_by"`
	CreatedAt          time.Time             `json:"created_at"`
	Reason             string                `json:"reason"`
	Ports              []wiring.ExpectedPort `json:"ports"`
	EffectiveVerdict   string                `json:"effective_verdict"`
	InvalidationReason string                `json:"invalidation_reason"`
}

func latestWiringReport(target *WiringBatchTarget, step int) *WiringStepReport {
	var out *WiringStepReport
	for i := range target.Reports {
		r := &target.Reports[i]
		if r.Step == step && (out == nil || out.Version < r.Version) {
			out = r
		}
	}
	return out
}

// Current observations, original scope and open physical-port issues are checked
// at confirmation and whenever a later gate consumes the retained confirmation.
func (s *Store) wiringConfirmableTx(ctx context.Context, tx *sql.Tx, project, batch, asset, report string) (*WiringBatchTarget, *WiringStepReport, *WiringCommand, *WiringFacts, error) {
	b, e := s.wiringBatchTx(ctx, tx, project, batch)
	if e != nil {
		return nil, nil, nil, nil, e
	}
	var target *WiringBatchTarget
	for i := range b.Targets {
		if b.Targets[i].AssetID == asset {
			target = &b.Targets[i]
			break
		}
	}
	if target == nil {
		return nil, nil, nil, nil, ErrNotFound
	}
	three := latestWiringReport(target, 3)
	if three == nil || three.ID != report || three.EffectiveVerdict != "PASS" || three.ValidUntil == nil || !three.ValidUntil.After(time.Now()) {
		return nil, nil, nil, nil, apiError(409, "WIRING_STEP3_REQUIRED", "Current passing Step 2/3 chain required")
	}
	c, facts, e := s.wiringReportScopeTx(ctx, tx, project, three, false)
	if e != nil {
		return nil, nil, nil, nil, e
	}
	return target, three, c, facts, nil
}
func (s *Store) ConfirmWiring(ctx context.Context, a *AuthSession, project, batch string, in WiringConfirmationInput, key, trace string) (*WiringConfirmation, error) {
	in.Reason = strings.TrimSpace(in.Reason)
	if !s.WiringEnabled {
		return nil, apiError(409, "WIRING_DISABLED", "Wiring confirmation disabled")
	}
	if !validEntityID(batch) || !validEntityID(in.AssetID) || !validEntityID(in.Step3ID) || utf8.RuneCountInString(in.Reason) < 3 || utf8.RuneCountInString(in.Reason) > 1000 || strings.TrimSpace(key) == "" || len(key) > 128 {
		return nil, apiError(400, "VALIDATION_ERROR", "Exact Step 3, asset, reason and Idempotency-Key required")
	}
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = wiringScopePermissionTx(ctx, tx, a, project, true); e != nil {
		return nil, e
	}
	role, e := issueRoleTx(ctx, tx, a, project)
	if e != nil {
		return nil, e
	}
	if role != "Admin" && role != "DL" {
		return nil, ErrForbidden
	}
	identity := digest("wiring.confirm:" + project + ":" + batch + ":" + a.User.ID + ":" + key)
	requestHash := digest(string(networkJSON(in)))
	if e = lockKey(ctx, tx, identity); e != nil {
		return nil, e
	}
	prior, e := priorWiringHandshakeTx(ctx, tx, identity, requestHash)
	if e != nil {
		return nil, e
	}
	if prior != "" {
		out, e := s.wiringConfirmationTx(ctx, tx, project, prior)
		if e != nil {
			return nil, e
		}
		return out, tx.Commit()
	}
	if e = lockKey(ctx, tx, "wiring-reports:"+batch+":"+in.AssetID); e != nil {
		return nil, e
	}
	target, three, c, _, e := s.wiringConfirmableTx(ctx, tx, project, batch, in.AssetID, in.Step3ID)
	if e != nil {
		return nil, e
	}
	var priorID string
	e = tx.QueryRowContext(ctx, `SELECT id FROM validation_wiring_confirmations WHERE step3_id=$1`, three.ID).Scan(&priorID)
	if e == nil {
		return nil, apiError(409, "WIRING_ALREADY_CONFIRMED", "This Step 3 already has its immutable DL confirmation")
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return nil, e
	}
	now := time.Now().UTC()
	out := &WiringConfirmation{ID: RandomID(), ProjectID: project, BatchID: batch, AssetID: in.AssetID, Step3ID: three.ID, Step4ID: RandomID(), CommandID: c.Binding.CommandID, SourceHash: three.SourceHash, ScopeHash: c.Expectation.ScopeHash, ValidUntil: *three.ValidUntil, CreatedBy: a.User.ID, CreatedAt: now, Reason: in.Reason, Ports: c.Expectation.Ports, EffectiveVerdict: "PASS"}
	_, e = tx.ExecContext(ctx, `INSERT INTO validation_wiring_confirmations(id,project_id,batch_id,asset_id,step3_id,step4_id,command_id,source_hash,scope_hash,valid_until,created_by,reason,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, out.ID, project, batch, in.AssetID, out.Step3ID, out.Step4ID, out.CommandID, out.SourceHash, out.ScopeHash, out.ValidUntil, a.User.ID, in.Reason, now)
	if e != nil {
		return nil, e
	}
	version := int64(1)
	if four := latestWiringReport(target, 4); four != nil {
		version = four.Version + 1
	}
	detail := three.Detail
	detail.ConfirmationID = out.ID
	detail.Step = 4
	detail.Verdict = "PASS"
	_, e = tx.ExecContext(ctx, `INSERT INTO validation_wiring_step_reports(id,batch_id,asset_id,step,version,parent_id,source_hash,verdict,detail,created_by,created_at,valid_until,policy_hash) VALUES($1,$2,$3,4,$4,$5,$6,'PASS',$7,$8,$9,$10,$11)`, out.Step4ID, batch, in.AssetID, version, three.ID, out.SourceHash, networkJSON(detail), a.User.ID, now, out.ValidUntil, three.PolicyHash)
	if e != nil {
		return nil, e
	}
	// Each endpoint keeps the handshake of its own original observation.
	for _, p := range c.Expectation.Ports {
		var peer string
		e = tx.QueryRowContext(ctx, `SELECT port->>'switch_port_id' FROM validation_wiring_commands source JOIN validation_wiring_handshakes h ON h.id=source.handshake_id CROSS JOIN LATERAL jsonb_array_elements(h.facts#>'{targets,0,ports}') port WHERE source.command_id=coalesce((SELECT proof->>'command_id' FROM validation_wiring_coverage_snapshots coverage CROSS JOIN LATERAL jsonb_array_elements(coverage.coverage->'proofs') proof WHERE coverage.id=$1 AND proof->>'physical_port_id'=$2),$3) AND port->>'physical_port_id'=$2`, three.Detail.CoverageID, p.PhysicalPortID, c.Binding.CommandID).Scan(&peer)
		if e != nil {
			return nil, e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO validation_wiring_confirmed_ports(confirmation_id,physical_port_id,switch_port_id,cable_id,expected) VALUES($1,$2,$3,$4,$5)`, out.ID, p.PhysicalPortID, peer, p.CableID, networkJSON(p)); e != nil {
			return nil, e
		}
	}
	// A physical observation stage does not change the inventory version or claim
	// Mapping publication. G3 alone advances lldp to mapped.
	if _, e = tx.ExecContext(ctx, `UPDATE project_assets SET state='lldp',updated_at=clock_timestamp() WHERE id=$1 AND project_id=$2 AND state='collected'`, in.AssetID, project); e != nil {
		return nil, e
	}
	if e = appendAudit(ctx, tx, a.User.ID, project, "wiring.confirm", out.ID, trace, map[string]any{"batch_id": batch, "asset_id": in.AssetID, "step3_id": three.ID, "source_hash": out.SourceHash, "reason": in.Reason}); e != nil {
		return nil, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO idempotencies(id,actor_id,request_hash,response,created_at) VALUES($1,$2,$3,$4,clock_timestamp())`, identity, a.User.ID, requestHash, networkJSON(map[string]string{"id": out.ID})); e != nil {
		return nil, e
	}
	return out, tx.Commit()
}
func (s *Store) wiringConfirmationTx(ctx context.Context, tx *sql.Tx, project, id string) (*WiringConfirmation, error) {
	var out WiringConfirmation
	e := tx.QueryRowContext(ctx, `SELECT id,project_id,batch_id,asset_id,step3_id,step4_id,command_id,source_hash,scope_hash,valid_until,created_by,created_at,reason FROM validation_wiring_confirmations WHERE project_id=$1 AND id=$2`, project, id).Scan(&out.ID, &out.ProjectID, &out.BatchID, &out.AssetID, &out.Step3ID, &out.Step4ID, &out.CommandID, &out.SourceHash, &out.ScopeHash, &out.ValidUntil, &out.CreatedBy, &out.CreatedAt, &out.Reason)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if e != nil {
		return nil, e
	}
	out.Ports = []wiring.ExpectedPort{}
	rows, e := tx.QueryContext(ctx, `SELECT expected FROM validation_wiring_confirmed_ports WHERE confirmation_id=$1 ORDER BY physical_port_id`, id)
	if e != nil {
		return nil, e
	}
	for rows.Next() {
		var raw []byte
		var p wiring.ExpectedPort
		if e = rows.Scan(&raw); e != nil {
			rows.Close()
			return nil, e
		}
		if e = json.Unmarshal(raw, &p); e != nil {
			rows.Close()
			return nil, e
		}
		out.Ports = append(out.Ports, p)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	out.EffectiveVerdict = "PASS"
	_, _, c, _, validErr := s.wiringConfirmableTx(ctx, tx, project, out.BatchID, out.AssetID, out.Step3ID)
	if validErr != nil {
		var domain *Error
		if errors.As(validErr, &domain) && (domain.Status == 409 || domain.Status == 404) {
			out.EffectiveVerdict = "BLOCKED"
			out.InvalidationReason = domain.Code
		} else {
			return nil, validErr
		}
	} else if len(out.Ports) != len(c.Expectation.Ports) {
		out.EffectiveVerdict = "BLOCKED"
		out.InvalidationReason = "WIRING_CONFIRMATION_INCOMPLETE"
	}
	if !out.ValidUntil.After(time.Now()) {
		out.EffectiveVerdict = "BLOCKED"
		out.InvalidationReason = "WIRING_CONFIRMATION_EXPIRED"
	}
	return &out, nil
}
func (s *Store) GetWiringConfirmation(ctx context.Context, a *AuthSession, project, id string) (*WiringConfirmation, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = wiringScopePermissionTx(ctx, tx, a, project, false); e != nil {
		return nil, e
	}
	out, e := s.wiringConfirmationTx(ctx, tx, project, id)
	if e != nil {
		return nil, e
	}
	return out, tx.Commit()
}

func (s *Store) wiringObservedScopeTx(ctx context.Context, tx *sql.Tx, project, commandID string) (*WiringCommand, *WiringFacts, error) {
	return s.wiringObservedScopeCheckTx(ctx, tx, project, commandID, false)
}
func (s *Store) wiringObservedScopeCheckTx(ctx context.Context, tx *sql.Tx, project, commandID string, ignoreIssues bool) (*WiringCommand, *WiringFacts, error) {
	var e error
	var newer bool
	if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM validation_wiring_commands old JOIN validation_wiring_capture_records original ON original.command_id=old.command_id JOIN validation_wiring_commands n ON n.project_id=old.project_id AND n.asset_id=old.asset_id AND n.command_id<>old.command_id JOIN validation_wiring_capture_records recent ON recent.command_id=n.command_id WHERE old.command_id=$1 AND n.command->>'host_origin'='real' AND n.origin='device' AND recent.stopped_at>original.stopped_at AND EXISTS(SELECT 1 FROM jsonb_array_elements(old.command#>'{expectation,ports}') a JOIN jsonb_array_elements(n.command#>'{expectation,ports}') b ON a->>'physical_port_id'=b->>'physical_port_id'))`, commandID).Scan(&newer); e != nil {
		return nil, nil, e
	}
	if newer {
		return nil, nil, apiError(409, "WIRING_OBSERVATION_SUPERSEDED", "A later real observation overlaps this scope; confirm the current result")
	}
	var raw []byte
	e = tx.QueryRowContext(ctx, `SELECT c.command FROM validation_wiring_commands c JOIN exec_task_attempts a ON a.id=c.attempt_id WHERE c.command_id=$1 AND c.project_id=$2 AND a.command_id=c.command_id AND a.fencing_token=c.fencing_token AND a.state='success'`, commandID, project).Scan(&raw)
	if e != nil {
		return nil, nil, e
	}
	var c WiringCommand
	if json.Unmarshal(raw, &c) != nil || c.HostOrigin != "real" || c.Binding.Origin != "device" {
		return nil, nil, apiError(409, "WIRING_REAL_EVIDENCE_REQUIRED", "Real host and authenticated device evidence required")
	}
	scope, e := loadWiringRunScopeTx(ctx, tx, c.Binding.ExecutionID, c.Expectation.MachineID)
	if e != nil {
		return nil, nil, e
	}
	inputs := []WiringTargetInput{{AssetID: c.AssetID, PhysicalPortIDs: []string{}}}
	for _, p := range c.Expectation.Ports {
		inputs[0].PhysicalPortIDs = append(inputs[0].PhysicalPortIDs, p.PhysicalPortID)
	}
	facts, e := s.wiringFactsTx(ctx, tx, project, inputs, false)
	if e != nil {
		return nil, nil, e
	}
	if !facts.Ready || wiringIdentityHash(facts) != scope.IdentityHash {
		return nil, nil, apiError(409, "WIRING_SCOPE_CHANGED", "Current topology, BOM or network prerequisites differ from the observed scope")
	}
	var open bool
	if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM validation_wiring_issue_bindings b JOIN issues i ON i.id=b.issue_id WHERE b.project_id=$1 AND b.asset_id=$2 AND b.host_origin='real' AND b.switch_origin='device' AND i.state<>'closed' AND b.physical_port_id IN(SELECT p->>'physical_port_id' FROM jsonb_array_elements($3::jsonb) p))`, project, c.AssetID, networkJSON(c.Expectation.Ports)).Scan(&open); e != nil {
		return nil, nil, e
	}
	if open && !ignoreIssues {
		return nil, nil, apiError(409, "WIRING_OPEN_ISSUES", "Resolve and explicitly close affected wiring issues before G2 confirmation")
	}
	return &c, facts, nil
}
