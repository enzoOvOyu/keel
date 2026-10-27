package execution

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/enzoOvOyu/keel/internal/platform"
)

func (s *Service) CreateTopologyFirmwareExecution(ctx context.Context, a *Actor, project, issue, planID string, in platform.TopologyFirmwareApproval, key, trace string) (*Execution, error) {
	if s.IssueStore == nil || !s.IssueStore.TopologyFirmwareMockEnabled {
		return nil, failure(409, "TOPOLOGY_FIRMWARE_MOCK_DISABLED", "P2 firmware simulation is disabled")
	}
	if len(in.ScopeHash) != 64 || len(in.Reason) < 3 || len(in.Reason) > 2000 {
		return nil, invalid("Exact approved scope and execution reason required")
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = advisory(ctx, tx, "keel-admin-initialization"); e != nil {
		return nil, e
	}
	if e = authorize(ctx, tx, a, project, "Admin", "DL", "NE"); e != nil {
		return nil, e
	}
	plan, e := s.IssueStore.TopologyFirmwareCurrentTx(ctx, tx, planID)
	if e != nil {
		return nil, e
	}
	if plan.ProjectID != project || plan.IssueID != issue {
		return nil, platform.ErrNotFound
	}
	idem, prior, e := idempotency(ctx, tx, a, "topology.firmware.mock:"+planID, key, in)
	if e != nil {
		return nil, e
	}
	if prior != nil {
		out, e := decode[Execution](prior)
		return &out, e
	}
	if plan.ScopeHash != in.ScopeHash || plan.ApprovedBy == "" {
		return nil, conflict("Exact current independent approval required")
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	task := Task{ID: "issue-repair", Type: "issue.repair", Retry: 0, Idempotent: false, TimeoutMS: 5000, Resources: []string{"device", "ssh", "agent"}}
	definition := Definition{APIVersion: "keel.topology-firmware-mock/v1", Inputs: map[string]Input{}, Tasks: []Task{task}}
	flow := FlowVersion{ID: platform.RandomID(), FlowID: platform.RandomID(), ProjectID: project, Name: "T7 firmware simulation " + plan.ID, Version: 1, Definition: definition, ContentHash: hash(jsonBytes(definition)), CreatedAt: now}
	if _, e = tx.ExecContext(ctx, `INSERT INTO exec_flows(id,project_id,name,created_by,created_at,updated_at)VALUES($1,$2,$3,$4,$5,$5)`, flow.FlowID, project, flow.Name, a.User.ID, now); e != nil {
		return nil, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO exec_flow_versions(id,flow_id,project_id,version,definition,content_hash,created_by,created_at)VALUES($1,$2,$3,1,$4,$5,$6,$7)`, flow.ID, flow.FlowID, project, jsonBytes(definition), flow.ContentHash, a.User.ID, now); e != nil {
		return nil, e
	}
	var targetID string
	if e = tx.QueryRowContext(ctx, `INSERT INTO exec_targets(id,project_id,kind,resource_key)VALUES($1,$2,'machine',$3) ON CONFLICT(project_id,kind,resource_key)DO UPDATE SET resource_key=EXCLUDED.resource_key RETURNING id`, platform.RandomID(), project, plan.MachineID).Scan(&targetID); e != nil {
		return nil, e
	}
	target := Target{ID: targetID, ProjectID: project, Name: plan.Serial, Revision: plan.AssetVersion, ModelVersion: plan.ModelVersionID, Mode: "fabric", ResourceKey: plan.MachineID, CreatedAt: now}
	params := map[string]any{"topology_firmware_plan_id": plan.ID, "issue_id": issue, "scope_hash": plan.ScopeHash, "origin": "sim", "hardware_evidence": false, "reason": in.Reason}
	targetHash := hash(jsonBytes(plan))
	out := &Execution{ID: platform.RandomID(), ProjectID: project, FlowVersionID: flow.ID, Mode: "fabric", State: "created", StateVersion: 1, DesiredState: "running", Parameters: params, TargetSetHash: targetHash, Concurrency: 1, CircuitBreakerPercent: 100, CreatedBy: a.User.ID, CreatedAt: now}
	out.Snapshot = Snapshot{Mode: "fabric", Flow: flow, Targets: []Target{target}, Parameters: params, TargetSetHash: targetHash, Admission: "P2 approved T7 firmware simulation only; frozen model/artifact/repair, four-eyes approval and exclusive host resources; requires separate new precollection/topology verification", FrozenAt: now}
	if _, e = tx.ExecContext(ctx, `INSERT INTO exec_executions(id,project_id,flow_version_id,mode,parameters,snapshot,target_set_hash,concurrency,circuit_breaker_percent,created_by,created_at)VALUES($1,$2,$3,'fabric',$4,$5,$6,1,100,$7,$8)`, out.ID, project, flow.ID, jsonBytes(params), jsonBytes(out.Snapshot), targetHash, a.User.ID, now); e != nil {
		return nil, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO exec_execution_targets(execution_id,target_id,snapshot)VALUES($1,$2,$3)`, out.ID, target.ID, jsonBytes(target)); e != nil {
		return nil, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO exec_task_attempts(id,execution_id,target_id,task_id,task_index,attempt_no,command_id,runner,created_at)VALUES($1,$2,$3,'issue-repair',0,1,$4,'fabric',$5)`, platform.RandomID(), out.ID, target.ID, platform.RandomID(), now); e != nil {
		return nil, e
	}
	if e = s.IssueStore.AttachTopologyFirmwareRunTx(ctx, tx, a, plan.ID, out.ID); e != nil {
		return nil, e
	}
	if e = event(ctx, tx, out.ID, "execution.created", map[string]any{"issue_id": issue, "origin": "sim", "hardware_evidence": false}); e != nil {
		return nil, e
	}
	if e = audit(ctx, tx, a.User.ID, project, "topology.firmware.mock.execute", out.ID, trace, map[string]any{"plan_id": plan.ID, "scope_hash": plan.ScopeHash, "origin": "sim"}); e != nil {
		return nil, e
	}
	if e = remember(ctx, tx, idem, in, out); e != nil {
		return nil, e
	}
	return out, tx.Commit()
}
func (s *Service) runTopologyFirmwareMockClaim(ctx context.Context, c *Claim) error {
	if s.IssueStore == nil {
		return errors.New("firmware simulation disabled")
	}
	id, _ := c.Parameters["topology_firmware_plan_id"].(string)
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = advisory(ctx, tx, "keel-admin-initialization"); e != nil {
		return e
	}
	started := time.Now().UTC()
	output, e := s.IssueStore.RecordTopologyFirmwareMockTx(ctx, tx, id, c.Attempt.ExecutionID, c.Attempt.ID, c.Attempt.CommandID, c.Attempt.FencingToken)
	if e != nil {
		return e
	}
	receipt := Receipt{CommandID: c.Attempt.CommandID, AttemptID: c.Attempt.ID, FencingToken: c.Attempt.FencingToken, Result: "success", StartedAt: started, FinishedAt: time.Now().UTC(), Output: output}
	if e = s.recordReceiptTx(ctx, tx, receipt); e != nil {
		return e
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	return s.Complete(ctx, c)
}
func (s *Service) topologyFirmwareReceiptAllowedTx(ctx context.Context, tx *sql.Tx, r Receipt) error {
	var plan string
	if e := tx.QueryRowContext(ctx, `SELECT coalesce(execution.parameters->>'topology_firmware_plan_id','') FROM exec_task_attempts attempt JOIN exec_executions execution ON execution.id=attempt.execution_id WHERE attempt.id=$1 AND attempt.command_id=$2`, r.AttemptID, r.CommandID).Scan(&plan); e != nil {
		return e
	}
	if plan == "" {
		return nil
	}
	var allowed bool
	e := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM topology_firmware_mock_receipts proof WHERE proof.command_id=$1 AND proof.attempt_id=$2 AND proof.fencing_token=$3 AND proof.plan_id=$4 AND proof.output=$5::jsonb)`, r.CommandID, r.AttemptID, r.FencingToken, plan, jsonBytes(r.Output)).Scan(&allowed)
	if e != nil {
		return e
	}
	if !allowed {
		return conflict("Firmware simulation receipt must retain its original fenced proof")
	}
	return nil
}
