package execution

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/enzoOvOyu/keel/internal/netbaseline"
	"github.com/enzoOvOyu/keel/internal/platform"
)

type NetworkExecutionInput struct {
	JobID string `json:"job_id"`
	Phase string `json:"phase"`
}

func (s *Service) CreateNetwork(ctx context.Context, a *Actor, project string, input NetworkExecutionInput, key, trace string) (*Execution, error) {
	if s.NetworkStore == nil {
		return nil, conflict("Network execution is not configured")
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = authorize(ctx, tx, a, project, "Admin", "NDE"); e != nil {
		return nil, e
	}
	idem, prior, e := idempotency(ctx, tx, a, "network.execute:"+project, key, input)
	if e != nil {
		return nil, e
	}
	if prior != nil {
		x, e := decode[Execution](prior)
		return &x, e
	}
	job, e := s.NetworkStore.QueueNetworkPushTx(ctx, tx, a, project, input.JobID, input.Phase)
	if e != nil {
		return nil, e
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	flowID, versionID := platform.RandomID(), platform.RandomID()
	task := Task{ID: "network-" + input.Phase, Type: "network." + input.Phase, Retry: 0, Idempotent: false, TimeoutMS: 180000, Resources: []string{"ssh"}}
	definition := Definition{APIVersion: "keel.network/v1", Inputs: map[string]Input{}, Tasks: []Task{task}}
	flow := FlowVersion{ID: versionID, FlowID: flowID, ProjectID: project, Name: "Network " + input.Phase + " " + job.ID, Version: 1, Definition: definition, ContentHash: hash(jsonBytes(definition)), CreatedAt: now}
	if _, e = tx.ExecContext(ctx, `INSERT INTO exec_flows(id,project_id,name,created_by,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$5)`, flowID, project, flow.Name, a.User.ID, now); e != nil {
		return nil, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO exec_flow_versions(id,flow_id,project_id,version,definition,content_hash,created_by,created_at) VALUES($1,$2,$3,1,$4,$5,$6,$7)`, versionID, flowID, project, jsonBytes(definition), flow.ContentHash, a.User.ID, now); e != nil {
		return nil, e
	}
	targets := []Target{}
	ids := []string{}
	for _, d := range job.Devices {
		var targetID string
		if e = tx.QueryRowContext(ctx, `INSERT INTO exec_targets(id,project_id,kind,resource_key) VALUES($1,$2,'switch',$3) ON CONFLICT(project_id,kind,resource_key) DO UPDATE SET resource_key=EXCLUDED.resource_key RETURNING id`, platform.RandomID(), project, d.Device.ID).Scan(&targetID); e != nil {
			return nil, e
		}
		target := Target{ID: targetID, ProjectID: project, Name: d.Device.Hostname, Revision: 1, ModelVersion: d.Device.ModelVersionID, Mode: "fabric", ResourceKey: d.Device.ID, CreatedAt: now}
		targets = append(targets, target)
		ids = append(ids, targetID)
	}
	params := map[string]any{"network_push_id": job.ID, "network_phase": input.Phase}
	out := &Execution{ID: platform.RandomID(), ProjectID: project, FlowVersionID: versionID, Mode: "fabric", State: "created", StateVersion: 1, DesiredState: "running", Parameters: params, TargetSetHash: hash(jsonBytes(ids)), Concurrency: job.Concurrency, CircuitBreakerPercent: 100, CreatedBy: a.User.ID, CreatedAt: now}
	out.Snapshot = Snapshot{Mode: "fabric", Flow: flow, Targets: targets, Parameters: params, TargetSetHash: out.TargetSetHash, Admission: "Immutable approved baseline, released template, fenced SSH leases; production push additionally requires current independent approval quorum and canary readback", FrozenAt: now}
	if _, e = tx.ExecContext(ctx, `INSERT INTO exec_executions(id,project_id,flow_version_id,mode,parameters,snapshot,target_set_hash,concurrency,circuit_breaker_percent,created_by,created_at) VALUES($1,$2,$3,'fabric',$4,$5,$6,$7,100,$8,$9)`, out.ID, project, versionID, jsonBytes(params), jsonBytes(out.Snapshot), out.TargetSetHash, out.Concurrency, a.User.ID, now); e != nil {
		return nil, e
	}
	for _, t := range targets {
		if _, e = tx.ExecContext(ctx, `INSERT INTO exec_execution_targets(execution_id,target_id,snapshot) VALUES($1,$2,$3)`, out.ID, t.ID, jsonBytes(t)); e != nil {
			return nil, e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO exec_task_attempts(id,execution_id,target_id,task_id,task_index,attempt_no,command_id,runner,created_at) VALUES($1,$2,$3,$4,0,1,$5,'fabric',$6)`, platform.RandomID(), out.ID, t.ID, task.ID, platform.RandomID(), now); e != nil {
			return nil, e
		}
	}
	column := "prepare_execution_id"
	if input.Phase == "push" {
		column = "push_execution_id"
	}
	if _, e = tx.ExecContext(ctx, "UPDATE network_push_jobs SET "+column+"=$2 WHERE id=$1", job.ID, out.ID); e != nil {
		return nil, e
	}
	if e = event(ctx, tx, out.ID, "execution.created", map[string]any{"mode": "fabric", "job_id": job.ID, "phase": input.Phase}); e != nil {
		return nil, e
	}
	if e = audit(ctx, tx, a.User.ID, project, "network.push."+input.Phase, out.ID, trace, map[string]any{"job_id": job.ID, "review_hash": job.ReviewHash}); e != nil {
		return nil, e
	}
	if e = remember(ctx, tx, idem, input, out); e != nil {
		return nil, e
	}
	return out, tx.Commit()
}
func (s *Service) runNetworkClaim(ctx context.Context, c *Claim) error {
	if s.NetworkStore == nil {
		return errors.New("network runner disabled")
	}
	work, cancel := context.WithTimeout(ctx, time.Duration(c.Task.TimeoutMS)*time.Millisecond)
	defer cancel()
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		tick := time.NewTicker(c.LeaseDuration / 3)
		defer tick.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tick.C:
				beat, done := context.WithTimeout(context.Background(), c.LeaseDuration/2)
				kill, e := s.Heartbeat(beat, c)
				done()
				if e != nil || kill {
					cancel()
				}
			}
		}
	}()
	defer func() { close(stop); wg.Wait() }()
	job, _ := c.Parameters["network_push_id"].(string)
	phase, _ := c.Parameters["network_phase"].(string)
	started := time.Now().UTC()
	result, e := s.NetworkStore.RunNetworkTarget(work, c.Target.ProjectID, job, c.Target.ResourceKey, phase, c.Attempt.ExecutionID, c.Attempt.CommandID, c.Attempt.FencingToken)
	var unknown *netbaseline.MutationUnknown
	if errors.As(e, &unknown) {
		return e
	}
	r := Receipt{CommandID: c.Attempt.CommandID, AttemptID: c.Attempt.ID, FencingToken: c.Attempt.FencingToken, Result: "success", StartedAt: started, FinishedAt: time.Now().UTC(), Output: map[string]any{"mode": "fabric", "job_id": job, "phase": phase}}
	if e != nil {
		r.Result = "failed"
		r.Output["code"] = "NETWORK_RUNNER_FAILED"
	} else {
		r.Output["device_state"] = result.State
		r.Output["code"] = result.Code
		r.Output["source"] = result.Source
		r.Output["hardware_evidence"] = result.Source == "device"
		r.Output["running_hash"] = result.RunningHash
		if result.State != "PREPARED" && result.State != "VERIFIED" {
			r.Result = "failed"
		}
	}
	finish, done := context.WithTimeout(context.Background(), 5*time.Second)
	defer done()
	if e = s.RecordReceipt(finish, r); e != nil {
		return e
	}
	return s.Complete(finish, c)
}
