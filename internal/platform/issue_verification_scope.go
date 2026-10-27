package platform

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"time"

	"github.com/enzoOvOyu/keel/internal/fabricboot"
	"github.com/enzoOvOyu/keel/internal/fabricmon"
	"github.com/enzoOvOyu/keel/internal/netbaseline"
)

// A scope preview is not an execution or successful verification. Its digest
// identifies current business facts; admission must reproduce them atomically.
type IssueVerificationScope struct {
	RuleRef            string                       `json:"rule_ref"`
	Version            string                       `json:"version"`
	IssueID            string                       `json:"issue_id"`
	ProjectID          string                       `json:"project_id"`
	IssueVersion       int64                        `json:"issue_version"`
	RepairVersion      int64                        `json:"repair_version"`
	Category           string                       `json:"category"`
	PackageID          string                       `json:"package_id"`
	EvidenceOrigin     string                       `json:"evidence_origin"`
	CableUID           string                       `json:"cable_uid"`
	PortUID            string                       `json:"port_uid"`
	Targets            []fabricboot.CollectorTarget `json:"targets"`
	RequiredChecks     []string                     `json:"required_checks"`
	Blockers           []string                     `json:"blockers"`
	PolicyHash         string                       `json:"policy_hash"`
	IntentHash         string                       `json:"intent_hash"`
	ScopeHash          string                       `json:"scope_hash"`
	ValidUntil         time.Time                    `json:"valid_until"`
	ExecutionAvailable bool                         `json:"execution_available"`
}

func issueVerificationChecks(category string) []string {
	switch category {
	case "T1", "T2":
		return []string{"lldp_single_link", "switch_readback"}
	case "T3", "T4":
		return []string{"lldp_single_link", "ddm_readback", "connectivity_single_path"}
	case "T5":
		return []string{"host_precollect_incremental", "lldp_single_link", "connectivity_single_path"}
	case "T6":
		return []string{"configuration_readback", "connectivity_single_path"}
	case "T7":
		return []string{"pcie_topology", "host_precollect_incremental"}
	}
	return []string{}
}
func (s *Store) IssueVerificationScope(ctx context.Context, a *AuthSession, id string) (*IssueVerificationScope, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	item, e := scanIssue(tx.QueryRowContext(ctx, "SELECT "+issueColumns+" FROM issues WHERE id=$1", id))
	if errors.Is(e, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if e != nil {
		return nil, e
	}
	role, e := issueRoleTx(ctx, tx, a, item.ProjectID)
	if e != nil {
		return nil, e
	}
	out, e := s.issueVerificationScopeTx(ctx, tx, a, item, role)
	if e != nil {
		return nil, e
	}
	return out, tx.Commit()
}
func (s *Store) issueVerificationScopeTx(ctx context.Context, tx *sql.Tx, a *AuthSession, item *Issue, role string) (*IssueVerificationScope, error) {
	out := &IssueVerificationScope{Version: "issue-verification-scope/v1", IssueID: item.ID, ProjectID: item.ProjectID, IssueVersion: item.Version, Category: item.Category, Targets: []fabricboot.CollectorTarget{}, RequiredChecks: issueVerificationChecks(item.Category), Blockers: []string{}, ValidUntil: time.Now().UTC().Add(30 * time.Second)}
	block := func(reason string) {
		if !slices.Contains(out.Blockers, reason) {
			out.Blockers = append(out.Blockers, reason)
		}
	}
	if !slices.Contains([]string{"Admin", "DL", "NE"}, role) || role == "NE" && item.AssigneeID != a.User.ID {
		block("issue_verification_authority_required")
	}
	if item.State != "in_repair" {
		block("issue_must_be_in_repair")
	}
	if e := tx.QueryRowContext(ctx, "SELECT coalesce(max(version),0) FROM issue_events WHERE issue_id=$1 AND action='repair'", item.ID).Scan(&out.RepairVersion); e != nil {
		return nil, e
	}
	var assignmentVersion int64
	if e := tx.QueryRowContext(ctx, "SELECT coalesce(max(version),0) FROM issue_events WHERE issue_id=$1 AND action IN('assign','reopen')", item.ID).Scan(&assignmentVersion); e != nil {
		return nil, e
	}
	if out.RepairVersion == 0 || out.RepairVersion <= assignmentVersion {
		block("current_repair_record_required")
	}
	if item.Source != "fabric" {
		block("source_verification_adapter_pending")
		return finishIssueVerificationScope(out), nil
	}
	var state string
	if e := tx.QueryRowContext(ctx, "SELECT state FROM projects WHERE id=$1", item.ProjectID).Scan(&state); e != nil {
		return nil, e
	}
	if state == "archived" || state == "delivered" {
		block("project_not_active")
	}
	e := tx.QueryRowContext(ctx, "SELECT cable_uid,evidence_origin FROM fabric_issue_bindings WHERE issue_id=$1", item.ID).Scan(&out.CableUID, &out.EvidenceOrigin)
	if errors.Is(e, sql.ErrNoRows) {
		e = tx.QueryRowContext(ctx, "SELECT port_uid,evidence_origin FROM fabric_port_issue_bindings WHERE issue_id=$1", item.ID).Scan(&out.PortUID, &out.EvidenceOrigin)
	}
	if errors.Is(e, sql.ErrNoRows) {
		block("trusted_discovery_binding_required")
		return finishIssueVerificationScope(out), nil
	}
	if e != nil {
		return nil, e
	}
	if e = tx.QueryRowContext(ctx, `SELECT coalesce((SELECT finding->>'rule_ref' FROM fabric_issue_observations WHERE issue_id=$1 ORDER BY recorded_at DESC,id DESC LIMIT 1),(SELECT o.check_result->>'rule_ref' FROM fabric_port_issue_observations o JOIN fabric_diagnostic_records r ON r.id=o.record_id WHERE o.issue_id=$1 ORDER BY r.recorded_at DESC,r.id DESC LIMIT 1),'')`, item.ID).Scan(&out.RuleRef); e != nil {
		return nil, e
	}
	if out.RuleRef == "" {
		block("versioned_discovery_rule_required")
	}
	pkg, devices, cables, e := fabricInventoryTx(ctx, tx, item.ProjectID)
	if e != nil {
		var business *Error
		if errors.As(e, &business) && business.Status == 409 {
			block("current_network_bom_required")
			return finishIssueVerificationScope(out), nil
		}
		return nil, e
	}
	out.PackageID = pkg
	bySerial := map[string]string{}
	for _, device := range devices {
		bySerial[device.Serial] = device.ID
	}
	addEndpoint := func(endpoint fabricmon.Endpoint) {
		if !endpoint.Switch {
			block("host_side_verification_adapter_pending")
			return
		}
		switchID := bySerial[endpoint.Serial]
		if switchID == "" {
			block("target_identity_unresolved")
			return
		}
		for i := range out.Targets {
			if out.Targets[i].SwitchID == switchID {
				if !slices.Contains(out.Targets[i].Ports, endpoint.Port) {
					out.Targets[i].Ports = append(out.Targets[i].Ports, endpoint.Port)
				}
				return
			}
		}
		out.Targets = append(out.Targets, fabricboot.CollectorTarget{SwitchID: switchID, Serial: endpoint.Serial, Ports: []string{endpoint.Port}})
	}
	matched := false
	for _, cable := range cables {
		if out.CableUID != "" && cable.UID == out.CableUID || out.PortUID != "" && (cable.A.UID == out.PortUID || cable.B.UID == out.PortUID) {
			if matched {
				block("ambiguous_link_scope")
				continue
			}
			matched = true
			addEndpoint(cable.A)
			addEndpoint(cable.B)
		}
	}
	if !matched {
		block("current_bom_link_unresolved")
	}
	slices.SortFunc(out.Targets, func(a, b fabricboot.CollectorTarget) int {
		if a.SwitchID < b.SwitchID {
			return -1
		}
		if a.SwitchID > b.SwitchID {
			return 1
		}
		return 0
	})
	for i := range out.Targets {
		slices.Sort(out.Targets[i].Ports)
	}
	policy, e := fabricCounterPolicyTx(ctx, tx, item.ProjectID)
	if e != nil {
		return nil, e
	}
	limits, e := fabricDDMLimitsTx(ctx, tx, item.ProjectID)
	if e != nil {
		return nil, e
	}
	out.PolicyHash = netbaseline.ContentHash([]any{policy, limits})
	intents, e := loadLosslessIntentTx(ctx, tx, item.ProjectID)
	if e != nil {
		return nil, e
	}
	out.IntentHash = netbaseline.ContentHash(intents)
	if out.PortUID != "" && item.Category == "T2" {
		out.RequiredChecks = append(out.RequiredChecks, "rail_intent_readback")
		if len(intents) == 0 {
			block("approved_rail_intent_required")
		}
	}
	if slices.Contains(out.RequiredChecks, "connectivity_single_path") {
		block("connectivity_verification_adapter_pending")
	}
	if !s.FabricProbes.CollectorConfigured() {
		block("trusted_collector_required")
	}
	// This preview deliberately cannot dispatch. The shared execution admission
	// and result consumer must be connected before the UI offers submission.
	block("targeted_execution_adapter_pending")
	return finishIssueVerificationScope(out), nil
}
func finishIssueVerificationScope(out *IssueVerificationScope) *IssueVerificationScope {
	copy := *out
	copy.ValidUntil = time.Time{}
	copy.ScopeHash = ""
	out.ScopeHash = netbaseline.ContentHash(copy)
	return out
}
