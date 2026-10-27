package execution

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"time"

	"github.com/enzoOvOyu/keel/internal/bmc"
	"github.com/enzoOvOyu/keel/internal/platform"
)

// BMC requests share the workflow scheduler, global machine resource, fenced
// attempts, outbox and durable receipts. Browser input never selects endpoints,
// credentials, arbitrary tasks or the evidence origin.
func (s *Service) CreateBMCCollection(ctx context.Context, a *Actor, project, asset string, in platform.BMCCollectionInput, key, trace string) (*Execution, error) {
	if s.BMCStore == nil || !s.BMCStore.BMCInventoryEnabled {
		return nil, failure(409, "BMC_INVENTORY_DISABLED", "BMC inventory admission is disabled")
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = advisory(ctx, tx, "keel-admin-initialization"); e != nil {
		return nil, e
	}
	if e = authorize(ctx, tx, a, project, "Admin", "DE", "DL", "SRE"); e != nil {
		return nil, e
	}
	idem, prior, e := idempotency(ctx, tx, a, "bmc.collection:"+project+":"+asset, key, in)
	if e != nil {
		return nil, e
	}
	if prior != nil {
		out, e := decode[Execution](prior)
		return &out, e
	}
	out, e := s.createBMCCollectionTx(ctx, tx, a, project, asset, in, trace, nil, "")
	if e != nil {
		return nil, e
	}
	if e = remember(ctx, tx, idem, in, out); e != nil {
		return nil, e
	}
	return out, tx.Commit()
}

// Only authenticated HTTP admission or a current authorized policy owner reaches
// this core. It neither commits nor replaces the shared Execution scheduler.
func (s *Service) createBMCCollectionTx(ctx context.Context, tx *sql.Tx, a *Actor, project, asset string, in platform.BMCCollectionInput, trace string, trigger *BMCExecutionTrigger, verification string) (*Execution, error) {
	admission, e := s.BMCStore.PrepareBMCCollectionTx(ctx, tx, project, asset, platform.RandomID(), in)
	if e != nil {
		return nil, e
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	task := Task{ID: "bmc-collect", Type: "bmc.inventory", Retry: 0, Idempotent: true, TimeoutMS: 900000, Resources: []string{"bmc", "device"}}
	definition := Definition{APIVersion: "keel.bmc-inventory/v1", Inputs: map[string]Input{}, Tasks: []Task{task}}
	flow := FlowVersion{ID: platform.RandomID(), FlowID: platform.RandomID(), ProjectID: project, Name: "BMC " + in.Level + " " + admission.Target.RequestID, Version: 1, Definition: definition, ContentHash: hash(jsonBytes(definition)), CreatedAt: now}
	if _, e = tx.ExecContext(ctx, `INSERT INTO exec_flows(id,project_id,name,created_by,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$5)`, flow.FlowID, project, flow.Name, a.User.ID, now); e != nil {
		return nil, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO exec_flow_versions(id,flow_id,project_id,version,definition,content_hash,created_by,created_at) VALUES($1,$2,$3,1,$4,$5,$6,$7)`, flow.ID, flow.FlowID, project, jsonBytes(definition), flow.ContentHash, a.User.ID, now); e != nil {
		return nil, e
	}
	target := Target{ProjectID: project, ResourceKey: admission.Target.MachineID, Name: admission.Hostname, Revision: admission.Target.AssetVersion, ModelVersion: admission.ModelVersion, Mode: "bmc", CreatedAt: now}
	if e = tx.QueryRowContext(ctx, `INSERT INTO exec_targets(id,project_id,kind,resource_key) VALUES($1,$2,'machine',$3) ON CONFLICT(project_id,kind,resource_key) DO UPDATE SET resource_key=excluded.resource_key RETURNING id`, platform.RandomID(), project, target.ResourceKey).Scan(&target.ID); e != nil {
		return nil, e
	}
	params := map[string]any{"bmc_collection_id": admission.Target.RequestID, "bmc_target": admission.Target, "bmc_level": in.Level, "evidence_origin": admission.Origin}
	if verification != "" {
		params["bmc_credential_verification_id"] = verification
	}
	if trigger != nil {
		params["bmc_trigger"] = trigger
	}
	targetHash := hash(jsonBytes(admission))
	out := &Execution{ID: platform.RandomID(), ProjectID: project, FlowVersionID: flow.ID, Mode: "bmc", State: "created", StateVersion: 1, DesiredState: "running", Parameters: params, TargetSetHash: targetHash, Concurrency: 1, CircuitBreakerPercent: 100, CreatedBy: a.User.ID, CreatedAt: now}
	out.Snapshot = Snapshot{Mode: "bmc", Flow: flow, Targets: []Target{target}, Parameters: params, TargetSetHash: targetHash, FrozenAt: now, Admission: "Read-only BMC inventory; current project BOM/asset identity, immutable credential version and adapter binding; shared machine and exclusive BMC channel leases; raw-backed sim/real evidence does not assert G1/G2/G3"}
	if _, e = tx.ExecContext(ctx, `INSERT INTO exec_executions(id,project_id,flow_version_id,mode,parameters,snapshot,target_set_hash,concurrency,circuit_breaker_percent,created_by,created_at) VALUES($1,$2,$3,'bmc',$4,$5,$6,1,100,$7,$8)`, out.ID, project, flow.ID, jsonBytes(params), jsonBytes(out.Snapshot), targetHash, a.User.ID, now); e != nil {
		return nil, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO exec_execution_targets(execution_id,target_id,snapshot) VALUES($1,$2,$3)`, out.ID, target.ID, jsonBytes(target)); e != nil {
		return nil, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO exec_task_attempts(id,execution_id,target_id,task_id,task_index,attempt_no,command_id,runner,created_at) VALUES($1,$2,$3,'bmc-collect',0,1,$4,'bmc',$5)`, platform.RandomID(), out.ID, target.ID, platform.RandomID(), now); e != nil {
		return nil, e
	}
	if e = s.BMCStore.AttachBMCCollectionTx(ctx, tx, a, out.ID, admission, in.Reason, trace); e != nil {
		return nil, e
	}
	if e = event(ctx, tx, out.ID, "execution.created", map[string]any{"mode": "bmc", "collection_id": admission.Target.RequestID, "origin": admission.Origin, "level": in.Level}); e != nil {
		return nil, e
	}
	return out, nil
}

func (s *Service) runBMCCollectionClaim(ctx context.Context, c *Claim) error {
	if s.BMCStore == nil || c.Task.Type != "bmc.inventory" {
		return errors.New("BMC inventory worker is disabled")
	}
	// Reentry consumes only the original durable proof. It cannot recollect.
	var receiptPayload []byte
	err := s.DB.QueryRowContext(ctx, `SELECT payload FROM exec_runner_receipts WHERE command_id=$1 AND attempt_id=$2 AND fencing_token=$3`, c.Attempt.CommandID, c.Attempt.ID, c.Attempt.FencingToken).Scan(&receiptPayload)
	if err == nil {
		return s.Complete(ctx, c)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if recovered, e := s.recoverBMCReceipt(ctx, c); e != nil {
		return e
	} else if recovered {
		return s.Complete(ctx, c)
	}
	work, cancel := context.WithTimeout(ctx, time.Duration(c.Task.TimeoutMS)*time.Millisecond)
	defer cancel()
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(c.LeaseDuration / 3)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
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
	id, _ := c.Parameters["bmc_collection_id"].(string)
	started := time.Now().UTC()
	report, runErr := s.BMCStore.RunBMCCollection(work, id, c.Attempt.ExecutionID, c.Attempt.ID, c.Attempt.CommandID, c.Attempt.FencingToken)
	var dispatched *platform.Error
	if errors.As(runErr, &dispatched) && dispatched.Code == "BMC_COLLECTION_ALREADY_DISPATCHED" {
		return runErr
	}
	// Once the collector has returned no hardware request may be dispatched by
	// this command again. A failed capture is a stopped failure, never a healthy
	// record or an automatically replayed unknown attempt.
	finish, done := context.WithTimeout(context.Background(), 20*time.Second)
	defer done()
	receipt := Receipt{CommandID: c.Attempt.CommandID, AttemptID: c.Attempt.ID, FencingToken: c.Attempt.FencingToken, Result: "failed", StartedAt: started, FinishedAt: time.Now().UTC(), Output: map[string]any{"mode": "bmc", "collection_id": id, "code": "BMC_COLLECTION_FAILED", "stopped": true, "hardware_evidence": false}}
	if runErr == nil && report != nil {
		runErr = s.BMCStore.RecordBMCCollection(finish, c.Attempt.ExecutionID, c.Attempt.FencingToken, report)
		if runErr == nil {
			receipt.Output["evidence_hash"] = bmc.CanonicalDigest(jsonBytes(report))
			receipt.Output["state"] = report.State
			receipt.Output["origin"] = report.Origin
			receipt.Output["identity_matched"] = report.IdentityMatched
			receipt.Output["resource_count"] = len(report.Resources)
			receipt.Output["failure_count"] = len(report.Failures)
			receipt.Output["code"] = "BMC_COLLECTION_RECORDED"
			if report.State == "completed" {
				receipt.Result = "success"
			}
			receipt.Output["hardware_evidence"] = report.Origin == "real" && report.IdentityMatched
		}
	}
	if runErr != nil {
		var business *platform.Error
		if errors.As(runErr, &business) {
			receipt.Output["code"] = business.Code
		}
	}
	if errors.Is(work.Err(), context.DeadlineExceeded) {
		receipt.Result = "failed"
		receipt.Output["code"] = "BMC_COLLECTION_TIMEOUT"
	} else if work.Err() != nil {
		receipt.Result = "cancelled"
		receipt.Output["code"] = "BMC_COLLECTION_STOPPED"
	}
	receipt.FinishedAt = time.Now().UTC()
	if e := s.RecordReceipt(finish, receipt); e != nil {
		return e
	}
	return s.Complete(finish, c)
}

// A success receipt must point to immutable raw-backed evidence from this exact
// attempt. Failed stopped captures may have no collection; they cannot assert
// hardware evidence. This check also runs before explicit unknown reconciliation.
func bmcReceiptAllowedTx(ctx context.Context, tx *sql.Tx, r Receipt) error {
	var mode, id string
	e := tx.QueryRowContext(ctx, `SELECT x.mode,coalesce(x.parameters->>'bmc_collection_id','') FROM exec_executions x JOIN exec_task_attempts a ON a.execution_id=x.id WHERE a.id=$1`, r.AttemptID).Scan(&mode, &id)
	if e != nil || mode != "bmc" {
		return e
	}
	request, _ := r.Output["collection_id"].(string)
	stopped, _ := r.Output["stopped"].(bool)
	if id == "" || request != id || !stopped {
		return conflict("BMC receipt lacks frozen request or stopped proof")
	}
	digest, _ := r.Output["evidence_hash"].(string)
	hardware, _ := r.Output["hardware_evidence"].(bool)
	if digest == "" {
		if r.Result == "success" || hardware {
			return conflict("BMC success requires immutable evidence")
		}
		return nil
	}
	var current bool
	e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM bmc_collection_records q JOIN exec_task_attempts a ON a.execution_id=q.execution_id WHERE a.id=$1 AND q.id=$2 AND q.content_hash=$3 AND q.fencing_token=$4 AND ( $5<>'success' OR q.state='completed') AND (NOT $6 OR (q.origin='real' AND (q.report->>'identity_matched')::boolean)))`, r.AttemptID, id, digest, r.FencingToken, r.Result, hardware).Scan(&current)
	if e != nil {
		return e
	}
	if !current {
		return conflict("BMC receipt evidence differs from its fenced collection")
	}
	return nil
}

// A committed report proves that the original bounded collector returned and
// stopped. Recover its receipt after a worker crash without issuing any GET.
func (s *Service) recoverBMCReceipt(ctx context.Context, c *Claim) (bool, error) {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return false, e
	}
	defer tx.Rollback()
	recovered, e := s.recoverBMCReceiptTx(ctx, tx, c)
	if e != nil {
		return false, e
	}
	return recovered, tx.Commit()
}
func (s *Service) recoverBMCReceiptTx(ctx context.Context, tx *sql.Tx, c *Claim) (bool, error) {
	var exists bool
	if e := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM exec_runner_receipts WHERE command_id=$1 AND attempt_id=$2 AND fencing_token=$3)`, c.Attempt.CommandID, c.Attempt.ID, c.Attempt.FencingToken).Scan(&exists); e != nil {
		return false, e
	}
	if exists {
		return true, nil
	}
	request, _ := c.Parameters["bmc_collection_id"].(string)
	var state, origin, digest string
	var started, finished time.Time
	var resources, failures int
	var identity bool
	e := tx.QueryRowContext(ctx, `SELECT r.state,r.origin,r.content_hash,d.dispatched_at,r.collected_at,jsonb_array_length(r.report->'resources'),jsonb_array_length(r.report->'failures'),(r.report->>'identity_matched')::boolean FROM bmc_collection_records r JOIN bmc_collection_dispatches d ON d.request_id=r.id AND d.execution_id=r.execution_id AND d.fencing_token=r.fencing_token WHERE r.id=$1 AND r.execution_id=$2 AND r.fencing_token=$3 AND d.attempt_id=$4 AND d.command_id=$5`, request, c.Attempt.ExecutionID, c.Attempt.FencingToken, c.Attempt.ID, c.Attempt.CommandID).Scan(&state, &origin, &digest, &started, &finished, &resources, &failures, &identity)
	if errors.Is(e, sql.ErrNoRows) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	result := "failed"
	if state == "completed" {
		result = "success"
	}
	receipt := Receipt{CommandID: c.Attempt.CommandID, AttemptID: c.Attempt.ID, FencingToken: c.Attempt.FencingToken, Result: result, StartedAt: started, FinishedAt: finished, Output: map[string]any{"mode": "bmc", "collection_id": request, "evidence_hash": digest, "state": state, "origin": origin, "identity_matched": identity, "resource_count": resources, "failure_count": failures, "code": "BMC_COLLECTION_RECEIPT_RECOVERED", "stopped": true, "hardware_evidence": origin == "real" && identity}}
	return true, s.recordReceiptTx(ctx, tx, receipt)
}

func (s *Service) recoverExpiredBMCReceiptTx(ctx context.Context, tx *sql.Tx, execution, attemptID string) error {
	attempt, e := scanAttempt(tx.QueryRowContext(ctx, "SELECT "+attemptColumns+" FROM exec_task_attempts WHERE id=$1 AND execution_id=$2 AND state='unknown'", attemptID, execution))
	if e != nil {
		return e
	}
	var request string
	if e = tx.QueryRowContext(ctx, `SELECT parameters->>'bmc_collection_id' FROM exec_executions WHERE id=$1`, execution).Scan(&request); e != nil {
		return e
	}
	_, e = s.recoverBMCReceiptTx(ctx, tx, &Claim{Attempt: attempt, Parameters: map[string]any{"bmc_collection_id": request}})
	return e
}

func (s *Service) bmcHeartbeatTx(ctx context.Context, tx *sql.Tx, c *Claim) bool {
	if c.Target.Mode != "bmc" {
		return false
	}
	if s.BMCStore == nil || !s.BMCStore.BMCInventoryEnabled {
		return true
	}
	var valid bool
	e := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM exec_executions x JOIN bmc_collection_requests q ON q.execution_id=x.id JOIN users u ON u.id=x.created_by JOIN projects p ON p.id=x.project_id JOIN project_assets a ON a.id=q.asset_id JOIN machines machine ON machine.id=a.machine_id WHERE x.id=$1 AND q.id=$2 AND a.project_id=q.project_id AND a.machine_id=q.machine_id AND a.version=q.asset_version AND machine.serial_number=q.target->>'serial' AND u.enabled AND p.state NOT IN('archived','delivered') AND (u.is_admin OR EXISTS(SELECT 1 FROM memberships m WHERE m.user_id=u.id AND m.project_id=x.project_id AND m.role IN('Admin','DE','DL','SRE'))) AND NOT EXISTS(SELECT 1 FROM device_credential_revocations r WHERE r.project_id=q.project_id AND r.credential_batch_ref=q.credential_batch_ref AND r.scope='bmc' AND r.credential_version=q.credential_version))`, c.Attempt.ExecutionID, c.Parameters["bmc_collection_id"]).Scan(&valid)
	return e != nil || !valid
}
