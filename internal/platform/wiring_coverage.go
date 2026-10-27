package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/enzoOvOyu/keel/internal/wiring"
	"time"
)

// recordWiringCoverageTx persists original port proofs before computing a full
// Step 1 denominator. It does not itself publish StepReports or a G2 verdict.
func recordWiringCoverageTx(ctx context.Context, tx *sql.Tx, c *WiringCommand, ports map[string]wiring.Result, sha, owner string, stopped, until time.Time) (*wiring.Coverage, error) {
	var retained []byte
	e := tx.QueryRowContext(ctx, `SELECT coverage FROM validation_wiring_coverage_snapshots WHERE command_id=$1`, c.Binding.CommandID).Scan(&retained)
	if e == nil {
		var out wiring.Coverage
		if e = json.Unmarshal(retained, &out); e != nil {
			return nil, e
		}
		return &out, nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return nil, e
	}
	if len(ports) != len(c.Expectation.Ports) {
		return nil, wiring.ErrCaptureRejected
	}
	for _, p := range c.Expectation.Ports {
		result, ok := ports[p.PhysicalPortID]
		if !ok {
			return nil, wiring.ErrCaptureRejected
		}
		id := RandomID()
		three := ""
		if result.Step2 == "PASS" {
			three = RandomID()
		}
		proof := wiring.PortProof{HealthChecks: result.HealthChecks, PhysicalPortID: p.PhysicalPortID, ParentID: c.ParentID, Step2ID: id, Step3ID: three, CommandID: c.Binding.CommandID, RawSHA256: sha, ScopeHash: c.Expectation.ScopeHash, PolicyHash: c.PolicyHash, HostOrigin: c.HostOrigin, SwitchOrigin: c.Binding.Origin, Expected: p, Step2: result.Step2, Step3: result.Step3, Findings: result.Findings, ObservedAt: stopped, ValidUntil: until}
		_, e = tx.ExecContext(ctx, `INSERT INTO validation_wiring_port_evidence(id,command_id,physical_port_id,parent_id,proof,observed_at,valid_until) VALUES($1,$2,$3,$4,$5,$6,$7)`, id, c.Binding.CommandID, p.PhysicalPortID, c.ParentID, networkJSON(proof), stopped, until)
		if e != nil {
			return nil, e
		}
	}
	var expectedRaw []byte
	if e = tx.QueryRowContext(ctx, `SELECT expectation FROM validation_wiring_batch_targets WHERE batch_id=$1 AND asset_id=$2 AND project_id=$3`, c.BatchID, c.AssetID, c.Binding.ProjectID).Scan(&expectedRaw); e != nil {
		return nil, e
	}
	var expected wiring.Expectation
	if json.Unmarshal(expectedRaw, &expected) != nil {
		return nil, wiring.ErrCaptureRejected
	}
	rows, e := tx.QueryContext(ctx, `SELECT DISTINCT ON(p.physical_port_id) p.proof FROM validation_wiring_port_evidence p JOIN validation_wiring_commands source ON source.command_id=p.command_id WHERE source.project_id=$1 AND source.batch_id=$2 AND source.asset_id=$3 AND source.command->>'host_origin'=$4 AND source.origin=$5 AND p.parent_id=$6 ORDER BY p.physical_port_id,p.observed_at DESC,p.command_id DESC`, c.Binding.ProjectID, c.BatchID, c.AssetID, c.HostOrigin, c.Binding.Origin, c.ParentID)
	if e != nil {
		return nil, e
	}
	candidates := []wiring.PortProof{}
	for rows.Next() {
		var raw []byte
		var p wiring.PortProof
		if e = rows.Scan(&raw); e != nil {
			rows.Close()
			return nil, e
		}
		if e = json.Unmarshal(raw, &p); e != nil {
			rows.Close()
			return nil, e
		}
		candidates = append(candidates, p)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	coverage, e := wiring.ComposeCoverage(expected, c.ParentID, c.PolicyHash, candidates, time.Now().UTC())
	if e != nil {
		return nil, e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO validation_wiring_coverage_snapshots(id,command_id,project_id,batch_id,asset_id,parent_id,source_hash,coverage,created_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, RandomID(), c.Binding.CommandID, c.Binding.ProjectID, c.BatchID, c.AssetID, c.ParentID, coverage.SourceHash, networkJSON(coverage), owner)
	return coverage, e
}
