package execution

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/enzoOvOyu/keel/internal/fabricboot"
	"github.com/enzoOvOyu/keel/internal/platform"
)

// FetchIssueVerificationReceipt only queries the frozen original collector
// command. The explicit operator action reconciles a proved terminal response;
// neither a timeout, HTTP 404 nor collector 'running' releases resources.
func (s *Service) FetchIssueVerificationReceipt(ctx context.Context, a *Actor, project, id, attemptID string, version int64, key, trace string) (*Execution, error) {
	if version < 1 {
		return nil, invalid("expected_version must be positive")
	}
	if s.IssueStore == nil || !s.IssueStore.FabricIssueVerificationEnabled {
		return nil, failure(409, "ISSUE_VERIFICATION_DISABLED", "Scoped verification recovery is disabled")
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = advisory(ctx, tx, "keel-admin-initialization"); e != nil {
		return nil, e
	}
	if e = authorize(ctx, tx, a, project, "Admin", "DL", "SRE"); e != nil {
		return nil, e
	}
	x, e := scanExecution(tx.QueryRowContext(ctx, "SELECT "+executionColumns+" FROM exec_executions WHERE id=$1 AND project_id=$2", id, project))
	if errors.Is(e, sql.ErrNoRows) {
		return nil, platform.ErrNotFound
	}
	if e != nil {
		return nil, e
	}
	requestID, _ := x.Parameters["issue_verification_id"].(string)
	if x.Mode != "fabric" || requestID == "" {
		return nil, conflict("Execution is not a scoped issue verification")
	}
	attempt, e := scanAttempt(tx.QueryRowContext(ctx, "SELECT "+attemptColumns+" FROM exec_task_attempts WHERE id=$1 AND execution_id=$2", attemptID, id))
	if errors.Is(e, sql.ErrNoRows) {
		return nil, platform.ErrNotFound
	}
	if e != nil {
		return nil, e
	}
	reconcile := func(result string) (*Execution, error) {
		return s.Reconcile(ctx, a, project, id, attemptID, ReconcileInput{ExpectedVersion: version, Result: result, Evidence: "Verified original mTLS collector terminal receipt for command " + attempt.CommandID}, key, trace)
	}
	var priorResult string
	e = tx.QueryRowContext(ctx, "SELECT result FROM exec_runner_receipts WHERE command_id=$1 AND attempt_id=$2 AND fencing_token=$3", attempt.CommandID, attempt.ID, attempt.FencingToken).Scan(&priorResult)
	if e == nil {
		tx.Rollback()
		return reconcile(priorResult)
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return nil, e
	}
	if attempt.State != "unknown" || attempt.StateVersion != version {
		return nil, conflict("Attempt is not unknown or its version changed")
	}
	request, e := s.IssueStore.IssueVerificationDispatchTx(ctx, tx, requestID, id, attempt.CommandID, attempt.FencingToken)
	if e != nil {
		var business *platform.Error
		if !errors.As(e, &business) || business.Code != "ISSUE_VERIFICATION_DISPATCH_REQUIRED" {
			return nil, e
		}
	}
	tx.Rollback()
	var observation *fabricboot.CollectorRecollectObservation
	if request != nil {
		query, stop := context.WithTimeout(ctx, 5*time.Second)
		observation, e = s.IssueStore.FabricProbes.QueryRecollection(query, *request)
		stop()
		if e != nil || observation == nil || !observation.Terminal() {
			return nil, failure(409, "ISSUE_VERIFICATION_RECEIPT_PENDING", "No authenticated terminal collector proof; resources remain quarantined")
		}
	}
	// Reauthorize after the network operation and serialize with account changes,
	// issue mutation, and concurrent reconciliation before storing any receipt.
	tx, e = s.DB.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = advisory(ctx, tx, "keel-admin-initialization"); e != nil {
		return nil, e
	}
	if e = authorize(ctx, tx, a, project, "Admin", "DL", "SRE"); e != nil {
		return nil, e
	}
	var locked string
	if e = tx.QueryRowContext(ctx, "SELECT id FROM exec_executions WHERE id=$1 AND project_id=$2 FOR UPDATE", id, project).Scan(&locked); e != nil {
		return nil, e
	}
	current, e := scanAttempt(tx.QueryRowContext(ctx, "SELECT "+attemptColumns+" FROM exec_task_attempts WHERE id=$1 AND execution_id=$2 FOR UPDATE", attemptID, id))
	if e != nil {
		return nil, e
	}
	e = tx.QueryRowContext(ctx, "SELECT result FROM exec_runner_receipts WHERE command_id=$1 AND attempt_id=$2 AND fencing_token=$3", attempt.CommandID, attempt.ID, attempt.FencingToken).Scan(&priorResult)
	if e == nil {
		tx.Rollback()
		return reconcile(priorResult)
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return nil, e
	}
	if current.State != "unknown" || current.StateVersion != version || current.CommandID != attempt.CommandID || current.FencingToken != attempt.FencingToken {
		return nil, conflict("Original unknown attempt changed during collector query")
	}
	var proof, digest string
	if request == nil {
		proof, digest, request, observation, e = s.IssueStore.PersistUndispatchedIssueVerificationTx(ctx, tx, requestID, id, attempt.CommandID, attempt.FencingToken)
	} else {
		proof, digest, e = s.IssueStore.PersistIssueVerificationRecoveryTx(ctx, tx, requestID, id, attempt.CommandID, attempt.FencingToken, observation)
	}
	if e != nil {
		return nil, e
	}
	verdict, e := s.IssueStore.IssueVerificationReceiptVerdictTx(ctx, tx, requestID, id, attempt.CommandID, attempt.FencingToken, proof, digest)
	if e != nil {
		return nil, e
	}
	result := "failed"
	if verdict == "PASS" {
		result = "success"
	}
	receipt := Receipt{CommandID: attempt.CommandID, AttemptID: attempt.ID, FencingToken: attempt.FencingToken, Result: result, StartedAt: request.NotBefore, FinishedAt: *observation.FinishedAt, Output: map[string]any{"issue_verification_id": requestID, "evidence_id": proof, "evidence_hash": digest, "operation": "query_original_recollection", "verification_verdict": verdict, "collector_state": observation.State, "evidence_source": request.EvidenceSource}}
	if e = s.recordReceiptTx(ctx, tx, receipt); e != nil {
		return nil, e
	}
	if e = audit(ctx, tx, a.User.ID, project, "issue.verification.receipt_queried", attemptID, trace, map[string]any{"request_id": requestID, "evidence_id": proof, "collector_state": observation.State}); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return reconcile(result)
}
