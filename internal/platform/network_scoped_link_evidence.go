package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/enzoOvOyu/keel/internal/fabricboot"
	"github.com/enzoOvOyu/keel/internal/fabricmon"
)

type scopedLinkEvidence struct {
	Request                                  *IssueVerificationRequest
	IssueID, ExecutionID, EvidenceID, Origin string
	Finding                                  fabricmon.Finding
	CollectedAt, ValidUntil                  time.Time
}

// These proofs replace only the frozen cable's precheck. They never refresh a
// collection, switch-wide metrics, other links, readiness TTL, or server NP-3.
func (s *Store) currentScopedLinkEvidenceTx(ctx context.Context, tx *sql.Tx, project string) (map[string][]scopedLinkEvidence, error) {
	out := map[string][]scopedLinkEvidence{}
	rows, e := tx.QueryContext(ctx, `SELECT q.id,q.execution_id,q.issue_id,p.id,q.snapshot->>'evidence_origin',r.finding,p.response,r.valid_until FROM issue_verification_requests q JOIN issue_verification_results r ON r.request_id=q.id JOIN issue_verification_evidence p ON p.id=r.evidence_id JOIN exec_executions x ON x.id=q.execution_id JOIN issues i ON i.id=q.issue_id WHERE q.project_id=$1 AND r.verdict IN('PASS','FAIL') AND r.valid_until>now() AND x.state IN('success','failed') AND ((i.state IN('verify','reopened') AND i.version=r.applied_issue_version) OR (i.state='closed' AND i.version=r.applied_issue_version+1)) ORDER BY r.evaluated_at DESC,q.id DESC`, project)
	if e != nil {
		return out, e
	}
	type candidate struct {
		request, execution, issue, proof, origin string
		finding, response                        []byte
		until                                    time.Time
	}
	candidates := []candidate{}
	for rows.Next() {
		var c candidate
		if e = rows.Scan(&c.request, &c.execution, &c.issue, &c.proof, &c.origin, &c.finding, &c.response, &c.until); e != nil {
			rows.Close()
			return out, e
		}
		candidates = append(candidates, c)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	for _, c := range candidates {
		request, e := loadIssueVerificationTx(ctx, tx, c.request, c.execution)
		if e != nil {
			return out, e
		}
		item, e := scanIssue(tx.QueryRowContext(ctx, "SELECT "+issueColumns+" FROM issues WHERE id=$1", c.issue))
		if e != nil {
			return out, e
		}
		current, e := s.issueVerificationScopeMatchesTx(ctx, tx, request, item)
		if e != nil {
			return out, e
		}
		if !current {
			continue
		}
		var finding fabricmon.Finding
		if e = json.Unmarshal(c.finding, &finding); e != nil {
			return out, e
		}
		var response fabricboot.CollectorRecollectResponse
		if e = json.Unmarshal(c.response, &response); e != nil {
			return out, e
		}
		if finding.CableUID != request.Snapshot.CableUID || finding.RuleRef != request.Snapshot.RuleRef || len(response.Devices) == 0 {
			continue
		}
		observed := response.Devices[0].CollectedAt
		for _, sample := range response.Devices {
			if sample.CollectedAt.Before(observed) {
				observed = sample.CollectedAt
			}
		}
		proof := scopedLinkEvidence{Request: request, IssueID: c.issue, ExecutionID: c.execution, EvidenceID: c.proof, Origin: c.origin, Finding: finding, CollectedAt: observed, ValidUntil: c.until}
		out[finding.CableUID] = append(out[finding.CableUID], proof)
	}
	return out, nil
}
func newestScopedLinkEvidence(candidates []scopedLinkEvidence, collection *FabricCollectionReport) *scopedLinkEvidence {
	var best *scopedLinkEvidence
	for i := range candidates {
		candidate := &candidates[i]
		if collection != nil && (collection.EvidenceSource != candidate.Origin || !candidate.CollectedAt.After(collection.CollectedAt)) {
			continue
		}
		if best == nil || candidate.CollectedAt.After(best.CollectedAt) {
			best = candidate
		}
	}
	return best
}
func applyScopedLinkEvidence(item *NetworkCableEvidence, proof *scopedLinkEvidence) {
	item.Gate = networkGate(item.CableID, "BLOCKED", proof.Finding.Reason)
	switch proof.Finding.Verdict {
	case "precheck_pass":
		item.Gate.State = "PASS"
	case "anomaly":
		item.Gate.State = "FAIL"
	}
	item.Gate.EvidenceRefs = []string{proof.Request.ID, proof.EvidenceID, proof.ExecutionID, proof.IssueID}
	item.RuleRef = proof.Finding.RuleRef
	item.EvidenceSource = proof.Origin
	item.CollectedAt = &proof.CollectedAt
	item.ValidUntil = proof.ValidUntil
	item.VerificationRequestID = proof.Request.ID
	item.ExecutionID = proof.ExecutionID
	item.IssueID = proof.IssueID
	item.EvidenceScope = "targeted_link"
	// The narrow proof may outlive the previous collection, but its own original
	// sample expiry is never extended. Presentation itself stays within 30s.
	if until := time.Now().UTC().Add(30 * time.Second); until.Before(item.ValidUntil) {
		item.ValidUntil = until
	}
}
