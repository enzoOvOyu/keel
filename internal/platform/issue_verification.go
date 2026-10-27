package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/enzoOvOyu/keel/internal/fabricboot"
	"github.com/enzoOvOyu/keel/internal/fabricmon"
	"github.com/enzoOvOyu/keel/internal/netbaseline"
	"github.com/enzoOvOyu/keel/internal/portdiag"
)

type IssueVerificationInput struct {
	Version   int    `json:"version"`
	ScopeHash string `json:"scope_hash"`
	Reason    string `json:"reason"`
}
type IssueVerificationRequest struct {
	ID           string                 `json:"id"`
	IssueID      string                 `json:"issue_id"`
	ProjectID    string                 `json:"project_id"`
	ExecutionID  string                 `json:"execution_id"`
	IssueVersion int                    `json:"issue_version"`
	ScopeHash    string                 `json:"scope_hash"`
	Snapshot     IssueVerificationScope `json:"snapshot"`
	Reason       string                 `json:"reason"`
	CreatedBy    string                 `json:"created_by"`
	CreatedAt    time.Time              `json:"created_at"`
}

const issueVerificationRequestColumns = "id,issue_id,project_id,execution_id,issue_version,scope_hash,snapshot,reason,created_by,created_at"

func loadIssueVerificationTx(ctx context.Context, tx *sql.Tx, id, execution string) (*IssueVerificationRequest, error) {
	var out IssueVerificationRequest
	var raw []byte
	e := tx.QueryRowContext(ctx, "SELECT "+issueVerificationRequestColumns+" FROM issue_verification_requests WHERE id=$1 AND execution_id=$2", id, execution).Scan(&out.ID, &out.IssueID, &out.ProjectID, &out.ExecutionID, &out.IssueVersion, &out.ScopeHash, &raw, &out.Reason, &out.CreatedBy, &out.CreatedAt)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if e != nil {
		return nil, e
	}
	if e = json.Unmarshal(raw, &out.Snapshot); e != nil {
		return nil, e
	}
	return &out, nil
}
func (s *Store) PrepareIssueVerificationTx(ctx context.Context, tx *sql.Tx, a *AuthSession, project, id string, in IssueVerificationInput) (*IssueVerificationScope, error) {
	if in.Version < 1 || len(in.ScopeHash) != 64 || strings.TrimSpace(in.Reason) == "" || utf8.RuneCountInString(in.Reason) > 2000 {
		return nil, apiError(400, "VALIDATION_ERROR", "Current issue version, scope digest and reason are required")
	}
	item, e := scanIssue(tx.QueryRowContext(ctx, "SELECT "+issueColumns+" FROM issues WHERE id=$1 AND project_id=$2 FOR UPDATE", id, project))
	if errors.Is(e, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if e != nil {
		return nil, e
	}
	role, e := issueRoleTx(ctx, tx, a, project)
	if e != nil {
		return nil, e
	}
	if !slices.Contains([]string{"Admin", "DL", "NE"}, role) || role == "NE" && item.AssigneeID != a.User.ID {
		return nil, ErrForbidden
	}
	if item.Version != in.Version {
		return nil, errVersion
	}
	scope, e := s.issueVerificationScopeTx(ctx, tx, a, item, role)
	if e != nil {
		return nil, e
	}
	if scope.ScopeHash != in.ScopeHash {
		return nil, apiError(409, "ISSUE_VERIFICATION_SCOPE_CHANGED", "Refresh the current issue verification scope")
	}
	if !scope.ExecutionAvailable {
		return nil, apiError(409, "ISSUE_VERIFICATION_BLOCKED", "Required scoped verification inputs are unavailable")
	}
	return scope, nil
}
func (s *Store) AttachIssueVerificationTx(ctx context.Context, tx *sql.Tx, a *AuthSession, id, execution string, scope *IssueVerificationScope, in IssueVerificationInput, trace string) error {
	if !scope.ValidUntil.After(time.Now()) {
		return apiError(409, "ISSUE_VERIFICATION_SCOPE_EXPIRED", "Refresh the verification scope")
	}
	item, e := scanIssue(tx.QueryRowContext(ctx, `UPDATE issues SET state='verify',version=version+1,updated_at=now() WHERE id=$1 AND version=$2 AND state='in_repair' RETURNING `+issueColumns, scope.IssueID, scope.IssueVersion))
	if e != nil {
		return e
	}
	raw, e := json.Marshal(scope)
	if e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO issue_verification_requests(id,issue_id,project_id,execution_id,issue_version,scope_hash,snapshot,reason,created_by,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,now())`, id, item.ID, item.ProjectID, execution, scope.IssueVersion, scope.ScopeHash, raw, in.Reason, a.User.ID); e != nil {
		return e
	}
	return issueEventTx(ctx, tx, a, item, "verify", trace, map[string]any{"request_id": id, "execution_id": execution, "scope_hash": scope.ScopeHash, "repair_version": scope.RepairVersion, "reason": in.Reason})
}

// VerificationCurrentTx must be called under the existing control advisory lock.
// Only this issue's fault is exempted; identity, scope, authority and versions remain current.
func (s *Store) VerificationCurrentTx(ctx context.Context, tx *sql.Tx, id, execution string) (*IssueVerificationRequest, bool, error) {
	request, e := loadIssueVerificationTx(ctx, tx, id, execution)
	if e != nil {
		return nil, false, e
	}
	item, e := scanIssue(tx.QueryRowContext(ctx, "SELECT "+issueColumns+" FROM issues WHERE id=$1", request.IssueID))
	if e != nil {
		return nil, false, e
	}
	if item.Version != request.IssueVersion+1 || item.State != "verify" {
		return request, false, nil
	}
	current, e := s.issueVerificationScopeMatchesTx(ctx, tx, request, item)
	return request, current, e
}
func (s *Store) issueVerificationScopeMatchesTx(ctx context.Context, tx *sql.Tx, request *IssueVerificationRequest, item *Issue) (bool, error) {
	var enabled, admin bool
	if e := tx.QueryRowContext(ctx, "SELECT enabled,is_admin FROM users WHERE id=$1", request.CreatedBy).Scan(&enabled, &admin); e != nil {
		return false, e
	}
	if !enabled || time.Since(request.CreatedAt) > 15*time.Minute {
		return false, nil
	}
	actor := &AuthSession{User: UserSummary{ID: request.CreatedBy, IsAdmin: admin}}
	role, e := issueRoleTx(ctx, tx, actor, item.ProjectID)
	if errors.Is(e, ErrNotFound) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	copyIssue := *item
	copyIssue.Version = request.IssueVersion
	copyIssue.State = "in_repair"
	current, e := s.issueVerificationScopeTx(ctx, tx, actor, &copyIssue, role)
	if e != nil {
		var business *Error
		if errors.As(e, &business) && business.Status == 409 {
			return false, nil
		}
		return false, e
	}
	return current.ExecutionAvailable && current.ScopeHash == request.ScopeHash, nil
}
func (s *Store) IssueVerificationResourcesTx(ctx context.Context, tx *sql.Tx, id, execution string) ([]string, error) {
	request, ok, e := s.VerificationCurrentTx(ctx, tx, id, execution)
	if e != nil {
		return nil, e
	}
	if !ok {
		return nil, apiError(409, "ISSUE_VERIFICATION_SCOPE_CHANGED", "Verification admission is no longer current")
	}
	resources := []string{}
	if request.Snapshot.PortMonitor != nil {
		machine := request.Snapshot.PortMonitor.Diagnostic.MachineID
		resources = append(resources, "device:machine:"+machine, "ssh:machine:"+machine, "agent:machine:"+machine)
	}
	for _, target := range request.Snapshot.Targets {
		resources = append(resources, "ssh:switch:"+target.SwitchID)
		if request.Snapshot.PortMonitor != nil {
			resources = append(resources, "device:switch:"+target.SwitchID)
		}
	}
	slices.Sort(resources)
	return slices.Compact(resources), nil
}
func (s *Store) RunIssueVerification(ctx context.Context, id, execution, command string, fence int64) (*fabricboot.CollectorRecollectRequest, *fabricboot.CollectorRecollectResponse, error) {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return nil, nil, e
	}
	defer tx.Rollback()
	if e = lockKey(ctx, tx, "keel-admin-initialization"); e != nil {
		return nil, nil, e
	}
	request, e := loadIssueVerificationTx(ctx, tx, id, execution)
	if e != nil {
		return nil, nil, e
	}
	op := &fabricboot.CollectorRecollectRequest{Version: "fabric-recollect/v1", ProjectID: request.ProjectID, ExecutionID: execution, CommandID: command, FencingToken: fence, ScopeHash: request.ScopeHash, EvidenceSource: request.Snapshot.EvidenceOrigin, NotBefore: time.Now().UTC(), Targets: request.Snapshot.Targets}
	if request.Snapshot.PortMonitor != nil {
		op.Version = portdiag.Version
		op.PortDiagnostic = &request.Snapshot.PortMonitor.Diagnostic
		if e = tx.QueryRowContext(ctx, `SELECT id FROM exec_task_attempts WHERE execution_id=$1 AND command_id=$2 AND fencing_token=$3`, execution, command, fence).Scan(&op.AttemptID); e != nil {
			return nil, nil, e
		}
	}
	_, ok, e := s.VerificationCurrentTx(ctx, tx, id, execution)
	if e != nil {
		return op, nil, e
	}
	if !ok {
		return op, nil, apiError(409, "ISSUE_VERIFICATION_SCOPE_CHANGED", "Verification admission changed before recollection")
	}
	var held bool
	if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM exec_task_attempts a JOIN exec_executions x ON x.id=a.execution_id WHERE a.execution_id=$1 AND a.command_id=$2 AND a.fencing_token=$3 AND a.state='running' AND a.lease_until>now() AND x.desired_state='running')`, execution, command, fence).Scan(&held); e != nil {
		return nil, nil, e
	}
	if !held {
		return nil, nil, apiError(409, "STALE_FENCE", "A current execution lease is required")
	}

	if e = op.Validate(); e != nil {
		return nil, nil, e
	}
	if e = persistIssueVerificationDispatchTx(ctx, tx, request.ID, op); e != nil {
		return nil, nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, nil, e
	}
	response, e := s.FabricProbes.RecollectPorts(ctx, *op)
	return op, response, e
}

// PersistIssueVerificationEvidenceTx is atomic with the kernel's durable receipt.
func (s *Store) PersistIssueVerificationEvidenceTx(ctx context.Context, tx *sql.Tx, id, execution, command string, fence int64, request *fabricboot.CollectorRecollectRequest, response *fabricboot.CollectorRecollectResponse, code string) (string, string, error) {
	return s.persistIssueVerificationEvidenceTx(ctx, tx, id, execution, command, fence, request, response, code, "", time.Now().UTC())
}
func (s *Store) persistIssueVerificationEvidenceTx(ctx context.Context, tx *sql.Tx, id, execution, command string, fence int64, request *fabricboot.CollectorRecollectRequest, response *fabricboot.CollectorRecollectResponse, code, queryID string, validationAt time.Time) (string, string, error) {
	if request == nil {
		return "", "", apiError(409, "ISSUE_VERIFICATION_PROOF_REQUIRED", "No issued recollection request")
	}
	frozen, e := loadIssueVerificationTx(ctx, tx, id, execution)
	if e != nil {
		return "", "", e
	}
	if request.ScopeHash != frozen.ScopeHash || request.ProjectID != frozen.ProjectID || request.CommandID != command || request.ExecutionID != execution || request.FencingToken != fence || request.EvidenceSource != frozen.Snapshot.EvidenceOrigin || netbaseline.ContentHash(request.Targets) != netbaseline.ContentHash(frozen.Snapshot.Targets) {
		return "", "", apiError(409, "ISSUE_VERIFICATION_PROOF_MISMATCH", "Recollection proof differs from frozen admission")
	}
	if netbaseline.ContentHash(request.PortDiagnostic) != netbaseline.ContentHash(func() *portdiag.Scope {
		if frozen.Snapshot.PortMonitor != nil {
			return &frozen.Snapshot.PortMonitor.Diagnostic
		}
		return nil
	}()) {
		return "", "", apiError(409, "ISSUE_VERIFICATION_PROOF_MISMATCH", "Physical-port diagnostic differs from frozen admission")
	}
	if response != nil {
		if e = response.Validate(*request, validationAt); e != nil {
			return "", "", e
		}
	} else if code == "" {
		return "", "", apiError(409, "ISSUE_VERIFICATION_PROOF_REQUIRED", "A bounded completion proof is required")
	}
	rawRequest, e := json.Marshal(request)
	if e != nil {
		return "", "", e
	}
	rawResponse, e := json.Marshal(response)
	if e != nil {
		return "", "", e
	}
	proofID := RandomID()
	hash := netbaseline.ContentHash([]any{request, response, code})
	_, e = tx.ExecContext(ctx, `INSERT INTO issue_verification_evidence(id,request_id,command_id,fencing_token,request,response,content_hash,code,recorded_at,query_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,now(),nullif($9,''))`, proofID, id, command, fence, rawRequest, rawResponse, hash, code, queryID)
	return proofID, hash, e
}

// ApplyIssueVerificationEvidenceTx is called only by fenced completion/reconciliation.
func (s *Store) ApplyIssueVerificationEvidenceTx(ctx context.Context, tx *sql.Tx, id, execution, command string, fence int64, proofID, proofHash string) error {
	request, e := loadIssueVerificationTx(ctx, tx, id, execution)
	if e != nil {
		return e
	}
	proof, e := loadIssueVerificationProofTx(ctx, tx, id, execution, command, fence, proofID, proofHash)
	if e != nil {
		return e
	}
	var prior string
	e = tx.QueryRowContext(ctx, "SELECT evidence_id FROM issue_verification_results WHERE request_id=$1", id).Scan(&prior)
	if e == nil {
		if prior != proofID {
			return apiError(409, "ISSUE_VERIFICATION_PROOF_MISMATCH", "Verification already consumed another proof")
		}
		return nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	verdict, code, expiry, finding, e := s.assessIssueVerificationTx(ctx, tx, request, proof.Operation, proof.Response, proof.RecordedAt, proof.ValidationAt, proof.Code)
	if e != nil {
		return e
	}
	var applied *int
	item, e := scanIssue(tx.QueryRowContext(ctx, "SELECT "+issueColumns+" FROM issues WHERE id=$1 FOR UPDATE", request.IssueID))
	if e != nil {
		return e
	}
	if item.Version == request.IssueVersion+1 && item.State == "verify" {
		state := "in_repair"
		if verdict == "PASS" {
			state = "verify"
		} else if verdict == "FAIL" {
			state = "reopened"
		}
		item, e = scanIssue(tx.QueryRowContext(ctx, `UPDATE issues SET state=$2,version=version+1,updated_at=now() WHERE id=$1 RETURNING `+issueColumns, item.ID, state))
		if e != nil {
			return e
		}
		applied = &item.Version
		actor := &AuthSession{User: UserSummary{ID: request.CreatedBy}}
		if e = issueEventTx(ctx, tx, actor, item, "verification_result", "worker", map[string]any{"request_id": id, "execution_id": execution, "verdict": verdict, "code": code, "evidence_id": proofID, "scope_hash": request.ScopeHash}); e != nil {
			return e
		}
	}
	rawFinding, e := json.Marshal(finding)
	if e != nil {
		return e
	}
	portChecks := []portdiag.Check{}
	if request.Snapshot.PortMonitor != nil && proof.Response != nil && proof.Response.PortDiagnostic != nil {
		evaluated, err := portdiag.Evaluate(request.Snapshot.PortMonitor.Diagnostic, portdiag.Binding{ExecutionID: proof.Operation.ExecutionID, AttemptID: proof.Operation.AttemptID, CommandID: proof.Operation.CommandID, FencingToken: proof.Operation.FencingToken, ScopeHash: proof.Operation.ScopeHash}, *proof.Response.PortDiagnostic, proof.ValidationAt)
		if err != nil {
			return err
		}
		portChecks = evaluated.Checks
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO issue_verification_results(request_id,evidence_id,verdict,finding,code,valid_until,applied_issue_version,evaluated_at,port_checks) VALUES($1,$2,$3,$4,$5,$6,$7,now(),$8)`, id, proofID, verdict, rawFinding, code, expiry, applied, networkJSON(portChecks))
	if e == nil && verdict == "FAIL" {
		e = issueVerificationEscalationTx(ctx, tx, request)
	}
	return e
}

func (s *Store) assessIssueVerificationTx(ctx context.Context, tx *sql.Tx, request *IssueVerificationRequest, op fabricboot.CollectorRecollectRequest, response *fabricboot.CollectorRecollectResponse, recorded, validationAt time.Time, code string) (string, string, time.Time, *fabricmon.Finding, error) {
	_, current, e := s.VerificationCurrentTx(ctx, tx, request.ID, request.ExecutionID)
	if e != nil {
		return "", "", time.Time{}, nil, e
	}
	verdict := "BLOCKED"
	expiry := recorded
	var finding *fabricmon.Finding
	if !current {
		code = "ISSUE_VERIFICATION_SCOPE_CHANGED"
	} else if response != nil {
		if e = response.Validate(op, validationAt); e != nil {
			return "", "", time.Time{}, nil, e
		}
		expiry = recorded.Add(5 * time.Minute)
		if request.Snapshot.PortMonitor != nil {
			if response.PortDiagnostic == nil {
				return "BLOCKED", "PORTMON_CAPTURE_REQUIRED", recorded, nil, nil
			}
			capture := response.PortDiagnostic
			evaluated, err := portdiag.Evaluate(request.Snapshot.PortMonitor.Diagnostic, portdiag.Binding{ExecutionID: op.ExecutionID, AttemptID: op.AttemptID, CommandID: op.CommandID, FencingToken: op.FencingToken, ScopeHash: op.ScopeHash}, *capture, validationAt)
			if err != nil {
				return "", "", time.Time{}, nil, err
			}
			expiry = capture.FinishedAt.Add(5 * time.Minute)
			if !expiry.After(time.Now()) {
				return "BLOCKED", "ISSUE_VERIFICATION_EVIDENCE_EXPIRED", expiry, nil, nil
			}
			return evaluated.Verdict, "PORTMON_SCOPED_" + evaluated.Verdict, expiry, nil, nil
		}
		samples := map[string]fabricmon.Snapshot{}
		for _, sample := range response.Devices {
			samples[sample.Serial] = sample
			if v := sample.CollectedAt.Add(5 * time.Minute); v.Before(expiry) {
				expiry = v
			}
		}
		_, _, cables, err := fabricInventoryTx(ctx, tx, request.ProjectID)
		if err != nil {
			return "", "", time.Time{}, nil, err
		}
		for _, cable := range cables {
			if cable.UID == request.Snapshot.CableUID {
				f := fabricmon.EvaluateCable(cable, samples)
				finding = &f
				break
			}
		}
		if finding != nil {
			switch finding.Verdict {
			case "precheck_pass":
				verdict = "PASS"
				code = "SCOPED_LINK_VERIFIED"
			case "anomaly":
				verdict = "FAIL"
				code = "SCOPED_LINK_FAILED"
			default:
				code = "SCOPED_LINK_EVIDENCE_INCOMPLETE"
			}
		}
		if finding != nil && finding.RuleRef != request.Snapshot.RuleRef {
			verdict = "BLOCKED"
			code = "ISSUE_VERIFICATION_RULE_CHANGED"
		}
		if !expiry.After(time.Now()) {
			verdict = "BLOCKED"
			code = "ISSUE_VERIFICATION_EVIDENCE_EXPIRED"
		}
	}
	return verdict, code, expiry, finding, nil
}

func (s *Store) IssueVerificationReceiptVerdictTx(ctx context.Context, tx *sql.Tx, id, execution, command string, fence int64, proofID, proofHash string) (string, error) {
	request, e := loadIssueVerificationTx(ctx, tx, id, execution)
	if e != nil {
		return "", e
	}
	proof, e := loadIssueVerificationProofTx(ctx, tx, id, execution, command, fence, proofID, proofHash)
	if e != nil {
		return "", e
	}
	verdict, _, _, _, e := s.assessIssueVerificationTx(ctx, tx, request, proof.Operation, proof.Response, proof.RecordedAt, proof.ValidationAt, proof.Code)
	return verdict, e
}

type issueVerificationProof struct {
	Operation                fabricboot.CollectorRecollectRequest
	Response                 *fabricboot.CollectorRecollectResponse
	Code                     string
	RecordedAt, ValidationAt time.Time
}

func loadIssueVerificationProofTx(ctx context.Context, tx *sql.Tx, id, execution, command string, fence int64, proofID, proofHash string) (*issueVerificationProof, error) {
	out := &issueVerificationProof{}
	var rawRequest, rawResponse []byte
	var hash string
	e := tx.QueryRowContext(ctx, `SELECT p.request,p.response,p.content_hash,p.code,p.recorded_at,coalesce((SELECT (o.observation->>'finished_at')::timestamptz FROM issue_verification_queries o WHERE o.id=p.query_id),p.recorded_at) FROM issue_verification_evidence p JOIN issue_verification_requests q ON q.id=p.request_id WHERE p.id=$1 AND p.request_id=$2 AND p.command_id=$3 AND p.fencing_token=$4 AND q.execution_id=$5`, proofID, id, command, fence, execution).Scan(&rawRequest, &rawResponse, &hash, &out.Code, &out.RecordedAt, &out.ValidationAt)
	if e != nil {
		return nil, e
	}
	if e = json.Unmarshal(rawRequest, &out.Operation); e != nil {
		return nil, e
	}
	if e = json.Unmarshal(rawResponse, &out.Response); e != nil {
		return nil, e
	}
	if hash != proofHash || netbaseline.ContentHash([]any{&out.Operation, out.Response, out.Code}) != hash {
		return nil, apiError(409, "ISSUE_VERIFICATION_PROOF_MISMATCH", "Stored proof does not match the receipt digest")
	}
	return out, nil
}
