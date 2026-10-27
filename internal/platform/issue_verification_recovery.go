package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/enzoOvOyu/keel/internal/fabricboot"
	"github.com/enzoOvOyu/keel/internal/netbaseline"
	"github.com/enzoOvOyu/keel/internal/portdiag"
)

func persistIssueVerificationDispatchTx(ctx context.Context, tx *sql.Tx, id string, request *fabricboot.CollectorRecollectRequest) error {
	var existing bool
	if e := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM issue_verification_dispatches WHERE request_id=$1)", id).Scan(&existing); e != nil {
		return e
	}
	if existing {
		return &fabricboot.RecollectionUnknown{Reason: "original recollection was already committed; query its receipt instead of redispatching"}
	}
	raw, e := json.Marshal(request)
	if e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO issue_verification_dispatches(request_id,command_id,fencing_token,request,content_hash,committed_at) VALUES($1,$2,$3,$4,$5,now())`, id, request.CommandID, request.FencingToken, raw, netbaseline.ContentHash(request))
	return e
}

func (s *Store) IssueVerificationDispatchTx(ctx context.Context, tx *sql.Tx, id, execution, command string, fence int64) (*fabricboot.CollectorRecollectRequest, error) {
	var raw []byte
	var digest string
	e := tx.QueryRowContext(ctx, `SELECT d.request,d.content_hash FROM issue_verification_dispatches d JOIN issue_verification_requests q ON q.id=d.request_id WHERE d.request_id=$1 AND q.execution_id=$2 AND d.command_id=$3 AND d.fencing_token=$4`, id, execution, command, fence).Scan(&raw, &digest)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, apiError(409, "ISSUE_VERIFICATION_DISPATCH_REQUIRED", "No immutable original collector dispatch is available")
	}
	if e != nil {
		return nil, e
	}
	var request fabricboot.CollectorRecollectRequest
	if e = json.Unmarshal(raw, &request); e != nil {
		return nil, e
	}
	if request.Validate() != nil || netbaseline.ContentHash(request) != digest {
		return nil, apiError(409, "ISSUE_VERIFICATION_PROOF_MISMATCH", "Original collector dispatch digest mismatch")
	}
	return &request, nil
}

// Called only after the operator reauthorizes and locks the unknown attempt.
// Persistence alone neither changes the issue nor releases quarantined leases.
func (s *Store) PersistIssueVerificationRecoveryTx(ctx context.Context, tx *sql.Tx, id, execution, command string, fence int64, observation *fabricboot.CollectorRecollectObservation) (string, string, error) {
	request, e := s.IssueVerificationDispatchTx(ctx, tx, id, execution, command, fence)
	if e != nil {
		return "", "", e
	}
	if observation == nil || !observation.Terminal() {
		return "", "", apiError(409, "ISSUE_VERIFICATION_RECEIPT_PENDING", "Collector has no terminal proof; resources remain quarantined")
	}
	if e = observation.Validate(*request, time.Now().UTC()); e != nil {
		return "", "", e
	}
	return s.persistIssueVerificationRecoveryProofTx(ctx, tx, id, execution, command, fence, request, observation)
}
func (s *Store) persistIssueVerificationRecoveryProofTx(ctx context.Context, tx *sql.Tx, id, execution, command string, fence int64, request *fabricboot.CollectorRecollectRequest, observation *fabricboot.CollectorRecollectObservation) (string, string, error) {
	raw, e := json.Marshal(observation)
	if e != nil {
		return "", "", e
	}
	queryID := RandomID()
	_, e = tx.ExecContext(ctx, `INSERT INTO issue_verification_queries(id,request_id,command_id,fencing_token,observation,content_hash,observed_at) VALUES($1,$2,$3,$4,$5,$6,now())`, queryID, id, command, fence, raw, netbaseline.ContentHash(observation))
	if e != nil {
		return "", "", e
	}
	code := "RECOLLECTION_QUERY_" + observation.State
	return s.persistIssueVerificationEvidenceTx(ctx, tx, id, execution, command, fence, request, observation.Result, code, queryID, *observation.FinishedAt)
}

// Absence is proof only for a local committed-dispatch record, never for HTTP
// 404. The control advisory and attempt row locks exclude a late local commit.
func (s *Store) UndispatchedIssueVerificationTx(ctx context.Context, tx *sql.Tx, id, execution, command string, fence int64) (*fabricboot.CollectorRecollectRequest, *fabricboot.CollectorRecollectObservation, error) {
	var dispatched bool
	if e := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM issue_verification_dispatches WHERE request_id=$1)", id).Scan(&dispatched); e != nil {
		return nil, nil, e
	}
	if dispatched {
		return nil, nil, apiError(409, "ISSUE_VERIFICATION_SCOPE_CHANGED", "Original dispatch appeared; query its collector proof")
	}
	frozen, e := loadIssueVerificationTx(ctx, tx, id, execution)
	if e != nil {
		return nil, nil, e
	}
	request := &fabricboot.CollectorRecollectRequest{Version: "fabric-recollect/v1", ProjectID: frozen.ProjectID, ExecutionID: execution, CommandID: command, FencingToken: fence, ScopeHash: frozen.ScopeHash, EvidenceSource: frozen.Snapshot.EvidenceOrigin, NotBefore: frozen.CreatedAt, Targets: frozen.Snapshot.Targets}
	if frozen.Snapshot.PortMonitor != nil {
		request.Version = portdiag.Version
		request.PortDiagnostic = &frozen.Snapshot.PortMonitor.Diagnostic
		if e = tx.QueryRowContext(ctx, `SELECT id FROM exec_task_attempts WHERE execution_id=$1 AND command_id=$2 AND fencing_token=$3`, execution, command, fence).Scan(&request.AttemptID); e != nil {
			return nil, nil, e
		}
	}
	if e = request.Validate(); e != nil {
		return nil, nil, e
	}
	now := time.Now().UTC()
	observation := &fabricboot.CollectorRecollectObservation{Version: "fabric-recollect-status/v1", ProjectID: request.ProjectID, ExecutionID: execution, CommandID: command, FencingToken: fence, ScopeHash: request.ScopeHash, RequestHash: netbaseline.ContentHash(request), EvidenceSource: request.EvidenceSource, State: "local_not_dispatched", FinishedAt: &now}
	return request, observation, nil
}
func (s *Store) PersistUndispatchedIssueVerificationTx(ctx context.Context, tx *sql.Tx, id, execution, command string, fence int64) (string, string, *fabricboot.CollectorRecollectRequest, *fabricboot.CollectorRecollectObservation, error) {
	request, observation, e := s.UndispatchedIssueVerificationTx(ctx, tx, id, execution, command, fence)
	if e != nil {
		return "", "", nil, nil, e
	}
	proof, digest, e := s.persistIssueVerificationRecoveryProofTx(ctx, tx, id, execution, command, fence, request, observation)
	return proof, digest, request, observation, e
}
