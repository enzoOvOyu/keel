package platform

import (
	"context"
	"encoding/csv"
	"strconv"
	"strings"
)

type MappingHistoryItem struct {
	ID            string `json:"id"`
	ScopeHash     string `json:"scope_hash"`
	Verdict       string `json:"verdict"`
	ConflictCount int    `json:"conflict_count"`
	RevisionID    string `json:"revision_id"`
	Version       int64  `json:"version"`
	CreatedAt     string `json:"created_at"`
	CreatedBy     string `json:"created_by"`
	Reason        string `json:"reason"`
}
type MappingHistoryPage struct {
	Items    []MappingHistoryItem `json:"items"`
	Total    int                  `json:"total"`
	Page     int                  `json:"page"`
	PageSize int                  `json:"page_size"`
}

func (s *Store) ListMappingHistory(ctx context.Context, a *AuthSession, project, asset string, page Pagination) (*MappingHistoryPage, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = precollectReadScope(ctx, tx, a, project, asset); e != nil {
		return nil, e
	}
	out := &MappingHistoryPage{Items: []MappingHistoryItem{}, Page: page.Page, PageSize: page.PageSize}
	if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM mapping_assessments WHERE project_id=$1 AND asset_id=$2`, project, asset).Scan(&out.Total); e != nil {
		return nil, e
	}
	rows, e := tx.QueryContext(ctx, `SELECT a.id,a.scope_hash,a.candidate->>'verdict',jsonb_array_length(a.candidate->'conflicts'),coalesce(r.id,''),coalesce(r.version,0),to_char(a.created_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),a.created_by,a.reason FROM mapping_assessments a LEFT JOIN mapping_revisions r ON r.assessment_id=a.id WHERE a.project_id=$1 AND a.asset_id=$2 ORDER BY a.created_at DESC,a.id DESC LIMIT $3 OFFSET $4`, project, asset, page.PageSize, page.Offset())
	if e != nil {
		return nil, e
	}
	for rows.Next() {
		var item MappingHistoryItem
		if e = rows.Scan(&item.ID, &item.ScopeHash, &item.Verdict, &item.ConflictCount, &item.RevisionID, &item.Version, &item.CreatedAt, &item.CreatedBy, &item.Reason); e != nil {
			rows.Close()
			return nil, e
		}
		out.Items = append(out.Items, item)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	return out, tx.Commit()
}
func (s *Store) GetMappingAssessment(ctx context.Context, a *AuthSession, project, asset, id string) (*MappingPublication, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = precollectReadScope(ctx, tx, a, project, asset); e != nil {
		return nil, e
	}
	out, e := s.mappingPublicationTx(ctx, tx, project, asset, id)
	if e != nil {
		return nil, e
	}
	return out, tx.Commit()
}

// Exports and all UI views share the exact immutable revision. Expiry and
// supersession are explicit columns, never silently replaced with latest data.
func MappingRevisionCSV(revision *MappingRevision) (string, error) {
	var out strings.Builder
	w := csv.NewWriter(&out)
	if e := w.Write([]string{"mapping_revision_id", "mapping_version", "scope_hash", "effective_verdict", "invalidation_reason", "physical_port_id", "component_id", "component_serial", "logical_name", "role", "rail", "os_interface", "permanent_mac", "pci_bdf", "switch_id", "switch_port_id", "cable_id", "g2_confirmation_id", "gpu_components", "gpu_applicability"}); e != nil {
		return "", e
	}
	for _, binding := range revision.Candidate.Bindings {
		gpus := []string{}
		for _, gpu := range binding.GPUs {
			gpus = append(gpus, gpu.ComponentID)
		}
		cells := []string{revision.ID, strconv.FormatInt(revision.Version, 10), revision.Candidate.ScopeHash, revision.EffectiveVerdict, revision.InvalidationReason, binding.Plan.PhysicalPortID, binding.Plan.ComponentID, binding.Plan.ComponentSerial, binding.LogicalName, binding.Plan.Role, binding.Plan.Rail, binding.Observed.Interface, binding.Observed.PermanentMAC, binding.Observed.BDF, binding.Observed.SwitchID, binding.SwitchPortID, binding.Observed.CableID, binding.ConfirmationID, strings.Join(gpus, ";"), binding.GPUApplicability}
		for i := range cells {
			cells[i] = assetViewCSVCell(cells[i])
		}
		if e := w.Write(cells); e != nil {
			return "", e
		}
	}
	w.Flush()
	return out.String(), w.Error()
}
