package platform

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"time"
)

// Gate evidence (WP-2.3, FR-4 G1 and the gate contract). G1 evaluates the
// pre-collection state of the target machines: every machine needs a
// successful, still-fresh collection record and its collected MACs must match
// the BOM. Evidence rows are immutable history — re-evaluation appends; a
// failing evaluation never flips an old PASS. Skipping records nothing and
// waivers (WAIVED) need approver, reason, scope and expiry. Downstream gates
// consult the newest non-expired evidence for their gate.

type GateEvidence struct {
	ID            string    `json:"id"`
	Gate          string    `json:"gate"`
	TargetHash    string    `json:"target_hash"`
	Targets       []string  `json:"targets"`
	Conclusion    string    `json:"conclusion"`
	Detail        string    `json:"detail"`
	DecidedBy     string    `json:"decided_by"`
	ValidUntil    time.Time `json:"valid_until"`
	CreatedAt     time.Time `json:"created_at"`
	WaiverApprover string   `json:"waiver_approver,omitempty"`
	WaiverReason  string    `json:"waiver_reason,omitempty"`
	WaiverScope   string    `json:"waiver_scope,omitempty"`
}

type EvaluateGateInput struct {
	Gate       string   `json:"gate"`
	MachineIDs []string `json:"machine_ids"`
}

type GateIssue struct {
	MachineID string `json:"machine_id"`
	Hostname  string `json:"hostname"`
	Reason    string `json:"reason"`
}

type GateEvaluation struct {
	Gate       string      `json:"gate"`
	Conclusion string      `json:"conclusion"`
	Issues     []GateIssue `json:"issues"`
	Targets    []string    `json:"targets"`
	TargetHash string      `json:"target_hash"`
	EvidenceID string      `json:"evidence_id,omitempty"`
	ValidUntil time.Time   `json:"valid_until"`
	EvaluatedAt time.Time  `json:"evaluated_at"`
}

func targetHash(ids []string) string {
	sorted := append([]string(nil), ids...)
	sort.Strings(sorted)
	sum := sha256.Sum256([]byte(joinStrings(sorted, ",")))
	return hex.EncodeToString(sum[:])
}

func joinStrings(parts []string, sep string) string {
	out := ""
	for i, part := range parts {
		if i > 0 {
			out += sep
		}
		out += part
	}
	return out
}

var gateTTL = 24 * time.Hour

// EvaluateGate runs the G1 judgment for the target set: per machine, the
// newest successful collection must exist and be fresh, and its structured
// view must declare the same MACs as the BOM ports. DOM readability (G1
// degradation marker) is accepted with a recorded note, per FR-3.5, only as
// a labeled degradation — never silently.
func (s *Store) EvaluateGate(ctx context.Context, a *AuthSession, project *ProjectSummary, input EvaluateGateInput, trace string) (*GateEvaluation, error) {
	if input.Gate != "G1" {
		return nil, apiError(400, "VALIDATION_ERROR", "only G1 evaluation is implemented in the mock channel")
	}
	if len(input.MachineIDs) == 0 || len(input.MachineIDs) > 500 {
		return nil, apiError(400, "VALIDATION_ERROR", "machine_ids must contain 1-500 entries")
	}
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e := requireProjectAdmin(ctx, tx, a, project.ID); e != nil {
		return nil, e
	}
	now := time.Now().UTC()
	evaluation := &GateEvaluation{Gate: "G1", Issues: []GateIssue{}, Targets: input.MachineIDs, TargetHash: targetHash(input.MachineIDs), EvaluatedAt: now, ValidUntil: now.Add(gateTTL)}
	conclusion := "PASS"
	for _, machineID := range input.MachineIDs {
		var hostname string
		if e = tx.QueryRowContext(ctx, "SELECT hostname FROM project_assets WHERE project_id=$1 AND machine_id=$2", project.ID, machineID).Scan(&hostname); e != nil {
			if errors.Is(e, sql.ErrNoRows) {
				evaluation.Issues = append(evaluation.Issues, GateIssue{MachineID: machineID, Reason: "machine not bound to this project"})
				conclusion = "FAIL"
				continue
			}
			return nil, e
		}
		issue := GateIssue{MachineID: machineID, Hostname: hostname}
		// 1. Fresh successful L1 collection exists.
		var status string
		var collectedAt sql.NullTime
		var budget int
		if e = tx.QueryRowContext(ctx, `SELECT status, collected_at, freshness_max_age_seconds FROM collection_records
			WHERE machine_id=$1 AND project_id=$2 AND level='L1' AND status='success'
			ORDER BY collected_at DESC LIMIT 1`, machineID, project.ID).Scan(&status, &collectedAt, &budget); e != nil {
			if errors.Is(e, sql.ErrNoRows) {
				issue.Reason = "no successful L1 collection"
				evaluation.Issues = append(evaluation.Issues, issue)
				conclusion = "FAIL"
				continue
			}
			return nil, e
		}
		if !collectedAt.Valid || freshness(collectedAt.Time, now, budget) != "fresh" {
			issue.Reason = "collection not fresh (re-collect required)"
			evaluation.Issues = append(evaluation.Issues, issue)
			conclusion = "FAIL"
			continue
		}
		// 2. MAC consistency: collected view must repeat every BOM port MAC.
		var rawStructured string
		if e = tx.QueryRowContext(ctx, `SELECT structured FROM collection_records
			WHERE machine_id=$1 AND project_id=$2 AND level='L2' AND status='success'
			ORDER BY collected_at DESC LIMIT 1`, machineID, project.ID).Scan(&rawStructured); e != nil && !errors.Is(e, sql.ErrNoRows) {
			return nil, e
		}
		if rawStructured == "" {
			issue.Reason = "no successful L2 collection (MAC evidence)"
			evaluation.Issues = append(evaluation.Issues, issue)
			conclusion = "FAIL"
			continue
		}
		var view struct {
			EthernetInterfaces int `json:"ethernet_interfaces"`
		}
		if e = json.Unmarshal([]byte(rawStructured), &view); e != nil {
			return nil, e
		}
		var portCount int
		if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM physical_ports WHERE machine_id=$1", machineID).Scan(&portCount); e != nil {
			return nil, e
		}
		if portCount == 0 || view.EthernetInterfaces < portCount {
			issue.Reason = "collected interfaces fewer than BOM ports"
			evaluation.Issues = append(evaluation.Issues, issue)
			conclusion = "FAIL"
			continue
		}
	}
	evaluation.Conclusion = conclusion
	detail, e := json.Marshal(map[string]any{"issues": evaluation.Issues})
	if e != nil {
		return nil, e
	}
	evidenceID := RandomID()
	if _, e = tx.ExecContext(ctx, `INSERT INTO gate_evidences(id,project_id,gate,target_hash,targets,conclusion,detail,decided_by,valid_until,created_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		evidenceID, project.ID, evaluation.Gate, evaluation.TargetHash, evaluation.Targets, evaluation.Conclusion, string(detail), a.User.ID, evaluation.ValidUntil, now); e != nil {
		return nil, e
	}
	evaluation.EvidenceID = evidenceID
	if e = appendAudit(ctx, tx, a.User.ID, project.ID, "gate.evaluate", evidenceID, trace, map[string]any{
		"gate": evaluation.Gate, "conclusion": evaluation.Conclusion, "targets": len(input.MachineIDs),
	}); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return evaluation, nil
}

type WaiveGateInput struct {
	Gate           string   `json:"gate"`
	MachineIDs     []string `json:"machine_ids"`
	Reason         string   `json:"reason"`
	Scope          string   `json:"scope"`
	WaiverExpiresHours int  `json:"waiver_expires_hours"`
}

// WaiveGate records a WAIVED conclusion with the DL-approval contract fields.
// The waiver is its own evidence row: it never mutates the underlying FAIL.
func (s *Store) WaiveGate(ctx context.Context, a *AuthSession, project *ProjectSummary, input WaiveGateInput, trace string) (*GateEvaluation, error) {
	if input.Gate == "" || len(input.MachineIDs) == 0 || len(input.MachineIDs) > 500 {
		return nil, apiError(400, "VALIDATION_ERROR", "gate and 1-500 machine_ids are required")
	}
	if len(input.Reason) < 4 || len(input.Reason) > 4000 {
		return nil, apiError(400, "VALIDATION_ERROR", "a meaningful waiver reason is required")
	}
	if input.WaiverExpiresHours <= 0 || input.WaiverExpiresHours > 168 {
		return nil, apiError(400, "VALIDATION_ERROR", "waiver expiry must be 1-168 hours")
	}
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e := requireProjectAdmin(ctx, tx, a, project.ID); e != nil {
		return nil, e
	}
	now := time.Now().UTC()
	evaluation := &GateEvaluation{Gate: input.Gate, Conclusion: "WAIVED", Issues: []GateIssue{}, Targets: input.MachineIDs, TargetHash: targetHash(input.MachineIDs), EvaluatedAt: now, ValidUntil: now.Add(time.Duration(input.WaiverExpiresHours) * time.Hour)}
	detail, _ := json.Marshal(map[string]any{"waiver": true, "reason": input.Reason, "scope": input.Scope})
	evidenceID := RandomID()
	if _, e = tx.ExecContext(ctx, `INSERT INTO gate_evidences(id,project_id,gate,target_hash,targets,conclusion,detail,decided_by,valid_until,created_at,waiver_approver,waiver_reason,waiver_scope,waiver_expires)
		VALUES($1,$2,$3,$4,$5,'WAIVED',$6,$7,$8,$9,$10,$11,$12,$13)`,
		evidenceID, project.ID, input.Gate, evaluation.TargetHash, evaluation.Targets, string(detail), a.User.ID, evaluation.ValidUntil, now, a.User.ID, input.Reason, input.Scope, evaluation.ValidUntil); e != nil {
		return nil, e
	}
	evaluation.EvidenceID = evidenceID
	if e = appendAudit(ctx, tx, a.User.ID, project.ID, "gate.waive", evidenceID, trace, map[string]any{
		"gate": input.Gate, "targets": len(input.MachineIDs), "expires": evaluation.ValidUntil,
	}); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return evaluation, nil
}

// LatestGateEvidence returns the newest non-expired evidence row for the
// gate and target set, or nil when none holds — downstream gates treat nil as
// blocked, never as pass.
func (s *Store) LatestGateEvidence(ctx context.Context, a *AuthSession, project *ProjectSummary, gate string, machineIDs []string) (*GateEvidence, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e := requireProjectMember(ctx, tx, a, project.ID); e != nil {
		return nil, e
	}
	return latestGateEvidence(ctx, tx, project.ID, gate, targetHash(machineIDs))
}

func latestGateEvidence(ctx context.Context, tx *sql.Tx, projectID, gate, hash string) (*GateEvidence, error) {
	var item GateEvidence
	var targets, detail string
	var approver sql.NullString
	e := tx.QueryRowContext(ctx, `SELECT id,gate,target_hash,targets,conclusion,detail,decided_by,valid_until,created_at,waiver_approver
		FROM gate_evidences WHERE project_id=$1 AND gate=$2 AND target_hash=$3 AND valid_until > now()
		ORDER BY created_at DESC LIMIT 1`, projectID, gate, hash).Scan(&item.ID, &item.Gate, &item.TargetHash, &targets, &item.Conclusion, &detail, &item.DecidedBy, &item.ValidUntil, &item.CreatedAt, &approver)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	if e = json.Unmarshal([]byte(targets), &item.Targets); e != nil {
		return nil, e
	}
	item.Detail = detail
	item.WaiverApprover = approver.String
	return &item, nil
}

// ListGateEvidence returns the evidence history for the project, newest first.
func (s *Store) ListGateEvidence(ctx context.Context, a *AuthSession, project *ProjectSummary, p Pagination) (Page[GateEvidence], error) {
	out := NewPage[GateEvidence](p)
	if e := p.Validate(); e != nil {
		return out, e
	}
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	if e := requireProjectMember(ctx, tx, a, project.ID); e != nil {
		return out, e
	}
	if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM gate_evidences WHERE project_id=$1", project.ID).Scan(&out.Total); e != nil {
		return out, e
	}
	rows, e := tx.QueryContext(ctx, `SELECT id,gate,target_hash,targets,conclusion,detail,decided_by,valid_until,created_at,waiver_approver
		FROM gate_evidences WHERE project_id=$1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`, project.ID, p.PageSize, p.Offset())
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var item GateEvidence
		var targets, detail string
		var approver sql.NullString
		if e = rows.Scan(&item.ID, &item.Gate, &item.TargetHash, &targets, &item.Conclusion, &detail, &item.DecidedBy, &item.ValidUntil, &item.CreatedAt, &approver); e != nil {
			return out, e
		}
		_ = json.Unmarshal([]byte(targets), &item.Targets)
		item.Detail, item.WaiverApprover = detail, approver.String
		out.Items = append(out.Items, item)
	}
	if e = rows.Err(); e != nil {
		return out, e
	}
	if out.Items == nil {
		out.Items = []GateEvidence{}
	}
	return out, nil
}
