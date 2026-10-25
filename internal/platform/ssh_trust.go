package platform

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// SSH trust actions (WP-1.5 mock channel, FR-1.2). The real channel (keel-ctrl
// key, authorized_keys injection, first-connection fingerprint capture) is a
// real-machine deliverable; here the semantics are enforced and audited while
// a simulated runner performs the transitions. State machine: unverified ->
// established (bootstrap or first successful verify); established -> revoked
// (exit); revoked -> established only through an audited trust.repair. The
// first fingerprint is sticky: bootstrap records it, repair may rotate it, and
// verify success against a mismatched fingerprint is a failure.

type TrustActionInput struct {
	MachineIDs  []string `json:"machine_ids"`
	Action      string   `json:"action"`
	Fingerprint string   `json:"fingerprint"`
}

type TrustActionResult struct {
	MachineID   string    `json:"machine_id"`
	Serial      string    `json:"serial_number"`
	BeforeState string    `json:"before_state"`
	AfterState  string    `json:"after_state"`
	Outcome     string    `json:"outcome"`
	Reason      string    `json:"reason,omitempty"`
	CheckedAt   time.Time `json:"checked_at"`
	Source      string    `json:"source"`
}

func (s *Store) RunTrustAction(ctx context.Context, a *AuthSession, project *ProjectSummary, input TrustActionInput, trace string) ([]TrustActionResult, error) {
	switch input.Action {
	case "trust.bootstrap", "trust.verify", "trust.repair", "trust.revoke":
	default:
		return nil, apiError(400, "VALIDATION_ERROR", "action must be trust.bootstrap, trust.verify, trust.repair or trust.revoke")
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
	// A mock transition must never authorize the real transport, including
	// configured targets whose credentials are currently expired.
	for _, machineID := range input.MachineIDs {
		var realTarget bool
		if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM ssh_trust_requests WHERE project_id=$1 AND machine_id=$2) OR EXISTS(SELECT 1 FROM exec_targets WHERE project_id=$1 AND kind='machine' AND resource_key=$2) OR EXISTS(SELECT 1 FROM machines m JOIN project_assets a ON a.machine_id=m.id WHERE a.project_id=$1 AND m.id=$2 AND m.ssh_trust_source='ssh')`, project.ID, machineID).Scan(&realTarget); e != nil {
			return nil, e
		}
		if realTarget || (s.SSHRunner != nil && s.SSHRunner.HasTarget(project.ID, machineID)) {
			return nil, apiError(409, "SSH_TRUST_REAL_VERIFICATION_REQUIRED", "Configured or historical SSH targets require approved, evidence-backed verification")
		}
	}
	results := []TrustActionResult{}
	now := time.Now().UTC()
	for _, machineID := range input.MachineIDs {
		result := TrustActionResult{MachineID: machineID, Outcome: "success", CheckedAt: now, Source: "sim"}
		var serial, trust, fingerprint string
		e := tx.QueryRowContext(ctx, `SELECT m.serial_number, m.ssh_trust, m.host_key_fingerprint
			FROM machines m JOIN project_assets pa ON pa.machine_id=m.id
			WHERE m.id=$1 AND pa.project_id=$2`, machineID, project.ID).Scan(&serial, &trust, &fingerprint)
		if e != nil {
			if errors.Is(e, sql.ErrNoRows) {
				result.Outcome, result.Reason = "failure", "machine not bound to this project"
				results = append(results, result)
				continue
			}
			return nil, e
		}
		result.Serial, result.BeforeState = serial, trust
		switch {
		case input.Action == "trust.bootstrap":
			// Restricted first contact: only allowed while unverified, records
			// the fingerprint, never runs on already-trusted machines.
			if trust != "unverified" {
				result.Outcome, result.Reason = "failure", "trust already "+trust
				break
			}
			if input.Fingerprint == "" || len(input.Fingerprint) > 120 {
				result.Outcome, result.Reason = "failure", "fingerprint required for bootstrap"
				break
			}
			if _, e = tx.ExecContext(ctx, "UPDATE machines SET ssh_trust='established', ssh_trust_source='sim', host_key_fingerprint=$2, trust_updated_at=$3 WHERE id=$1", machineID, input.Fingerprint, now); e != nil {
				return nil, e
			}
			result.AfterState = "established"
		case input.Action == "trust.verify":
			// The simulated runner verifies connectivity. Success on an
			// established machine confirms the recorded fingerprint; success
			// on unverified is the PXE-callback path and establishes trust
			// with the reported fingerprint.
			if trust == "revoked" {
				result.Outcome, result.Reason = "failure", "trust revoked; run trust.repair first"
				break
			}
			if trust == "established" && input.Fingerprint != "" && input.Fingerprint != fingerprint {
				result.Outcome, result.Reason = "failure", "host key fingerprint mismatch"
				break
			}
			newFingerprint := fingerprint
			if newFingerprint == "" {
				if input.Fingerprint == "" {
					result.Outcome, result.Reason = "failure", "fingerprint required for first verification"
					break
				}
				newFingerprint = input.Fingerprint
			}
			if _, e = tx.ExecContext(ctx, "UPDATE machines SET ssh_trust='established', ssh_trust_source='sim', host_key_fingerprint=$2, trust_updated_at=$3 WHERE id=$1", machineID, newFingerprint, now); e != nil {
				return nil, e
			}
			result.AfterState = "established"
		case input.Action == "trust.repair":
			// Re-establishes trust from any state, rotating the fingerprint;
			// the audited controlled-initiation exception in FR-1.2.
			if input.Fingerprint == "" || len(input.Fingerprint) > 120 {
				result.Outcome, result.Reason = "failure", "fingerprint required for repair"
				break
			}
			if _, e = tx.ExecContext(ctx, "UPDATE machines SET ssh_trust='established', ssh_trust_source='sim', host_key_fingerprint=$2, trust_updated_at=$3 WHERE id=$1", machineID, input.Fingerprint, now); e != nil {
				return nil, e
			}
			result.AfterState = "established"
		case input.Action == "trust.revoke":
			// Exit path: platform-side revocation marks the machine
			// untrusted; the actual authorized_keys removal is a real-channel
			// step recorded here as the platform-side half.
			if trust == "revoked" {
				result.Outcome, result.Reason = "failure", "trust already revoked"
				break
			}
			if _, e = tx.ExecContext(ctx, "UPDATE machines SET ssh_trust='revoked', ssh_trust_source='sim', trust_updated_at=$2 WHERE id=$1", machineID, now); e != nil {
				return nil, e
			}
			result.AfterState = "revoked"
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO trust_logs(id,machine_id,project_id,action,outcome,fingerprint,detail,created_by,created_at)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
			RandomID(), machineID, project.ID, input.Action, result.Outcome, input.Fingerprint,
			map[string]any{"reason": result.Reason, "before": result.BeforeState, "after": result.AfterState, "source": "sim", "hardware_verified": false},
			a.User.ID, now); e != nil {
			return nil, e
		}
		results = append(results, result)
	}
	if e = appendAudit(ctx, tx, a.User.ID, project.ID, input.Action, project.ID, trace, map[string]any{
		"machines": len(input.MachineIDs), "successes": countTrustSuccesses(results),
	}); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return results, nil
}

func countTrustSuccesses(results []TrustActionResult) int {
	n := 0
	for _, r := range results {
		if r.Outcome == "success" {
			n++
		}
	}
	return n
}

type TrustLogEntry struct {
	ID          string    `json:"id"`
	MachineID   string    `json:"machine_id"`
	Serial      string    `json:"serial_number"`
	Action      string    `json:"action"`
	Outcome     string    `json:"outcome"`
	Fingerprint string    `json:"fingerprint"`
	CreatedBy   string    `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
}

// ListTrustLogs returns the project's trust history, newest first.
func (s *Store) ListTrustLogs(ctx context.Context, a *AuthSession, project *ProjectSummary, p Pagination) (Page[TrustLogEntry], error) {
	out := NewPage[TrustLogEntry](p)
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
	if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM trust_logs WHERE project_id=$1", project.ID).Scan(&out.Total); e != nil {
		return out, e
	}
	rows, e := tx.QueryContext(ctx, `SELECT tl.id,tl.machine_id,m.serial_number,tl.action,tl.outcome,tl.fingerprint,tl.created_by,tl.created_at
		FROM trust_logs tl JOIN machines m ON m.id=tl.machine_id
		WHERE tl.project_id=$1 ORDER BY tl.created_at DESC, tl.id LIMIT $2 OFFSET $3`, project.ID, p.PageSize, p.Offset())
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var item TrustLogEntry
		if e = rows.Scan(&item.ID, &item.MachineID, &item.Serial, &item.Action, &item.Outcome, &item.Fingerprint, &item.CreatedBy, &item.CreatedAt); e != nil {
			return out, e
		}
		out.Items = append(out.Items, item)
	}
	if e = rows.Err(); e != nil {
		return out, e
	}
	if out.Items == nil {
		out.Items = []TrustLogEntry{}
	}
	return out, nil
}
