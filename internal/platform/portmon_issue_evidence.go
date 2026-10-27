package platform

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Discovery is immutable. Current observations are separately labelled and do
// not constitute a post-repair Execution or authority to close an issue.
type PortMonitorIssueEvidence struct {
	IssueID                  string             `json:"issue_id"`
	AssetID                  string             `json:"asset_id"`
	MachineID                string             `json:"machine_id"`
	AssetVersion             int64              `json:"asset_version"`
	PhysicalPortID           string             `json:"physical_port_id"`
	MappingRevisionID        string             `json:"mapping_revision_id"`
	RuleID                   string             `json:"rule_id"`
	RuleVersion              string             `json:"rule_version"`
	Initial                  MonitorObservation `json:"initial"`
	Current                  *PortMonitorItem   `json:"current"`
	CurrentMappingState      string             `json:"current_mapping_state"`
	CurrentMappingRevisionID string             `json:"current_mapping_revision_id"`
	RepairVersion            int                `json:"repair_version"`
	RepairedAt               *time.Time         `json:"repaired_at"`
	RequiredChecks           []string           `json:"required_checks"`
}

func (s *Store) PortMonitorIssueEvidence(ctx context.Context, a *AuthSession, issue string) (*PortMonitorIssueEvidence, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	item, e := scanIssue(tx.QueryRowContext(ctx, `SELECT `+issueColumns+` FROM issues WHERE id=$1`, issue))
	if errors.Is(e, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if e != nil {
		return nil, e
	}
	if _, e = issueRoleTx(ctx, tx, a, item.ProjectID); e != nil {
		return nil, e
	}
	if item.Source != "portmon" {
		return nil, apiError(409, "PORTMON_ISSUE_REQUIRED", "Original port monitoring discovery is required")
	}
	out := &PortMonitorIssueEvidence{IssueID: issue, RequiredChecks: append(issueVerificationChecks(item.Category), "original_monitor_rule")}
	var sample int64
	e = tx.QueryRowContext(ctx, `SELECT asset_id,machine_id,asset_version,physical_port_id,mapping_revision_id,sample_sequence,rule_id,rule_version FROM portmon_issue_bindings WHERE issue_id=$1 AND project_id=$2`, issue, item.ProjectID).Scan(&out.AssetID, &out.MachineID, &out.AssetVersion, &out.PhysicalPortID, &out.MappingRevisionID, &sample, &out.RuleID, &out.RuleVersion)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, apiError(409, "PORTMON_ORIGINAL_BINDING_REQUIRED", "This record has no immutable original discovery binding; current alert state cannot replace it")
	}
	if e != nil {
		return nil, e
	}
	initial, e := scanMonitorObservation(tx.QueryRowContext(ctx, `SELECT `+monitorObservationFields+` FROM monitor_metric_samples sample WHERE sample.sequence=$1`, sample), time.Now().UTC())
	if e != nil {
		return nil, e
	}
	out.Initial = *initial
	if out.RuleID == "link-flapping" {
		out.RequiredChecks = append(out.RequiredChecks, "continuous_post_repair_flapping_window")
	}
	e = tx.QueryRowContext(ctx, `SELECT version,created_at FROM issue_events WHERE issue_id=$1 AND action='repair' ORDER BY version DESC LIMIT 1`, issue).Scan(&out.RepairVersion, &out.RepairedAt)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return nil, e
	}
	current, e := s.portMonitorSnapshotTx(ctx, tx, item.ProjectID, out.AssetID)
	if e != nil {
		return nil, e
	}
	out.CurrentMappingState = current.MappingState
	out.CurrentMappingRevisionID = current.MappingRevisionID
	for i := range current.Items {
		if current.Items[i].PhysicalPortID == out.PhysicalPortID {
			out.Current = &current.Items[i]
			break
		}
	}
	return out, tx.Commit()
}
