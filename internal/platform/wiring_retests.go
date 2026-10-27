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

type WiringRetestInput struct {
	Version  int    `json:"version"`
	ReportID string `json:"report_id"`
	Reason   string `json:"reason"`
}
type WiringRetestScope struct {
	IssueID        string     `json:"issue_id"`
	IssueVersion   int        `json:"issue_version"`
	RepairVersion  int        `json:"repair_version"`
	RepairedAt     *time.Time `json:"repaired_at"`
	AssetID        string     `json:"asset_id"`
	PhysicalPortID string     `json:"physical_port_id"`
	BatchID        string     `json:"batch_id"`
	ReportID       string     `json:"report_id"`
	CommandID      string     `json:"command_id"`
	ExecutionID    string     `json:"execution_id"`
	Verdict        string     `json:"verdict"`
	Ready          bool       `json:"ready"`
	Blockers       []string   `json:"blockers"`
}
type WiringRetest struct {
	ID                  string    `json:"id"`
	IssueID             string    `json:"issue_id"`
	ProjectID           string    `json:"project_id"`
	ReportID            string    `json:"report_id"`
	CommandID           string    `json:"command_id"`
	ExecutionID         string    `json:"execution_id"`
	RepairVersion       int       `json:"repair_version"`
	AppliedIssueVersion int       `json:"applied_issue_version"`
	Verdict             string    `json:"verdict"`
	Reason              string    `json:"reason"`
	CreatedBy           string    `json:"created_by"`
	CreatedAt           time.Time `json:"created_at"`
	Stale               bool      `json:"stale"`
	CanClose            bool      `json:"can_close"`
	Escalated           bool      `json:"escalated"`
	RecipientIDs        []string  `json:"recipient_ids"`
}

func (s *Store) wiringRetestScopeTx(ctx context.Context, tx *sql.Tx, item *Issue, role, actor string) (*WiringRetestScope, error) {
	out := &WiringRetestScope{IssueID: item.ID, IssueVersion: item.Version, Ready: true, Blockers: []string{}}
	block := func(code string) { out.Ready = false; out.Blockers = append(out.Blockers, code) }
	if !canConfirmIssueVerification(item, role, actor) {
		block("WIRING_RETEST_AUTHORITY_REQUIRED")
	}
	if !s.WiringEnabled {
		block("WIRING_DISABLED")
	}
	if item.Source != "g2" {
		block("WIRING_ISSUE_REQUIRED")
		return out, nil
	}
	if item.State != "in_repair" {
		block("WIRING_REPAIR_REQUIRED")
	}
	var state string
	if e := tx.QueryRowContext(ctx, `SELECT state FROM projects WHERE id=$1`, item.ProjectID).Scan(&state); e != nil {
		return nil, e
	}
	if state == "archived" || state == "delivered" {
		block("PROJECT_READ_ONLY")
	}
	var hostOrigin, switchOrigin string
	e := tx.QueryRowContext(ctx, `SELECT asset_id,physical_port_id,host_origin,switch_origin FROM validation_wiring_issue_bindings WHERE issue_id=$1 AND project_id=$2`, item.ID, item.ProjectID).Scan(&out.AssetID, &out.PhysicalPortID, &hostOrigin, &switchOrigin)
	if errors.Is(e, sql.ErrNoRows) {
		block("WIRING_DISCOVERY_BINDING_REQUIRED")
		return out, nil
	}
	if e != nil {
		return nil, e
	}
	e = tx.QueryRowContext(ctx, `SELECT version,created_at FROM issue_events WHERE issue_id=$1 AND action='repair' ORDER BY version DESC LIMIT 1`, item.ID).Scan(&out.RepairVersion, &out.RepairedAt)
	if errors.Is(e, sql.ErrNoRows) {
		block("WIRING_REPAIR_REQUIRED")
		return out, nil
	}
	if e != nil {
		return nil, e
	}
	var consumed bool
	if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM validation_wiring_issue_retests WHERE issue_id=$1 AND repair_version=$2)`, item.ID, out.RepairVersion).Scan(&consumed); e != nil {
		return nil, e
	}
	if consumed {
		block("WIRING_REPAIR_ALREADY_RETESTED")
	}
	var superseded bool
	if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM issue_events WHERE issue_id=$1 AND version>$2 AND action IN('assign','reopen'))`, item.ID, out.RepairVersion).Scan(&superseded); e != nil {
		return nil, e
	}
	if superseded {
		block("WIRING_REPAIR_SCOPE_CHANGED")
	}
	e = tx.QueryRowContext(ctx, `SELECT r.id,r.batch_id,c.command_id,c.execution_id FROM validation_wiring_commands c JOIN validation_wiring_capture_records p ON p.command_id=c.command_id JOIN validation_wiring_step_reports r ON (NOT (r.detail ? 'coverage_id') AND r.detail->>'command_id'=c.command_id) OR EXISTS(SELECT 1 FROM validation_wiring_coverage_snapshots coverage CROSS JOIN LATERAL jsonb_array_elements(coverage.coverage->'proofs') proof WHERE coverage.id=r.detail->>'coverage_id' AND proof->>'command_id'=c.command_id AND proof->>'physical_port_id'=$6) JOIN exec_task_attempts a ON a.id=c.attempt_id WHERE c.project_id=$1 AND c.asset_id=$2 AND c.command->>'host_origin'=$3 AND c.origin=$4 AND c.created_at>$5 AND a.state IN('success','failed') AND a.command_id=c.command_id AND a.fencing_token=c.fencing_token AND EXISTS(SELECT 1 FROM jsonb_array_elements(c.command#>'{expectation,ports}') port WHERE port->>'physical_port_id'=$6) AND r.step IN(2,3) ORDER BY p.stopped_at DESC,c.command_id DESC,r.step DESC,r.version DESC LIMIT 1`, item.ProjectID, out.AssetID, hostOrigin, switchOrigin, out.RepairedAt, out.PhysicalPortID).Scan(&out.ReportID, &out.BatchID, &out.CommandID, &out.ExecutionID)
	if errors.Is(e, sql.ErrNoRows) {
		block("WIRING_POST_REPAIR_CAPTURE_REQUIRED")
		return out, nil
	}
	if e != nil {
		return nil, e
	}
	b, e := s.wiringBatchTx(ctx, tx, item.ProjectID, out.BatchID)
	if e != nil {
		return nil, e
	}
	var found *WiringStepReport
	for i := range b.Targets {
		if b.Targets[i].AssetID == out.AssetID {
			for j := range b.Targets[i].Reports {
				r := &b.Targets[i].Reports[j]
				if r.ID == out.ReportID {
					found = r
				}
			}
		}
	}
	if found == nil {
		block("WIRING_REPORT_REQUIRED")
		return out, nil
	}
	out.Verdict = found.Verdict
	if found.Detail.CoverageID != "" {
		var two, three string
		if e = tx.QueryRowContext(ctx, `SELECT proof->>'step2',proof->>'step3' FROM validation_wiring_coverage_snapshots coverage CROSS JOIN LATERAL jsonb_array_elements(coverage.coverage->'proofs') proof WHERE coverage.id=$1 AND proof->>'physical_port_id'=$2 AND proof->>'command_id'=$3`, found.Detail.CoverageID, out.PhysicalPortID, out.CommandID).Scan(&two, &three); e != nil {
			return nil, e
		}
		verdict := two
		if found.Step == 3 {
			verdict = three
		}
		if verdict != found.Verdict {
			out.Verdict = "BLOCKED"
			block("WIRING_RETEST_OTHER_PORTS_BLOCKED")
		}
	}
	if found.EffectiveVerdict != found.Verdict {
		block("WIRING_RETEST_NOT_CURRENT")
	}
	if found.Verdict == "PASS" {
		if found.Step != 3 {
			block("WIRING_STEP3_REQUIRED")
		} else {
			_, _, scopeErr := s.wiringReportScopeTx(ctx, tx, item.ProjectID, found, true)
			if scopeErr != nil {
				var domain *Error
				if errors.As(scopeErr, &domain) && domain.Status == 409 {
					block(domain.Code)
				} else {
					return nil, scopeErr
				}
			}
		}
	}
	return out, nil
}
func (s *Store) PreviewWiringRetest(ctx context.Context, a *AuthSession, issue string) (*WiringRetestScope, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	item, e := scanIssue(tx.QueryRowContext(ctx, `SELECT `+issueColumns+` FROM issues WHERE id=$1`, issue))
	if e != nil {
		return nil, e
	}
	role, e := issueRoleTx(ctx, tx, a, item.ProjectID)
	if e != nil {
		return nil, e
	}
	out, e := s.wiringRetestScopeTx(ctx, tx, item, role, a.User.ID)
	if e != nil {
		return nil, e
	}
	return out, tx.Commit()
}

const wiringRetestFields = `id,issue_id,project_id,report_id,command_id,execution_id,repair_version,applied_issue_version,verdict,reason,created_by,created_at`

func scanWiringRetest(row scanner) (*WiringRetest, error) {
	out := &WiringRetest{RecipientIDs: []string{}}
	e := row.Scan(&out.ID, &out.IssueID, &out.ProjectID, &out.ReportID, &out.CommandID, &out.ExecutionID, &out.RepairVersion, &out.AppliedIssueVersion, &out.Verdict, &out.Reason, &out.CreatedBy, &out.CreatedAt)
	return out, e
}
func (s *Store) CreateWiringRetest(ctx context.Context, a *AuthSession, issue string, in WiringRetestInput, key, trace string) (*WiringRetest, error) {
	in.Reason = strings.TrimSpace(in.Reason)
	if in.Version < 1 || !validEntityID(in.ReportID) || utf8.RuneCountInString(in.Reason) < 3 || utf8.RuneCountInString(in.Reason) > 1000 || strings.TrimSpace(key) == "" || len(key) > 128 {
		return nil, apiError(400, "VALIDATION_ERROR", "Current issue version, report, reason and Idempotency-Key required")
	}
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	item, e := scanIssue(tx.QueryRowContext(ctx, `SELECT `+issueColumns+` FROM issues WHERE id=$1 FOR UPDATE`, issue))
	if e != nil {
		return nil, e
	}
	role, e := issueRoleTx(ctx, tx, a, item.ProjectID)
	if e != nil {
		return nil, e
	}
	if !canConfirmIssueVerification(item, role, a.User.ID) {
		return nil, ErrForbidden
	}
	if e = wiringScopePermissionTx(ctx, tx, a, item.ProjectID, true); e != nil {
		return nil, e
	}
	identity := digest("wiring.retest:" + issue + ":" + a.User.ID + ":" + key)
	requestHash := digest(string(networkJSON(in)))
	if e = lockKey(ctx, tx, identity); e != nil {
		return nil, e
	}
	prior, e := priorWiringHandshakeTx(ctx, tx, identity, requestHash)
	if e != nil {
		return nil, e
	}
	if prior != "" {
		out, e := s.wiringRetestTx(ctx, tx, item, role, a.User.ID, prior)
		if e != nil {
			return nil, e
		}
		return out, tx.Commit()
	}
	if item.Version != in.Version {
		return nil, errVersion
	}
	scope, e := s.wiringRetestScopeTx(ctx, tx, item, role, a.User.ID)
	if e != nil {
		return nil, e
	}
	if !scope.Ready || scope.ReportID != in.ReportID {
		return nil, apiError(409, "WIRING_RETEST_BLOCKED", "Refresh the current post-repair wiring report and resolve blockers")
	}
	state := "in_repair"
	if scope.Verdict == "PASS" {
		state = "verify"
	}
	item, e = scanIssue(tx.QueryRowContext(ctx, `UPDATE issues SET state=$2,version=version+1,updated_at=clock_timestamp() WHERE id=$1 RETURNING `+issueColumns, item.ID, state))
	if e != nil {
		return nil, e
	}
	id := RandomID()
	_, e = tx.ExecContext(ctx, `INSERT INTO validation_wiring_issue_retests(id,issue_id,project_id,report_id,command_id,execution_id,repair_version,applied_issue_version,verdict,reason,created_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, id, issue, item.ProjectID, scope.ReportID, scope.CommandID, scope.ExecutionID, scope.RepairVersion, item.Version, scope.Verdict, in.Reason, a.User.ID)
	if e != nil {
		return nil, e
	}
	if e = issueEventTx(ctx, tx, a, item, "wiring_retest", trace, map[string]any{"retest_id": id, "report_id": scope.ReportID, "execution_id": scope.ExecutionID, "repair_version": scope.RepairVersion, "verdict": scope.Verdict, "reason": in.Reason}); e != nil {
		return nil, e
	}
	if scope.Verdict == "FAIL" {
		if e = wiringRetestEscalationTx(ctx, tx, a, id, issue, item.ProjectID, scope.ExecutionID, trace); e != nil {
			return nil, e
		}
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO idempotencies(id,actor_id,request_hash,response,created_at) VALUES($1,$2,$3,$4,clock_timestamp())`, identity, a.User.ID, requestHash, networkJSON(map[string]string{"id": id})); e != nil {
		return nil, e
	}
	out, e := s.wiringRetestTx(ctx, tx, item, role, a.User.ID, id)
	if e != nil {
		return nil, e
	}
	return out, tx.Commit()
}

func (s *Store) wiringCloseEvidenceTx(ctx context.Context, tx *sql.Tx, item *Issue) (*IssueVerificationRequest, error) {
	retest, e := scanWiringRetest(tx.QueryRowContext(ctx, `SELECT `+wiringRetestFields+` FROM validation_wiring_issue_retests WHERE issue_id=$1 AND applied_issue_version=$2 AND verdict='PASS' ORDER BY created_at DESC,id DESC LIMIT 1`, item.ID, item.Version))
	if errors.Is(e, sql.ErrNoRows) {
		return nil, apiError(409, "WIRING_REVALIDATION_REQUIRED", "A current successful post-repair wiring retest is required")
	}
	if e != nil {
		return nil, e
	}
	var batch, asset, hostOrigin, switchOrigin string
	e = tx.QueryRowContext(ctx, `SELECT c.batch_id,c.asset_id,b.host_origin,b.switch_origin FROM validation_wiring_commands c JOIN validation_wiring_issue_bindings b ON b.issue_id=$1 AND b.asset_id=c.asset_id AND b.project_id=c.project_id WHERE c.command_id=$2 AND c.command->>'host_origin'=b.host_origin AND c.origin=b.switch_origin AND EXISTS(SELECT 1 FROM jsonb_array_elements(c.command#>'{expectation,ports}') p WHERE p->>'physical_port_id'=b.physical_port_id)`, item.ID, retest.CommandID).Scan(&batch, &asset, &hostOrigin, &switchOrigin)
	if e != nil {
		return nil, e
	}
	if hostOrigin != "real" || switchOrigin != "device" {
		return nil, apiError(409, "WIRING_REAL_EVIDENCE_REQUIRED", "Simulation cannot close a real physical wiring verification")
	}
	var repair, assignment int
	if e = tx.QueryRowContext(ctx, `SELECT coalesce(max(version) FILTER(WHERE action='repair'),0),coalesce(max(version) FILTER(WHERE action IN('assign','reopen')),0) FROM issue_events WHERE issue_id=$1`, item.ID).Scan(&repair, &assignment); e != nil {
		return nil, e
	}
	if repair != retest.RepairVersion || assignment > repair {
		return nil, apiError(409, "WIRING_REPAIR_SCOPE_CHANGED", "Repair or assignment changed after the retest")
	}
	b, e := s.wiringBatchTx(ctx, tx, item.ProjectID, batch)
	if e != nil {
		return nil, e
	}
	var latest *WiringStepReport
	for i := range b.Targets {
		if b.Targets[i].AssetID == asset {
			latest = latestWiringReport(&b.Targets[i], 3)
		}
	}
	if latest == nil || latest.ID != retest.ReportID || latest.EffectiveVerdict != "PASS" {
		return nil, apiError(409, "WIRING_RETEST_NOT_CURRENT", "The post-repair three-source report is no longer current")
	}
	c, _, e := s.wiringReportScopeTx(ctx, tx, item.ProjectID, latest, true)
	if e != nil {
		return nil, e
	}
	return &IssueVerificationRequest{ID: retest.ID, IssueID: item.ID, ProjectID: item.ProjectID, ExecutionID: retest.ExecutionID, ScopeHash: c.Expectation.ScopeHash}, nil
}
func (s *Store) wiringRetestTx(ctx context.Context, tx *sql.Tx, item *Issue, role, actor, id string) (*WiringRetest, error) {
	out, e := scanWiringRetest(tx.QueryRowContext(ctx, `SELECT `+wiringRetestFields+` FROM validation_wiring_issue_retests WHERE id=$1 AND issue_id=$2`, id, item.ID))
	if e != nil {
		return nil, e
	}
	out.Stale = out.AppliedIssueVersion != item.Version
	var recipients []byte
	e = tx.QueryRowContext(ctx, `SELECT recipient_ids FROM validation_wiring_issue_escalations WHERE retest_id=$1`, id).Scan(&recipients)
	if e == nil {
		out.Escalated = true
		if e = json.Unmarshal(recipients, &out.RecipientIDs); e != nil {
			return nil, e
		}
	} else if !errors.Is(e, sql.ErrNoRows) {
		return nil, e
	}
	if out.Verdict == "PASS" && !out.Stale {
		_, e = s.wiringCloseEvidenceTx(ctx, tx, item)
		if e == nil {
			out.CanClose = item.State == "verify" && canConfirmIssueVerification(item, role, actor)
		} else {
			var domain *Error
			if errors.As(e, &domain) && (domain.Status == 409 || domain.Status == 404) {
				out.Stale = true
			} else {
				return nil, e
			}
		}
	}
	return out, nil
}
func (s *Store) ListWiringRetests(ctx context.Context, a *AuthSession, issue string, page Pagination) (Page[WiringRetest], error) {
	out := NewPage[WiringRetest](page)
	if e := page.Validate(); e != nil {
		return out, e
	}
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	item, e := scanIssue(tx.QueryRowContext(ctx, `SELECT `+issueColumns+` FROM issues WHERE id=$1`, issue))
	if e != nil {
		return out, e
	}
	role, e := issueRoleTx(ctx, tx, a, item.ProjectID)
	if e != nil {
		return out, e
	}
	if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM validation_wiring_issue_retests WHERE issue_id=$1`, issue).Scan(&out.Total); e != nil {
		return out, e
	}
	rows, e := tx.QueryContext(ctx, `SELECT id FROM validation_wiring_issue_retests WHERE issue_id=$1 ORDER BY created_at DESC,id DESC LIMIT $2 OFFSET $3`, issue, page.PageSize, page.Offset())
	if e != nil {
		return out, e
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return out, e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	for _, id := range ids {
		v, err := s.wiringRetestTx(ctx, tx, item, role, a.User.ID, id)
		if err != nil {
			return out, err
		}
		out.Items = append(out.Items, *v)
	}
	return out, tx.Commit()
}
