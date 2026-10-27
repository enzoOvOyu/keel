package execution

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"time"

	"github.com/enzoOvOyu/keel/internal/fabricboot"
	"github.com/enzoOvOyu/keel/internal/platform"
)

func (s *Service) CreateIssueVerification(ctx context.Context, a *Actor, project, issue string, in platform.IssueVerificationInput, key, trace string) (*Execution, error) {
	if s.IssueStore == nil || !s.IssueStore.FabricIssueVerificationEnabled {
		return nil, failure(409, "ISSUE_VERIFICATION_DISABLED", "Scoped issue verification is disabled")
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = advisory(ctx, tx, "keel-admin-initialization"); e != nil {
		return nil, e
	}
	if e = authorize(ctx, tx, a, project, "Admin", "DL", "NE"); e != nil {
		return nil, e
	}
	// Check the current issue assignment even for an idempotency replay.
	var assignee string
	if e = tx.QueryRowContext(ctx, "SELECT coalesce(assignee_id,'') FROM issues WHERE id=$1 AND project_id=$2", issue, project).Scan(&assignee); e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return nil, platform.ErrNotFound
		}
		return nil, e
	}
	if !a.User.IsAdmin {
		var role string
		if e = tx.QueryRowContext(ctx, "SELECT role FROM memberships WHERE user_id=$1 AND project_id=$2", a.User.ID, project).Scan(&role); e != nil {
			return nil, e
		}
		if role == "NE" && assignee != a.User.ID {
			return nil, platform.ErrForbidden
		}
	}
	idem, prior, e := idempotency(ctx, tx, a, "issue.verify:"+issue, key, in)
	if e != nil {
		return nil, e
	}
	if prior != nil {
		out, e := decode[Execution](prior)
		return &out, e
	}
	scope, e := s.IssueStore.PrepareIssueVerificationTx(ctx, tx, a, project, issue, in)
	if e != nil {
		return nil, e
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	flowID, versionID, requestID := platform.RandomID(), platform.RandomID(), platform.RandomID()
	task := Task{ID: "issue-verify", Type: "issue.verify", Retry: 0, Idempotent: false, TimeoutMS: 45000, Resources: []string{"ssh"}}
	definition := Definition{APIVersion: "keel.issue-verify/v1", Inputs: map[string]Input{}, Tasks: []Task{task}}
	flow := FlowVersion{ID: versionID, FlowID: flowID, ProjectID: project, Name: "Issue verification " + requestID, Version: 1, Definition: definition, ContentHash: hash(jsonBytes(definition)), CreatedAt: now}
	if _, e = tx.ExecContext(ctx, `INSERT INTO exec_flows(id,project_id,name,created_by,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$5)`, flowID, project, flow.Name, a.User.ID, now); e != nil {
		return nil, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO exec_flow_versions(id,flow_id,project_id,version,definition,content_hash,created_by,created_at) VALUES($1,$2,$3,1,$4,$5,$6,$7)`, versionID, flowID, project, jsonBytes(definition), flow.ContentHash, a.User.ID, now); e != nil {
		return nil, e
	}
	// One orchestration task owns the complete link; its resource list is loaded
	// from the immutable request, never from arbitrary browser resource strings.
	var targetID string
	if e = tx.QueryRowContext(ctx, `INSERT INTO exec_targets(id,project_id,kind,resource_key) VALUES($1,$2,'switch',$3) ON CONFLICT(project_id,kind,resource_key) DO UPDATE SET resource_key=EXCLUDED.resource_key RETURNING id`, platform.RandomID(), project, scope.Targets[0].SwitchID).Scan(&targetID); e != nil {
		return nil, e
	}
	target := Target{ID: targetID, ProjectID: project, Name: "Issue link " + scope.CableUID, Revision: 1, ModelVersion: "issue-link/v1", Mode: "fabric", ResourceKey: scope.Targets[0].SwitchID, CreatedAt: now}
	params := map[string]any{"issue_verification_id": requestID, "issue_id": issue, "scope_hash": scope.ScopeHash, "scope": scope}
	targetHash := hash(jsonBytes(scope.Targets))
	out := &Execution{ID: platform.RandomID(), ProjectID: project, FlowVersionID: versionID, Mode: "fabric", State: "created", StateVersion: 1, DesiredState: "running", Parameters: params, TargetSetHash: targetHash, Concurrency: 1, CircuitBreakerPercent: 100, CreatedBy: a.User.ID, CreatedAt: now}
	out.Snapshot = Snapshot{Mode: "fabric", Flow: flow, Targets: []Target{target}, Parameters: params, TargetSetHash: targetHash, Admission: "Scoped N-S5 T1/T2 read-only verification; immutable issue/repair/BOM/origin, both switch resources, no G2/NP-3 or production configuration mutation", FrozenAt: now}
	if _, e = tx.ExecContext(ctx, `INSERT INTO exec_executions(id,project_id,flow_version_id,mode,parameters,snapshot,target_set_hash,concurrency,circuit_breaker_percent,created_by,created_at) VALUES($1,$2,$3,'fabric',$4,$5,$6,1,100,$7,$8)`, out.ID, project, versionID, jsonBytes(params), jsonBytes(out.Snapshot), targetHash, a.User.ID, now); e != nil {
		return nil, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO exec_execution_targets(execution_id,target_id,snapshot) VALUES($1,$2,$3)`, out.ID, target.ID, jsonBytes(target)); e != nil {
		return nil, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO exec_task_attempts(id,execution_id,target_id,task_id,task_index,attempt_no,command_id,runner,created_at) VALUES($1,$2,$3,'issue-verify',0,1,$4,'fabric',$5)`, platform.RandomID(), out.ID, target.ID, platform.RandomID(), now); e != nil {
		return nil, e
	}
	if e = s.IssueStore.AttachIssueVerificationTx(ctx, tx, a, requestID, out.ID, scope, in, trace); e != nil {
		return nil, e
	}
	if e = event(ctx, tx, out.ID, "execution.created", map[string]any{"mode": "fabric", "issue_id": issue, "request_id": requestID}); e != nil {
		return nil, e
	}
	if e = audit(ctx, tx, a.User.ID, project, "issue.verification.execute", out.ID, trace, map[string]any{"issue_id": issue, "scope_hash": scope.ScopeHash}); e != nil {
		return nil, e
	}
	if e = remember(ctx, tx, idem, in, out); e != nil {
		return nil, e
	}
	return out, tx.Commit()
}
func (s *Service) runIssueVerificationClaim(ctx context.Context, c *Claim) error {
	if s.IssueStore == nil {
		return errors.New("issue verification runner disabled")
	}
	work, cancel := context.WithTimeout(ctx, time.Duration(c.Task.TimeoutMS)*time.Millisecond)
	defer cancel()
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(c.LeaseDuration / 3)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				beat, done := context.WithTimeout(context.Background(), c.LeaseDuration/2)
				kill, e := s.Heartbeat(beat, c)
				done()
				if e != nil || kill {
					cancel()
				}
			}
		}
	}()
	defer func() { close(stop); wg.Wait() }()
	id, _ := c.Parameters["issue_verification_id"].(string)
	started := time.Now().UTC()
	op, response, e := s.IssueStore.RunIssueVerification(work, id, c.Attempt.ExecutionID, c.Attempt.CommandID, c.Attempt.FencingToken)
	var unknown *fabricboot.RecollectionUnknown
	if errors.As(e, &unknown) || op == nil {
		return e
	} // Unknown reads keep both resources quarantined.
	code := "RECOLLECTION_COMPLETED"
	result := "success"
	if e != nil {
		code = "RECOLLECTION_PRESTART_REJECTED"
		result = "failed"
	}
	finish, done := context.WithTimeout(context.Background(), 5*time.Second)
	defer done()
	tx, e := s.DB.BeginTx(finish, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = advisory(finish, tx, "keel-admin-initialization"); e != nil {
		return e
	}
	proof, digest, e := s.IssueStore.PersistIssueVerificationEvidenceTx(finish, tx, id, c.Attempt.ExecutionID, c.Attempt.CommandID, c.Attempt.FencingToken, op, response, code)
	if e != nil {
		return e
	}
	verdict, e := s.IssueStore.IssueVerificationReceiptVerdictTx(finish, tx, id, c.Attempt.ExecutionID, *op, response, code)
	if e != nil {
		return e
	}
	if verdict != "PASS" {
		result = "failed"
	}
	receipt := Receipt{CommandID: c.Attempt.CommandID, AttemptID: c.Attempt.ID, FencingToken: c.Attempt.FencingToken, Result: result, StartedAt: started, FinishedAt: time.Now().UTC(), Output: map[string]any{"issue_verification_id": id, "evidence_id": proof, "evidence_hash": digest, "operation": "bounded_recollection", "verification_verdict": verdict, "evidence_source": op.EvidenceSource}}
	if e = s.recordReceiptTx(finish, tx, receipt); e != nil {
		return e
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	return s.Complete(finish, c)
}
func (s *Service) applyIssueVerificationReceipt(ctx context.Context, tx *sql.Tx, a Attempt, r Receipt) error {
	var id string
	e := tx.QueryRowContext(ctx, `SELECT coalesce(parameters->>'issue_verification_id','') FROM exec_executions WHERE id=$1`, a.ExecutionID).Scan(&id)
	if e != nil || id == "" {
		return e
	}
	if s.IssueStore == nil {
		return failure(409, "ISSUE_VERIFICATION_DISABLED", "Verification evidence consumer is disabled")
	}
	owner, _ := r.Output["issue_verification_id"].(string)
	if owner != id {
		return conflict("Receipt belongs to another issue verification")
	}
	proof, _ := r.Output["evidence_id"].(string)
	digest, _ := r.Output["evidence_hash"].(string)
	return s.IssueStore.ApplyIssueVerificationEvidenceTx(ctx, tx, id, a.ExecutionID, a.CommandID, a.FencingToken, proof, digest)
}
func lockIssueVerificationExecutionTx(ctx context.Context, tx *sql.Tx, id string) error {
	var issue bool
	if e := tx.QueryRowContext(ctx, `SELECT parameters ? 'issue_verification_id' FROM exec_executions WHERE id=$1`, id).Scan(&issue); e != nil {
		return e
	}
	if issue {
		return advisory(ctx, tx, "keel-admin-initialization")
	}
	return nil
}
