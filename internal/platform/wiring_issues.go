package platform

import (
	"context"
	"database/sql"
	"errors"
	"github.com/enzoOvOyu/keel/internal/wiring"
	"slices"
	"time"
)

// Missing, unreadable and unknown observations remain BLOCKED. Only concrete
// evaluator failures create a physical-port issue, before any G3 mapping exists.
func recordWiringIssuesTx(ctx context.Context, tx *sql.Tx, c *WiringCommand, result wiring.Result, owner string, observed time.Time, sha string) error {
	actor := &AuthSession{User: UserSummary{ID: owner}}
	ports := map[string]wiring.ExpectedPort{}
	for _, p := range c.Expectation.Ports {
		ports[p.PhysicalPortID] = p
	}
	for _, finding := range result.Findings {
		if finding.Verdict != "FAIL" || !slices.Contains([]string{"T1", "T2", "T6"}, finding.Category) {
			continue
		}
		port, ok := ports[finding.PhysicalPortID]
		if !ok || finding.CableID != port.CableID {
			return wiring.ErrCaptureRejected
		}
		var report string
		if e := tx.QueryRowContext(ctx, `SELECT id FROM validation_wiring_step_reports WHERE detail->>'command_id'=$1 AND step=$2`, c.Binding.CommandID, finding.Step).Scan(&report); e != nil {
			return e
		}
		key := "wiring-issue:" + c.Binding.ProjectID + ":" + c.AssetID + ":" + port.PhysicalPortID + ":" + finding.Category + ":" + c.HostOrigin + ":" + c.Binding.Origin
		if e := lockKey(ctx, tx, key); e != nil {
			return e
		}
		var id string
		e := tx.QueryRowContext(ctx, `SELECT issue_id FROM validation_wiring_issue_bindings WHERE project_id=$1 AND asset_id=$2 AND physical_port_id=$3 AND category=$4 AND host_origin=$5 AND switch_origin=$6`, c.Binding.ProjectID, c.AssetID, port.PhysicalPortID, finding.Category, c.HostOrigin, c.Binding.Origin).Scan(&id)
		fresh := errors.Is(e, sql.ErrNoRows)
		if e != nil && !fresh {
			return e
		}
		if fresh {
			in := CreateIssueInput{Title: "Wiring: " + finding.Category + " / " + finding.Code, Category: finding.Category, Source: "g2", Phase: "delivery", Anchors: []IssueAnchor{{Kind: "machine", ID: c.Expectation.MachineID}, {Kind: "component", ID: port.ComponentID}, {Kind: "port", ID: port.PhysicalPortID}, {Kind: "cable", ID: port.CableID}}, Evidence: IssueEvidence{ObservedAt: observed, SourceVersion: wiring.RuleVersion, Expected: string(networkJSON(finding.Expected)), Actual: string(networkJSON(map[string]any{"finding": finding, "raw_sha256": sha, "command_id": c.Binding.CommandID})), ExecutionID: c.Binding.ExecutionID}}
			if len(in.Evidence.Actual) > 3800 || len(in.Evidence.Expected) > 3800 {
				in.Evidence.Expected = string(networkJSON(map[string]string{"raw_sha256": sha, "field": "finding.expected"}))
				in.Evidence.Actual = string(networkJSON(map[string]any{"command_id": c.Binding.CommandID, "raw_sha256": sha, "physical_port_id": port.PhysicalPortID, "code": finding.Code, "finding_hash": digest(string(networkJSON(finding)))}))
			}
			if e = validIssueCreate(in); e != nil {
				return e
			}
			for _, a := range in.Anchors {
				if e = issueAnchorTx(ctx, tx, c.Binding.ProjectID, a); e != nil {
					return e
				}
			}
			item, err := insertIssueTx(ctx, tx, actor, c.Binding.ProjectID, in, "wiring", "", map[string]any{"command_id": c.Binding.CommandID, "raw_sha256": sha, "host_origin": c.HostOrigin, "switch_origin": c.Binding.Origin})
			if err != nil {
				return err
			}
			id = item.ID
			if _, e = tx.ExecContext(ctx, `INSERT INTO validation_wiring_issue_bindings(issue_id,project_id,asset_id,physical_port_id,category,host_origin,switch_origin) VALUES($1,$2,$3,$4,$5,$6,$7)`, id, c.Binding.ProjectID, c.AssetID, port.PhysicalPortID, finding.Category, c.HostOrigin, c.Binding.Origin); e != nil {
				return e
			}
		}
		changed, e := tx.ExecContext(ctx, `INSERT INTO validation_wiring_issue_observations(issue_id,report_id,command_id,finding_hash,finding,observed_at) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`, id, report, c.Binding.CommandID, digest(string(networkJSON(finding))), networkJSON(finding), observed)
		if e != nil {
			return e
		}
		n, e := changed.RowsAffected()
		if e != nil {
			return e
		}
		if !fresh && n > 0 {
			var state string
			if e = tx.QueryRowContext(ctx, `SELECT state FROM issues WHERE id=$1 FOR UPDATE`, id).Scan(&state); e != nil {
				return e
			}
			action := "observe"
			if state == "closed" {
				var closed time.Time
				if e = tx.QueryRowContext(ctx, `SELECT updated_at FROM issues WHERE id=$1`, id).Scan(&closed); e != nil {
					return e
				}
				if closed.After(observed) {
					continue
				} // Late historical evidence cannot reopen a later repair.
			}
			if state == "closed" {
				state = "reopened"
				action = "reopen"
			}
			item, e := scanIssue(tx.QueryRowContext(ctx, `UPDATE issues SET state=$2,version=version+1,updated_at=clock_timestamp() WHERE id=$1 RETURNING `+issueColumns, id, state))
			if e != nil {
				return e
			}
			if e = issueEventTx(ctx, tx, actor, item, action, "", map[string]any{"command_id": c.Binding.CommandID, "report_id": report, "raw_sha256": sha, "finding": finding}); e != nil {
				return e
			}
		}
	}
	return nil
}
