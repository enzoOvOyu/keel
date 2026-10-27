package platform

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"

	"github.com/enzoOvOyu/keel/internal/fabricboot"
	"github.com/enzoOvOyu/keel/internal/portdiag"
)

// Shared issue verification carries this server-frozen extension. The request
// authorizes one physical host/peer link and cannot select arbitrary commands.
type PortMonitorVerificationScope struct {
	AssetID           string         `json:"asset_id"`
	AssetVersion      int64          `json:"asset_version"`
	MappingRevisionID string         `json:"mapping_revision_id"`
	HostTrustHash     string         `json:"host_trust_hash"`
	RuleVersion       string         `json:"rule_version"`
	Diagnostic        portdiag.Scope `json:"diagnostic"`
}

func (s *Store) portMonitorVerificationScopeTx(ctx context.Context, tx *sql.Tx, item *Issue, out *IssueVerificationScope) (*IssueVerificationScope, error) {
	block := func(code string) { out.Blockers = append(out.Blockers, code) }
	if !s.MonitorEnabled || !s.FabricIssueVerificationEnabled {
		block("targeted_execution_disabled")
	}
	if s.FabricProbes == nil || !s.FabricProbes.CollectorConfigured() {
		block("trusted_collector_required")
	}
	var asset, port, rule, source, method, origin string
	var repaired time.Time
	e := tx.QueryRowContext(ctx, `SELECT discovery.asset_id,discovery.physical_port_id,discovery.rule_id,sample.source,sample.method,sample.origin FROM portmon_issue_bindings discovery JOIN monitor_metric_samples sample ON sample.sequence=discovery.sample_sequence WHERE discovery.issue_id=$1 AND discovery.project_id=$2`, item.ID, item.ProjectID).Scan(&asset, &port, &rule, &source, &method, &origin)
	if errors.Is(e, sql.ErrNoRows) {
		block("portmon_original_binding_required")
		return finishIssueVerificationScope(out), nil
	}
	if e != nil {
		return nil, e
	}
	e = tx.QueryRowContext(ctx, `SELECT created_at FROM issue_events WHERE issue_id=$1 AND action='repair' AND version=$2`, item.ID, out.RepairVersion).Scan(&repaired)
	if errors.Is(e, sql.ErrNoRows) {
		block("current_repair_record_required")
		return finishIssueVerificationScope(out), nil
	}
	if e != nil {
		return nil, e
	}
	var used bool
	if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM issue_verification_requests WHERE issue_id=$1 AND snapshot->>'repair_version'=$2 AND issue_version<>$3)`, item.ID, strconv.Itoa(out.RepairVersion), item.Version).Scan(&used); e != nil {
		return nil, e
	}
	if used {
		block("repair_already_verified")
	}
	revision, e := scanMappingRevision(tx.QueryRowContext(ctx, `SELECT `+mappingRevisionFields+` FROM mapping_revisions r JOIN mapping_assessments a ON a.id=r.assessment_id WHERE r.project_id=$1 AND r.asset_id=$2 ORDER BY r.version DESC LIMIT 1`, item.ProjectID, asset))
	if errors.Is(e, ErrNotFound) {
		block("current_physical_mapping_required")
		return finishIssueVerificationScope(out), nil
	}
	if e != nil {
		return nil, e
	}
	if e = s.mappingValidityTx(ctx, tx, revision); e != nil {
		return nil, e
	}
	if revision.EffectiveVerdict != "PASS" {
		block("current_physical_mapping_required")
	}
	var selected *MappingBinding
	for i := range revision.Candidate.Bindings {
		if revision.Candidate.Bindings[i].Plan.PhysicalPortID == port {
			selected = &revision.Candidate.Bindings[i]
			break
		}
	}
	if selected == nil || selected.Plan.Role == "unused" {
		block("enabled_physical_port_required")
		return finishIssueVerificationScope(out), nil
	}
	var serial, state, projectState string
	var assetVersion int64
	if e = tx.QueryRowContext(ctx, `SELECT machine.serial_number,asset.version,asset.state,project.state FROM project_assets asset JOIN machines machine ON machine.id=asset.machine_id JOIN projects project ON project.id=asset.project_id WHERE asset.id=$1 AND asset.project_id=$2`, asset, item.ProjectID).Scan(&serial, &assetVersion, &state, &projectState); e != nil {
		return nil, e
	}
	if state == "maintained" || projectState == "archived" || projectState == "delivered" {
		block("project_or_channel_not_active")
	}
	var trusted bool
	if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM machines WHERE id=$1 AND ssh_trust='established' AND ssh_trust_source='ssh') OR EXISTS(SELECT 1 FROM agent_credentials identity JOIN agent_connections connection ON connection.credential_id=identity.id WHERE identity.project_id=$2 AND identity.machine_id=$1 AND identity.revoked_at IS NULL AND identity.expires_at>clock_timestamp() AND connection.disconnected_at IS NULL AND connection.last_seen_at>clock_timestamp()-interval '30 seconds')`, revision.Candidate.MachineID, item.ProjectID).Scan(&trusted); e != nil {
		return nil, e
	}
	if !trusted {
		block("current_host_trust_required")
	}
	hostHash, e := repairHostIdentityTx(ctx, tx, revision.Candidate.MachineID, item.ProjectID)
	if e != nil {
		return nil, e
	}
	limits, e := monitorLimitsTx(ctx, tx, item.ProjectID)
	if e != nil {
		return nil, e
	}
	expected := selected.Observed
	diagnostic := portdiag.Scope{ProjectID: item.ProjectID, IssueID: item.ID, MachineID: revision.Candidate.MachineID, Serial: serial, PhysicalPortID: port, Interface: expected.Interface, PermanentMAC: expected.PermanentMAC, BDF: expected.BDF, SwitchID: expected.SwitchID, SwitchSerial: expected.SwitchSerial, SwitchPort: expected.SwitchPort, Category: item.Category, RuleID: rule, Source: source, Method: method, Origin: origin, RepairedAt: repaired, Limits: limits}
	out.PortMonitor = &PortMonitorVerificationScope{AssetID: asset, AssetVersion: assetVersion, HostTrustHash: hostHash, MappingRevisionID: revision.ID, RuleVersion: "monitor/v1:" + digest(string(networkJSON(limits))), Diagnostic: diagnostic}
	out.PortUID = port
	out.RuleRef = out.PortMonitor.RuleVersion
	out.Targets = []fabricboot.CollectorTarget{{SwitchID: expected.SwitchID, Serial: expected.SwitchSerial, Ports: []string{expected.SwitchPort}}}
	out.EvidenceOrigin = "device"
	if origin == "sim" {
		out.EvidenceOrigin = "protocol_fixture"
	}
	out.PolicyHash = digest(string(networkJSON(limits)))
	// Read the same management/credential binding as other scoped read-only jobs.
	management, e := s.issueVerificationManagementHashTx(ctx, tx, out)
	if e != nil {
		var domain *Error
		if errors.As(e, &domain) && domain.Status == 409 {
			block("current_switch_management_required")
		} else {
			return nil, e
		}
	} else {
		out.ManagementHash = management
	}
	if revision.Candidate.ValidUntil != nil && revision.Candidate.ValidUntil.Before(out.ValidUntil) {
		out.ValidUntil = *revision.Candidate.ValidUntil
	}
	if rule == "link-flapping" {
		out.RequiredChecks = append(out.RequiredChecks, "continuous_post_repair_flapping_window")
	}
	out.RequiredChecks = append(out.RequiredChecks, "original_monitor_rule")
	if item.Category == "T6" {
		var expectedHash string
		e = tx.QueryRowContext(ctx, latestNetworkTemplateDevicesCTE+` SELECT facts->>'intended_hash' FROM latest WHERE switch_id=$2 AND device_state='VERIFIED' AND execution_state='success'`, item.ProjectID, expected.SwitchID).Scan(&expectedHash)
		if errors.Is(e, sql.ErrNoRows) {
			block("approved_configuration_readback_scope_required")
		} else if e != nil {
			return nil, e
		} else if expectedHash == "" {
			block("approved_configuration_readback_scope_required")
		} else {
			out.PortMonitor.Diagnostic.ExpectedConfigurationHash = expectedHash
		}
	}
	out.ExecutionAvailable = len(out.Blockers) == 0
	return finishIssueVerificationScope(out), nil
}
