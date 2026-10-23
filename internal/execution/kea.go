package execution

import (
	"context"
	"errors"
	"github.com/enzoOvOyu/keel/internal/fabricboot"
	"github.com/enzoOvOyu/keel/internal/platform"
	"sync"
	"time"
)

type KeaCreateInput struct {
	DeploymentID string `json:"deployment_id"`
}

// Domain-owned flow creation is atomic with staging admission, immutable target
// snapshots, attempts, outbox and audit. Generic sim create cannot select it.
func (s *Service) CreateKea(ctx context.Context, a *Actor, projectID string, input KeaCreateInput, key, trace string) (*Execution, error) {
	if s.KeaStore == nil {
		return nil, conflict("Kea deployment service is not configured")
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = authorize(ctx, tx, a, projectID, "Admin", "NDE"); e != nil {
		return nil, e
	}
	idem, prior, e := idempotency(ctx, tx, a, "kea.execute:"+projectID, key, input)
	if e != nil {
		return nil, e
	}
	if prior != nil {
		out, e := decode[Execution](prior)
		return &out, e
	}
	job, e := s.KeaStore.QueueKeaDeploymentTx(ctx, tx, a, projectID, input.DeploymentID)
	if e != nil {
		return nil, e
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	flowID, versionID := platform.RandomID(), platform.RandomID()
	definition := Definition{APIVersion: "keel.kea/v1", Inputs: map[string]Input{}, Tasks: []Task{{ID: "kea-config", Type: "kea.apply", Retry: 0, Idempotent: false, TimeoutMS: 120000, Resources: []string{"kea"}}}}
	flow := FlowVersion{ID: versionID, FlowID: flowID, ProjectID: projectID, Name: "Kea " + job.ID, Version: 1, Definition: definition, ContentHash: hash(jsonBytes(definition)), CreatedAt: now}
	_, e = tx.ExecContext(ctx, `INSERT INTO exec_flows(id,project_id,name,created_by,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$5)`, flowID, projectID, flow.Name, a.User.ID, now)
	if e != nil {
		return nil, e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO exec_flow_versions(id,flow_id,project_id,version,definition,content_hash,created_by,created_at) VALUES($1,$2,$3,1,$4,$5,$6,$7)`, versionID, flowID, projectID, jsonBytes(definition), flow.ContentHash, a.User.ID, now)
	if e != nil {
		return nil, e
	}
	var targetID string
	e = tx.QueryRowContext(ctx, `INSERT INTO exec_targets(id,project_id,kind,resource_key) VALUES($1,$2,'kea',$3) ON CONFLICT(project_id,kind,resource_key) DO UPDATE SET resource_key=EXCLUDED.resource_key RETURNING id`, platform.RandomID(), projectID, job.Spec.ControllerKey).Scan(&targetID)
	if e != nil {
		return nil, e
	}
	target := Target{ID: targetID, ProjectID: projectID, Name: "Kea DHCPv4 control", Revision: 1, ModelVersion: "kea-control/v1", Mode: "kea", ResourceKey: job.Spec.ControllerKey, CreatedAt: now}
	params := map[string]any{"kea_deployment_id": job.ID}
	targetHash := hash(jsonBytes([]string{target.ID}))
	out := &Execution{ID: platform.RandomID(), ProjectID: projectID, FlowVersionID: versionID, Mode: "kea", State: "created", StateVersion: 1, DesiredState: "running", Parameters: params, TargetSetHash: targetHash, Concurrency: 1, CircuitBreakerPercent: 1, CreatedBy: a.User.ID, CreatedAt: now}
	out.Snapshot = Snapshot{Mode: "kea", Flow: flow, Targets: []Target{target}, Parameters: params, TargetSetHash: targetHash, Admission: "Approved management plan and unexpired N-S3; control configuration only, not DORA/device/NP evidence", FrozenAt: now}
	_, e = tx.ExecContext(ctx, `INSERT INTO exec_executions(id,project_id,flow_version_id,mode,parameters,snapshot,target_set_hash,concurrency,circuit_breaker_percent,created_by,created_at) VALUES($1,$2,$3,'kea',$4,$5,$6,1,1,$7,$8)`, out.ID, projectID, versionID, jsonBytes(params), jsonBytes(out.Snapshot), targetHash, a.User.ID, now)
	if e != nil {
		return nil, e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO exec_execution_targets(execution_id,target_id,snapshot) VALUES($1,$2,$3)`, out.ID, target.ID, jsonBytes(target))
	if e != nil {
		return nil, e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO exec_task_attempts(id,execution_id,target_id,task_id,task_index,attempt_no,command_id,runner,created_at) VALUES($1,$2,$3,'kea-config',0,1,$4,'kea',$5)`, platform.RandomID(), out.ID, target.ID, platform.RandomID(), now)
	if e != nil {
		return nil, e
	}
	_, e = tx.ExecContext(ctx, `UPDATE ztp_kea_deployments SET state='queued',execution_id=$2,updated_at=now() WHERE id=$1`, job.ID, out.ID)
	if e != nil {
		return nil, e
	}
	if e = event(ctx, tx, out.ID, "execution.created", map[string]any{"mode": "kea", "deployment_id": job.ID, "target_count": 1}); e != nil {
		return nil, e
	}
	if e = audit(ctx, tx, a.User.ID, projectID, "ztp.kea.execute", out.ID, trace, map[string]any{"deployment_id": job.ID, "mode": "kea", "candidate_hash": job.CandidateHash}); e != nil {
		return nil, e
	}
	if e = remember(ctx, tx, idem, input, out); e != nil {
		return nil, e
	}
	return out, tx.Commit()
}
func (s *Service) runKeaClaim(ctx context.Context, c *Claim) error {
	if s.KeaStore == nil {
		return errors.New("Kea runner is disabled")
	}
	work, cancel := context.WithTimeout(ctx, time.Duration(c.Task.TimeoutMS)*time.Millisecond)
	defer cancel()
	stop := make(chan struct{})
	var heartbeats sync.WaitGroup
	heartbeats.Add(1)
	go func() {
		defer heartbeats.Done()
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
	defer func() { close(stop); heartbeats.Wait() }()
	started := time.Now().UTC()
	jobID, _ := c.Parameters["kea_deployment_id"].(string)
	result, e := s.KeaStore.RunKeaDeployment(work, jobID, c.Attempt.ExecutionID)
	var unknown *fabricboot.KeaMutationUnknown
	if errors.As(e, &unknown) {
		return e
	} // No false stopped receipt: lease reaper quarantines.
	receipt := Receipt{CommandID: c.Attempt.CommandID, AttemptID: c.Attempt.ID, FencingToken: c.Attempt.FencingToken, Result: "success", StartedAt: started, FinishedAt: time.Now().UTC(), Output: map[string]any{"mode": "kea", "deployment_id": jobID, "hardware_evidence": false, "dora_verified": false, "device_managed": false}}
	if e != nil {
		receipt.Result = "failed"
		receipt.Output["reason"] = "kea_prewrite_rejected"
	} else {
		receipt.Output["before_hash"] = result.BeforeHash
		receipt.Output["active_hash"] = result.ActiveHash
		receipt.Output["candidate_sha256"] = result.CandidateSHA256
		receipt.Output["config_persisted"] = true
		receipt.Output["dora_verified"] = result.DORA != nil
		receipt.Output["boot_route_ready"] = result.DORA != nil
		receipt.Output["verification_code"] = result.VerificationCode
		if result.DORA != nil {
			receipt.Output["dora_proof"] = result.DORA
		}
	}
	finish, done := context.WithTimeout(context.Background(), 5*time.Second)
	defer done()
	if e = s.RecordReceipt(finish, receipt); e != nil {
		return e
	}
	return s.Complete(finish, c)
}
