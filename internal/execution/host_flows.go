package execution

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/enzoOvOyu/keel/internal/platform"
)

type HostFlowCreateInput struct {
	FlowVersionID         string   `json:"flow_version_id"`
	Transport             string   `json:"transport"`
	MachineIDs            []string `json:"machine_ids"`
	Concurrency           int      `json:"concurrency,omitempty"`
	CircuitBreakerPercent int      `json:"circuit_breaker_percent,omitempty"`
}

// Only immutable, published read-only host workflows enter this endpoint.
// Domain-owned write/script/install workflows retain their original admission.
func (s *Service) CreateHostFlow(ctx context.Context, a *Actor, project string, in HostFlowCreateInput, key, trace string) (*Execution, error) {
	if !opaqueID.MatchString(in.FlowVersionID) || (in.Transport != "ssh" && in.Transport != "agent") || len(in.MachineIDs) < 1 || len(in.MachineIDs) > 32 {
		return nil, invalid("host flow revision, explicit ssh/agent transport and1..32 machines are required")
	}
	ids, err := sortedUnique(in.MachineIDs)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		if !opaqueID.MatchString(id) {
			return nil, invalid("invalid machine ID")
		}
	}
	in.MachineIDs = ids
	if in.Concurrency == 0 {
		in.Concurrency = 4
	}
	if in.CircuitBreakerPercent == 0 {
		in.CircuitBreakerPercent = 100
	}
	if in.Concurrency < 1 || in.Concurrency > 32 || in.CircuitBreakerPercent < 1 || in.CircuitBreakerPercent > 100 {
		return nil, invalid("host concurrency must be1..32 and circuit breaker1..100")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = authorize(ctx, tx, a, project, "Admin", "DE", "DL", "SRE"); err != nil {
		return nil, err
	}
	idem, prior, err := idempotency(ctx, tx, a, "host.flow.create:"+project, key, in)
	if err != nil {
		return nil, err
	}
	if prior != nil {
		x, e := decode[Execution](prior)
		return &x, e
	}
	if in.Transport == "agent" && s.AgentRunner == nil && !s.AgentAdmissionOnly {
		return nil, failure(503, "AGENT_DISABLED", "Agent task admission is not configured")
	}
	flow, err := scanFlow(tx.QueryRowContext(ctx, "SELECT "+flowColumns+" FROM exec_flow_versions v JOIN exec_flows f ON f.id=v.flow_id WHERE v.id=$1 AND v.project_id=$2", in.FlowVersionID, project))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, platform.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if flow.ContentHash != hash(jsonBytes(flow.Definition)) {
		return nil, conflict("published host flow digest does not match its definition")
	}
	if flow.Definition.APIVersion != "keel.host/v1" {
		return nil, invalid("only a published keel.host/v1 flow is admitted")
	}
	if err = flow.Definition.Validate(); err != nil {
		return nil, err
	}
	targets := make([]Target, 0, len(ids))
	targetIDs := make([]string, 0, len(ids))
	for _, machine := range ids {
		var target Target
		if in.Transport == "ssh" {
			target, err = s.sshTargetTx(ctx, tx, project, machine)
		} else {
			target, err = s.agentTargetTx(ctx, tx, project, machine)
		}
		if err != nil {
			return nil, err
		}
		err = tx.QueryRowContext(ctx, `INSERT INTO exec_targets(id,project_id,kind,resource_key) VALUES($1,$2,'machine',$3) ON CONFLICT(project_id,kind,resource_key) DO UPDATE SET resource_key=EXCLUDED.resource_key RETURNING id`, platform.RandomID(), project, machine).Scan(&target.ID)
		if err != nil {
			return nil, err
		}
		targets = append(targets, target)
		targetIDs = append(targetIDs, target.ID)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	params := map[string]any{}
	x := &Execution{ID: platform.RandomID(), ProjectID: project, FlowVersionID: flow.ID, Mode: in.Transport, State: "created", StateVersion: 1, DesiredState: "running", Parameters: params, TargetSetHash: hash(jsonBytes(targetIDs)), Concurrency: in.Concurrency, CircuitBreakerPercent: in.CircuitBreakerPercent, CreatedBy: a.User.ID, CreatedAt: now}
	x.Snapshot = Snapshot{Mode: in.Transport, Flow: flow, Targets: targets, Parameters: params, TargetSetHash: x.TargetSetHash, Admission: "Published bounded read-only host DAG; exact current project machine/channel identities; shared device and exclusive transport leases; no automatic replay or hardware acceptance", FrozenAt: now}
	_, err = tx.ExecContext(ctx, `INSERT INTO exec_executions(id,project_id,flow_version_id,mode,parameters,snapshot,target_set_hash,concurrency,circuit_breaker_percent,created_by,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, x.ID, project, flow.ID, x.Mode, jsonBytes(params), jsonBytes(x.Snapshot), x.TargetSetHash, x.Concurrency, x.CircuitBreakerPercent, a.User.ID, now)
	if err != nil {
		return nil, err
	}
	for _, target := range targets {
		if _, err = tx.ExecContext(ctx, `INSERT INTO exec_execution_targets(execution_id,target_id,snapshot) VALUES($1,$2,$3)`, x.ID, target.ID, jsonBytes(target)); err != nil {
			return nil, err
		}
		for index, task := range flow.Definition.Tasks {
			if _, err = tx.ExecContext(ctx, `INSERT INTO exec_task_attempts(id,execution_id,target_id,task_id,task_index,attempt_no,command_id,runner,created_at) VALUES($1,$2,$3,$4,$5,1,$6,$7,$8)`, platform.RandomID(), x.ID, target.ID, task.ID, index, platform.RandomID(), x.Mode, now); err != nil {
				return nil, err
			}
		}
	}
	details := map[string]any{"mode": x.Mode, "flow_version_id": flow.ID, "flow_content_hash": flow.ContentHash, "target_count": len(targets), "task_count": len(flow.Definition.Tasks), "target_hash": x.TargetSetHash}
	if err = event(ctx, tx, x.ID, "execution.created", details); err != nil {
		return nil, err
	}
	if err = audit(ctx, tx, a.User.ID, project, "execution.host.flow.create", x.ID, trace, details); err != nil {
		return nil, err
	}
	if err = remember(ctx, tx, idem, in, x); err != nil {
		return nil, err
	}
	return x, tx.Commit()
}
