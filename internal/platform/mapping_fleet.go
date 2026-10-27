package platform

import (
	"context"
	"encoding/json"
	"time"
	"unicode/utf8"
)

type MappingFleetItem struct {
	AssetID   string           `json:"asset_id"`
	MachineID string           `json:"machine_id"`
	Hostname  string           `json:"hostname"`
	Serial    string           `json:"serial"`
	State     string           `json:"state"`
	Revision  *MappingRevision `json:"revision"`
}
type MappingFleetPage struct {
	ProjectID string             `json:"project_id"`
	Rail      string             `json:"rail"`
	AsOf      time.Time          `json:"as_of"`
	Items     []MappingFleetItem `json:"items"`
	Total     int                `json:"total"`
	Page      int                `json:"page"`
	PageSize  int                `json:"page_size"`
}

// A rail view filters published bindings of the SAME selected revisions. It
// never creates a smaller gate denominator. Unbuilt assets remain visible in
// the all-server view, and never contribute to mapped coverage.
func (s *Store) MappingFleet(ctx context.Context, a *AuthSession, project, rail string, page Pagination) (*MappingFleetPage, error) {
	if e := page.Validate(); e != nil {
		return nil, e
	}
	if utf8.RuneCountInString(rail) > 64 {
		return nil, apiError(400, "VALIDATION_ERROR", "Rail filter is too long")
	}
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireProjectMember(ctx, tx, a, project); e != nil {
		return nil, e
	}
	out := &MappingFleetPage{ProjectID: project, Rail: rail, Items: []MappingFleetItem{}, Page: page.Page, PageSize: page.PageSize}
	from := ` FROM project_assets asset JOIN machines machine ON machine.id=asset.machine_id LEFT JOIN LATERAL(SELECT * FROM mapping_revisions r WHERE r.project_id=asset.project_id AND r.asset_id=asset.id ORDER BY version DESC LIMIT 1) revision ON true LEFT JOIN mapping_assessments assessment ON assessment.id=revision.assessment_id WHERE asset.project_id=$1 AND ($2='' OR EXISTS(SELECT 1 FROM jsonb_array_elements(assessment.candidate->'bindings') b WHERE b#>>'{plan,rail}'=$2))`
	if e = tx.QueryRowContext(ctx, `SELECT count(*)`+from, project, rail).Scan(&out.Total); e != nil {
		return nil, e
	}
	rows, e := tx.QueryContext(ctx, `SELECT asset.id,asset.machine_id,asset.hostname,machine.serial_number,coalesce(revision.id,''),coalesce(revision.assessment_id,''),coalesce(revision.version,0),coalesce(revision.previous_id,''),coalesce(assessment.candidate,'{}'::jsonb),coalesce(revision.confirmed_by,''),coalesce(revision.created_at,asset.created_at),coalesce(assessment.reason,'')`+from+` ORDER BY asset.hostname,asset.id LIMIT $3 OFFSET $4`, project, rail, page.PageSize, page.Offset())
	if e != nil {
		return nil, e
	}
	for rows.Next() {
		var item MappingFleetItem
		var revision MappingRevision
		var raw []byte
		if e = rows.Scan(&item.AssetID, &item.MachineID, &item.Hostname, &item.Serial, &revision.ID, &revision.AssessmentID, &revision.Version, &revision.PreviousID, &raw, &revision.ConfirmedBy, &revision.CreatedAt, &revision.Reason); e != nil {
			rows.Close()
			return nil, e
		}
		item.State = "unbuilt"
		if revision.ID != "" {
			if e = json.Unmarshal(raw, &revision.Candidate); e != nil {
				rows.Close()
				return nil, e
			}
			revision.EffectiveVerdict = "PASS"
			item.Revision = &revision
		}
		out.Items = append(out.Items, item)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	for i := range out.Items {
		item := &out.Items[i]
		if item.Revision == nil {
			continue
		}
		if e = s.mappingValidityTx(ctx, tx, item.Revision); e != nil {
			return nil, e
		}
		item.State = "verified"
		if item.Revision.EffectiveVerdict != "PASS" {
			item.State = "blocked"
		}
	}
	out.AsOf = time.Now().UTC()
	return out, tx.Commit()
}
