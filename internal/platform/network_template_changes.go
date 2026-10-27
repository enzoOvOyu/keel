package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/enzoOvOyu/keel/internal/netbaseline"
)

type NetworkTemplateChange struct {
	ID                  string                      `json:"id"`
	ProjectID           string                      `json:"project_id"`
	CandidateTemplateID string                      `json:"candidate_template_id"`
	CandidateName       string                      `json:"candidate_name"`
	CandidateVersion    int64                       `json:"candidate_version"`
	BaseTemplateID      string                      `json:"base_template_id"`
	SwitchID            string                      `json:"switch_id"`
	SourceExecutionID   string                      `json:"source_execution_id"`
	State               string                      `json:"state"`
	AssessmentHash      string                      `json:"assessment_hash"`
	Assessment          NetworkTemplateImpactDevice `json:"assessment"`
	PlanInput           NetworkPlanInput            `json:"plan_input"`
	LinkedJobID         string                      `json:"linked_job_id,omitempty"`
	CreatedAt           time.Time                   `json:"created_at"`
}
type NetworkTemplateChangePage struct {
	Version     string                  `json:"version"`
	ProjectID   string                  `json:"project_id"`
	Page        int                     `json:"page"`
	PageSize    int                     `json:"page_size"`
	Total       int                     `json:"total"`
	EvaluatedAt time.Time               `json:"evaluated_at"`
	Items       []NetworkTemplateChange `json:"items"`
}

// Release queues assessment work in one bounded SQL operation. Rendering is a
// paginated read later; release never calls devices or silently approves a push.
func queueNetworkTemplateChangesTx(ctx context.Context, tx *sql.Tx, template *NetworkTemplateVersion, actor string) (int64, error) {
	result, e := tx.ExecContext(ctx, latestNetworkTemplateDevicesCTE+` INSERT INTO network_template_changes(id,project_id,candidate_template_id,base_template_id,switch_id,source_job_id,source_execution_id,created_by,created_at)
 SELECT md5($2||':'||l.switch_id||':'||l.push_execution_id),$1,$2,l.template_id,l.switch_id,l.job_id,l.push_execution_id,$3,now()
 FROM latest l JOIN network_template_versions t ON t.id=l.template_id
 WHERE t.name=$4 AND t.version<$5 ON CONFLICT(candidate_template_id,switch_id,source_execution_id) DO NOTHING`, template.ProjectID, template.ID, actor, template.Name, template.Version)
	if e != nil {
		return 0, e
	}
	return result.RowsAffected()
}

type templateChangeRecord struct {
	item                                           NetworkTemplateChange
	source, latest                                 templateImpactRecord
	latestTemplateID, latestFactsHash, linkedState string
}

// All caller suffixes are fixed SQL, never client-provided fragments.
func loadTemplateChangeRecordsTx(ctx context.Context, tx *sql.Tx, project, suffix string, args ...any) ([]templateChangeRecord, error) {
	values := append([]any{project}, args...)
	rows, e := tx.QueryContext(ctx, latestNetworkTemplateDevicesCTE+` SELECT q.id,q.project_id,q.candidate_template_id,q.base_template_id,q.switch_id,q.source_execution_id,q.created_at,
 j.id,j.input,d.state,x.state,d.facts,coalesce(d.verify_evidence,'null'),i.configuration,
 coalesce(l.template_id,''),coalesce(l.job_id,''),coalesce(l.input,'null'),coalesce(l.push_execution_id,''),coalesce(l.device_state,''),coalesce(l.execution_state,''),coalesce(l.facts,'null'),coalesce(l.verify_evidence,'null'),coalesce(li.configuration,'null'),coalesce(lj.facts_hash,''),
 coalesce(link.job_id,''),coalesce(link.state,'')
 FROM network_template_changes q JOIN network_push_jobs j ON j.id=q.source_job_id JOIN network_push_devices d ON d.job_id=j.id AND d.switch_id=q.switch_id JOIN exec_executions x ON x.id=q.source_execution_id JOIN network_intended_configs i ON i.id=d.intended_id
 LEFT JOIN latest l ON l.switch_id=q.switch_id LEFT JOIN network_intended_configs li ON li.id=l.intended_id LEFT JOIN network_push_jobs lj ON lj.id=l.job_id
 LEFT JOIN LATERAL(SELECT cj.job_id,pj.state FROM network_template_change_jobs cj JOIN network_push_jobs pj ON pj.id=cj.job_id WHERE cj.change_id=q.id ORDER BY cj.created_at DESC,cj.job_id DESC LIMIT 1) link ON true
 WHERE q.project_id=$1 `+suffix, values...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []templateChangeRecord{}
	for rows.Next() {
		var r templateChangeRecord
		if e = rows.Scan(&r.item.ID, &r.item.ProjectID, &r.item.CandidateTemplateID, &r.item.BaseTemplateID, &r.item.SwitchID, &r.item.SourceExecutionID, &r.item.CreatedAt, &r.source.job, &r.source.input, &r.source.state, &r.source.executionState, &r.source.facts, &r.source.proof, &r.source.previous, &r.latestTemplateID, &r.latest.job, &r.latest.input, &r.latest.execution, &r.latest.state, &r.latest.executionState, &r.latest.facts, &r.latest.proof, &r.latest.previous, &r.latestFactsHash, &r.item.LinkedJobID, &r.linkedState); e != nil {
			return nil, e
		}
		r.source.execution = r.item.SourceExecutionID
		r.source.device = r.item.SwitchID
		r.latest.device = r.item.SwitchID
		out = append(out, r)
	}
	return out, rows.Err()
}
func assessTemplateChangesTx(ctx context.Context, tx *sql.Tx, project string, records []templateChangeRecord) ([]NetworkTemplateChange, error) {
	templates := map[string]*NetworkTemplateVersion{}
	groups := map[string][]templateImpactRecord{}
	indices := map[string][]int{}
	for i := range records {
		r := &records[i]
		candidate, ok := templates[r.item.CandidateTemplateID]
		if !ok {
			var e error
			candidate, e = loadNetworkTemplateTx(ctx, tx, project, r.item.CandidateTemplateID)
			if e != nil {
				return nil, e
			}
			templates[candidate.ID] = candidate
		}
		record := r.source
		if r.latestTemplateID == candidate.ID {
			record = r.latest
		}
		groups[candidate.ID] = append(groups[candidate.ID], record)
		indices[candidate.ID] = append(indices[candidate.ID], i)
	}
	for id, group := range groups {
		candidate := templates[id]
		assessments, e := evaluateNetworkTemplateImpactTx(ctx, tx, project, candidate, group)
		if e != nil {
			return nil, e
		}
		for n, assessment := range assessments {
			r := &records[indices[id][n]]
			item := &r.item
			item.CandidateName = candidate.Name
			item.CandidateVersion = candidate.Version
			item.Assessment = assessment
			item.State = assessment.Verdict
			switch {
			case candidate.State != "released":
				item.State = "BLOCKED"
				item.Assessment.Reason = "candidate_template_not_released"
			case r.latestTemplateID == candidate.ID && assessment.Verdict == "UNCHANGED" && assessment.CandidateFactsHash == r.latestFactsHash:
				var original NetworkPlannedDevice
				if e = json.Unmarshal(r.source.facts, &original); e != nil {
					return nil, e
				}
				if original.Serial == assessment.Serial {
					item.State = "RESOLVED"
				} else {
					item.State = "STALE"
					item.Assessment.Reason = "physical_identity_changed"
				}
			case item.LinkedJobID != "" && !networkTemplateChangeTerminal(r.linkedState) && (r.latest.execution == r.source.execution || r.latest.job == item.LinkedJobID):
				item.State = "PLANNED"
			case r.latest.execution != r.source.execution:
				item.State = "STALE"
				item.Assessment.Reason = "production_reference_changed"
			}
			// Bind the entire current approved batch as well as the selected link to
			// this preview. A changed sibling target also invalidates job creation.
			item.AssessmentHash = netbaseline.ContentHash(struct {
				ID, CandidateHash, LatestExecution, LinkedJob, State string
				Assessment                                           NetworkTemplateImpactDevice
			}{item.ID, candidate.ContentHash, r.latest.execution, item.LinkedJobID, item.State, item.Assessment})
			item.PlanInput = NetworkPlanInput{TemplateID: candidate.ID, BaselineRef: assessment.BaselineRef, BatchRef: assessment.BatchRef, TemplateChangeID: item.ID, ExpectedImpactHash: item.AssessmentHash}
		}
	}
	out := []NetworkTemplateChange{}
	for _, r := range records {
		out = append(out, r.item)
	}
	return out, nil
}
func networkTemplateChangeTerminal(state string) bool {
	switch state {
	case "COMPLETED", "PARTIAL", "ROLLED_BACK", "REJECTED", "ARCHIVED":
		return true
	default:
		return false
	}
}
func (s *Store) NetworkTemplateChanges(ctx context.Context, a *AuthSession, p *ProjectSummary, page, size int) (*NetworkTemplateChangePage, error) {
	if page < 1 || page > 1000000 || size < 1 || size > 100 {
		return nil, apiError(400, "FABRIC_PAGE_INVALID", "Use page >=1 and page_size 1 through 100")
	}
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireFabricPermission(ctx, tx, a, p.ID, "fabric:read"); e != nil {
		return nil, e
	}
	out := &NetworkTemplateChangePage{Version: "network-template-changes/v1", ProjectID: p.ID, Page: page, PageSize: size, EvaluatedAt: time.Now().UTC(), Items: []NetworkTemplateChange{}}
	if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM network_template_changes WHERE project_id=$1", p.ID).Scan(&out.Total); e != nil {
		return nil, e
	}
	records, e := loadTemplateChangeRecordsTx(ctx, tx, p.ID, "ORDER BY q.created_at DESC,q.id DESC LIMIT $2 OFFSET $3", size, (page-1)*size)
	if e != nil {
		return nil, e
	}
	out.Items, e = assessTemplateChangesTx(ctx, tx, p.ID, records)
	return out, e
}
func reviewTemplateChangePlanTx(ctx context.Context, tx *sql.Tx, project string, input NetworkPlanInput, factsHash string) error {
	if input.TemplateChangeID == "" && input.ExpectedImpactHash == "" {
		return nil
	}
	if input.TemplateChangeID == "" || input.ExpectedImpactHash == "" {
		return apiError(400, "NETWORK_TEMPLATE_CHANGE_INVALID", "Select a change and its reviewed assessment hash")
	}
	records, e := loadTemplateChangeRecordsTx(ctx, tx, project, "AND q.id=$2", input.TemplateChangeID)
	if e != nil {
		return e
	}
	if len(records) != 1 {
		return ErrNotFound
	}
	items, e := assessTemplateChangesTx(ctx, tx, project, records)
	if e != nil {
		return e
	}
	item := items[0]
	if item.State != "DIFF" || item.AssessmentHash != input.ExpectedImpactHash || item.Assessment.CandidateFactsHash != factsHash || item.PlanInput.TemplateID != input.TemplateID || item.PlanInput.BaselineRef != input.BaselineRef || item.PlanInput.BatchRef != input.BatchRef {
		return apiError(409, "NETWORK_TEMPLATE_CHANGE_STALE", "The selected upgrade, production evidence or approved batch changed; refresh the queue")
	}
	var active bool
	e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM network_push_jobs WHERE project_id=$1 AND template_id=$2 AND input->>'batch_ref'=$3 AND state NOT IN('COMPLETED','PARTIAL','ROLLED_BACK','REJECTED','ARCHIVED'))`, project, input.TemplateID, input.BatchRef).Scan(&active)
	if e != nil {
		return e
	}
	if active {
		return apiError(409, "NETWORK_TEMPLATE_CHANGE_ALREADY_PLANNED", "This candidate batch already has an active PushJob")
	}
	return nil
}
func linkTemplateChangeJobTx(ctx context.Context, tx *sql.Tx, j *NetworkPushJob, actor string) error {
	if j.Input.TemplateChangeID == "" {
		return nil
	}
	result, e := tx.ExecContext(ctx, latestNetworkTemplateDevicesCTE+` INSERT INTO network_template_change_jobs(change_id,job_id,created_by,created_at)
 SELECT q.id,$2,$3,now() FROM network_template_changes q JOIN latest l ON l.switch_id=q.switch_id AND l.push_execution_id=q.source_execution_id JOIN network_push_devices d ON d.switch_id=q.switch_id AND d.job_id=$2
 WHERE q.project_id=$1 AND q.candidate_template_id=$4 ON CONFLICT DO NOTHING`, j.ProjectID, j.ID, actor, j.TemplateID)
	if e != nil {
		return e
	}
	n, e := result.RowsAffected()
	if e != nil {
		return e
	}
	if n == 0 {
		return apiError(409, "NETWORK_TEMPLATE_CHANGE_STALE", "Original production references changed before job creation")
	}
	// Keep ordinary PushJob permissions, prechecks, signatures and canary logic.
	return nil
}
