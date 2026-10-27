package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/enzoOvOyu/keel/internal/netbaseline"
)

type NetworkTemplateImpactChange struct {
	Path     string `json:"path"`
	Severity string `json:"severity"`
}
type NetworkTemplateImpactDevice struct {
	SwitchID              string                        `json:"switch_id"`
	Serial                string                        `json:"serial"`
	JobID                 string                        `json:"job_id"`
	ExecutionID           string                        `json:"execution_id"`
	BatchRef              string                        `json:"batch_ref"`
	BaselineRef           string                        `json:"baseline_ref"`
	PreviousIntendedHash  string                        `json:"previous_intended_hash"`
	CandidateFactsHash    string                        `json:"candidate_facts_hash,omitempty"`
	TargetCount           int                           `json:"target_count"`
	CandidateIntendedHash string                        `json:"candidate_intended_hash,omitempty"`
	Verdict               string                        `json:"verdict"`
	Reason                string                        `json:"reason,omitempty"`
	Changes               []NetworkTemplateImpactChange `json:"changes"`
	EvidenceSource        string                        `json:"evidence_source,omitempty"`
}
type NetworkTemplateImpact struct {
	Version              string                        `json:"version"`
	ProjectID            string                        `json:"project_id"`
	BaseTemplateID       string                        `json:"base_template_id"`
	BaseContentHash      string                        `json:"base_content_hash"`
	CandidateTemplateID  string                        `json:"candidate_template_id"`
	CandidateContentHash string                        `json:"candidate_content_hash"`
	ReferencingJobs      int                           `json:"referencing_jobs"`
	ReferencingBatches   int                           `json:"referencing_batches"`
	ReferencingDevices   int                           `json:"referencing_devices"`
	CurrentDevices       int                           `json:"current_devices"`
	Page                 int                           `json:"page"`
	PageSize             int                           `json:"page_size"`
	EvaluatedAt          time.Time                     `json:"evaluated_at"`
	ValidUntil           time.Time                     `json:"valid_until"`
	Items                []NetworkTemplateImpactDevice `json:"items"`
}

// Latest production attempt wins even when its result is unknown or rolled
// back. An earlier successful job must never hide a newer unresolved attempt.
const latestNetworkTemplateDevicesCTE = `WITH latest AS (
 SELECT DISTINCT ON(d.switch_id) j.template_id,j.id AS job_id,j.input,j.state AS job_state,j.push_execution_id,d.switch_id,d.state AS device_state,d.facts,d.verify_evidence,d.intended_id,x.state AS execution_state
 FROM network_push_jobs j JOIN network_push_devices d ON d.job_id=j.id JOIN exec_executions x ON x.id=j.push_execution_id
 WHERE j.project_id=$1 ORDER BY d.switch_id,x.created_at DESC,x.id DESC
)`

// This is a read-only potential Golden diff against retained production intent,
// not a live device recollection, a PushJob approval or an automatic write.
func (s *Store) NetworkTemplateImpact(ctx context.Context, a *AuthSession, p *ProjectSummary, candidateID, baseID string, page, size int) (*NetworkTemplateImpact, error) {
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
	candidate, e := loadNetworkTemplateTx(ctx, tx, p.ID, candidateID)
	if e != nil {
		return nil, e
	}
	if baseID == "" {
		baseID = candidate.ParentVersionID
	}
	if baseID == "" {
		return nil, apiError(400, "NETWORK_TEMPLATE_BASE_REQUIRED", "Select an earlier version in the same template family")
	}
	base, e := loadNetworkTemplateTx(ctx, tx, p.ID, baseID)
	if e != nil {
		return nil, e
	}
	if base.Name != candidate.Name || base.Version >= candidate.Version {
		return nil, apiError(409, "NETWORK_TEMPLATE_FAMILY_MISMATCH", "Impact requires an earlier version in the same template family")
	}
	now := time.Now().UTC()
	out := &NetworkTemplateImpact{Version: "network-template-impact/v1", ProjectID: p.ID, BaseTemplateID: base.ID, BaseContentHash: base.ContentHash, CandidateTemplateID: candidate.ID, CandidateContentHash: candidate.ContentHash, Page: page, PageSize: size, EvaluatedAt: now, ValidUntil: now.Add(30 * time.Second), Items: []NetworkTemplateImpactDevice{}}
	if e = tx.QueryRowContext(ctx, `SELECT count(DISTINCT j.id),count(DISTINCT j.input->>'batch_ref'),count(DISTINCT d.switch_id) FROM network_push_jobs j LEFT JOIN network_push_devices d ON d.job_id=j.id WHERE j.project_id=$1 AND j.template_id=$2`, p.ID, base.ID).Scan(&out.ReferencingJobs, &out.ReferencingBatches, &out.ReferencingDevices); e != nil {
		return nil, e
	}
	if e = tx.QueryRowContext(ctx, latestNetworkTemplateDevicesCTE+` SELECT count(*) FROM latest WHERE template_id=$2`, p.ID, base.ID).Scan(&out.CurrentDevices); e != nil {
		return nil, e
	}
	rows, e := tx.QueryContext(ctx, latestNetworkTemplateDevicesCTE+` SELECT l.job_id,l.input,l.push_execution_id,l.switch_id,l.device_state,l.execution_state,l.facts,coalesce(l.verify_evidence,'null'),i.configuration FROM latest l JOIN network_intended_configs i ON i.id=l.intended_id WHERE l.template_id=$2 ORDER BY l.switch_id LIMIT $3 OFFSET $4`, p.ID, base.ID, size, (page-1)*size)
	if e != nil {
		return nil, e
	}
	records := []templateImpactRecord{}
	for rows.Next() {
		var r templateImpactRecord
		if e = rows.Scan(&r.job, &r.input, &r.execution, &r.device, &r.state, &r.executionState, &r.facts, &r.proof, &r.previous); e != nil {
			rows.Close()
			return nil, e
		}
		records = append(records, r)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	out.Items, e = evaluateNetworkTemplateImpactTx(ctx, tx, p.ID, candidate, records)
	if e != nil {
		return nil, e
	}
	return out, nil
}

type templateImpactRecord struct {
	job, execution, device, state, executionState string
	input, facts, proof, previous                 []byte
}

func evaluateNetworkTemplateImpactTx(ctx context.Context, tx *sql.Tx, project string, candidate *NetworkTemplateVersion, records []templateImpactRecord) ([]NetworkTemplateImpactDevice, error) {
	var e error
	now := time.Now().UTC()
	items := []NetworkTemplateImpactDevice{}
	// Render each current approved batch once per page. All lookups share this
	// repeatable-read snapshot, and never call a device/validation adapter.
	type batch struct {
		devices   map[string]NetworkPlannedDevice
		reason    string
		factsHash string
		count     int
	}
	batches := map[string]batch{}
	for _, r := range records {
		var input NetworkPlanInput
		var previous NetworkPlannedDevice
		if e = json.Unmarshal(r.input, &input); e != nil {
			return nil, e
		}
		if e = json.Unmarshal(r.facts, &previous); e != nil {
			return nil, e
		}
		item := NetworkTemplateImpactDevice{SwitchID: r.device, Serial: previous.Serial, JobID: r.job, ExecutionID: r.execution, BatchRef: input.BatchRef, BaselineRef: input.BaselineRef, PreviousIntendedHash: netbaseline.ContentHash(json.RawMessage(r.previous)), Verdict: "BLOCKED", Changes: []NetworkTemplateImpactChange{}}
		var proof *netbaseline.OperationResult
		if e = json.Unmarshal(r.proof, &proof); e != nil {
			return nil, e
		}
		if proof != nil {
			item.EvidenceSource = proof.EvidenceSource
		}
		if previous.ID != r.device || !networkGoldenProofMatches(project, previous, r.state, r.executionState, proof, now) || item.PreviousIntendedHash != previous.IntendedHash {
			item.Reason = "current_golden_or_execution_unresolved"
			items = append(items, item)
			continue
		}
		key := input.BaselineRef + "\x00" + input.BatchRef
		b, ok := batches[key]
		if !ok {
			input.TemplateID = candidate.ID
			_, _, devices, err := networkTemplateFactsTx(ctx, tx, project, input, false)
			b = batch{devices: map[string]NetworkPlannedDevice{}, factsHash: netbaseline.ContentHash(devices), count: len(devices)}
			if err != nil {
				var business *Error
				if !errors.As(err, &business) {
					return nil, err
				}
				b.reason = business.Code
			} else {
				for _, d := range devices {
					b.devices[d.ID] = d
				}
			}
			batches[key] = b
		}
		next, exists := b.devices[r.device]
		switch {
		case b.reason != "":
			item.Reason = b.reason
		case !exists || next.Serial != previous.Serial:
			item.Reason = "current_approved_device_binding_changed"
		default:
			item.CandidateIntendedHash = next.IntendedHash
			item.CandidateFactsHash = b.factsHash
			item.TargetCount = b.count
			changes, err := netbaseline.StructuredDiff(json.RawMessage(r.previous), next.Intended)
			if err != nil {
				return nil, err
			}
			item.Verdict = "UNCHANGED"
			if len(changes) > 0 {
				item.Verdict = "DIFF"
			}
			for _, change := range changes {
				item.Changes = append(item.Changes, NetworkTemplateImpactChange{Path: change.Path, Severity: change.Severity})
			}
		}
		items = append(items, item)
	}
	return items, nil
}
