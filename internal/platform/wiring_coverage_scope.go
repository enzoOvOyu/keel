package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/enzoOvOyu/keel/internal/wiring"
)

// An aggregate report is an index of original observations, never a synthetic
// capture attributed to its last command. Every retained port is revalidated.
func (s *Store) wiringReportScopeTx(ctx context.Context, tx *sql.Tx, project string, report *WiringStepReport, ignoreIssues bool) (*WiringCommand, *WiringFacts, error) {
	if report.Detail.CoverageID == "" {
		return s.wiringObservedScopeCheckTx(ctx, tx, project, report.Detail.CommandID, ignoreIssues)
	}
	var raw, expectedRaw []byte
	e := tx.QueryRowContext(ctx, `SELECT snapshot.coverage,target.expectation FROM validation_wiring_coverage_snapshots snapshot JOIN validation_wiring_batch_targets target ON target.batch_id=snapshot.batch_id AND target.asset_id=snapshot.asset_id WHERE snapshot.id=$1 AND snapshot.project_id=$2 AND snapshot.command_id=$3 AND snapshot.source_hash=$4`, report.Detail.CoverageID, project, report.Detail.CommandID, report.SourceHash).Scan(&raw, &expectedRaw)
	if e != nil {
		return nil, nil, e
	}
	var retained wiring.Coverage
	var expected wiring.Expectation
	if json.Unmarshal(raw, &retained) != nil || json.Unmarshal(expectedRaw, &expected) != nil {
		return nil, nil, wiring.ErrCaptureRejected
	}
	_, policy, e := wiringReportPolicyTx(ctx, tx, project)
	if e != nil {
		return nil, nil, e
	}
	current, e := wiring.ComposeCoverage(expected, retained.ParentID, policy, retained.Proofs, time.Now().UTC())
	if e != nil {
		return nil, nil, e
	}
	if current.SourceHash != retained.SourceHash || current.Step2 != "PASS" || current.Step3 != "PASS" {
		return nil, nil, apiError(409, "WIRING_COVERAGE_NOT_CURRENT", "Every original Step 1 port requires current real passing evidence")
	}
	var anchor *WiringCommand
	var anchorFacts *WiringFacts
	checked := map[string]bool{}
	for _, proof := range retained.Proofs {
		var superseded bool
		e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM validation_wiring_commands c JOIN validation_wiring_capture_records evidence ON evidence.command_id=c.command_id WHERE c.project_id=$1 AND c.asset_id=$2 AND c.command->>'host_origin'='real' AND c.origin='device' AND (evidence.stopped_at,c.command_id)>($3::timestamptz,$4::varchar) AND EXISTS(SELECT 1 FROM jsonb_array_elements(c.command#>'{expectation,ports}') p WHERE p->>'physical_port_id'=$5))`, project, expected.AssetID, proof.ObservedAt, proof.CommandID, proof.PhysicalPortID).Scan(&superseded)
		if e != nil {
			return nil, nil, e
		}
		if superseded {
			return nil, nil, apiError(409, "WIRING_OBSERVATION_SUPERSEDED", "A later real observation supersedes a retained physical port")
		}
		if checked[proof.CommandID] {
			continue
		}
		checked[proof.CommandID] = true
		var commandRaw []byte
		e = tx.QueryRowContext(ctx, `SELECT c.command FROM validation_wiring_commands c JOIN exec_task_attempts a ON a.id=c.attempt_id JOIN validation_wiring_capture_records evidence ON evidence.command_id=c.command_id WHERE c.command_id=$1 AND c.project_id=$2 AND a.command_id=c.command_id AND a.fencing_token=c.fencing_token AND a.state IN('success','failed') AND evidence.raw_sha256=$3`, proof.CommandID, project, proof.RawSHA256).Scan(&commandRaw)
		if e == sql.ErrNoRows {
			return nil, nil, apiError(409, "WIRING_ORIGINAL_NOT_SETTLED", "Original physical-port capture must be settled")
		}
		if e != nil {
			return nil, nil, e
		}
		var c WiringCommand
		if json.Unmarshal(commandRaw, &c) != nil || c.HostOrigin != "real" || c.Binding.Origin != "device" {
			return nil, nil, wiring.ErrCaptureRejected
		}
		scope, e := loadWiringRunScopeTx(ctx, tx, c.Binding.ExecutionID, c.Expectation.MachineID)
		if e != nil {
			return nil, nil, e
		}
		input := WiringTargetInput{AssetID: c.AssetID, PhysicalPortIDs: []string{}}
		for _, p := range c.Expectation.Ports {
			input.PhysicalPortIDs = append(input.PhysicalPortIDs, p.PhysicalPortID)
		}
		facts, e := s.wiringFactsTx(ctx, tx, project, []WiringTargetInput{input}, false)
		if e != nil {
			return nil, nil, e
		}
		if !facts.Ready || wiringIdentityHash(facts) != scope.IdentityHash {
			return nil, nil, apiError(409, "WIRING_SCOPE_CHANGED", "Original topology, BOM or network prerequisites changed")
		}
		if anchor == nil || c.Binding.CommandID == report.Detail.CommandID {
			anchor, anchorFacts = &c, facts
		}
	}
	if anchor == nil {
		return nil, nil, wiring.ErrCaptureRejected
	}
	if !ignoreIssues {
		var open bool
		e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM validation_wiring_issue_bindings b JOIN issues i ON i.id=b.issue_id WHERE b.project_id=$1 AND b.asset_id=$2 AND b.host_origin='real' AND b.switch_origin='device' AND i.state<>'closed' AND b.physical_port_id IN(SELECT p->>'physical_port_id' FROM jsonb_array_elements($3::jsonb) p))`, project, expected.AssetID, networkJSON(expected.Ports)).Scan(&open)
		if e != nil {
			return nil, nil, e
		}
		if open {
			return nil, nil, apiError(409, "WIRING_OPEN_ISSUES", "Close affected wiring issues before G2 confirmation")
		}
	}
	// Return the report's anchor identity with the full confirmed denominator.
	// This value is only a gate projection and is never submitted to a runner.
	if anchor.Binding.CommandID != report.Detail.CommandID {
		var commandRaw []byte
		if e = tx.QueryRowContext(ctx, `SELECT command FROM validation_wiring_commands WHERE command_id=$1 AND project_id=$2`, report.Detail.CommandID, project).Scan(&commandRaw); e != nil {
			return nil, nil, e
		}
		if json.Unmarshal(commandRaw, anchor) != nil {
			return nil, nil, wiring.ErrCaptureRejected
		}
	}
	anchor.Expectation = expected
	return anchor, anchorFacts, nil
}
