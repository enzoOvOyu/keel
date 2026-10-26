package execution

import (
	"context"
	"database/sql"
	"sync"
	"time"

	"github.com/enzoOvOyu/keel/internal/platform"
)

// CreateProvisionPreparation consumes independently approved consent once. This
// task verifies actual control-plane sources, never boots or reinstalls a host.
func (s *Service) CreateProvisionPreparation(ctx context.Context, a *Actor, project, requestID string, version int64, key, trace string) (*Execution, error) {
	if s.ProvisionStore == nil || !s.ProvisionStore.ProvisionPrepareEnabled {
		return nil, failure(503, "PROVISION_PREPARATION_DISABLED", "Installation preparation worker admission is disabled")
	}
	if !opaqueID.MatchString(requestID) || version < 1 {
		return nil, invalid("Exact request ID and version required")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = authorize(ctx, tx, a, project, "Admin", "DE", "DL", "SRE"); err != nil {
		return nil, err
	}
	input := struct {
		RequestID string
		Version   int64
	}{requestID, version}
	idem, prior, err := idempotency(ctx, tx, a, "provision.prepare:"+project, key, input)
	if err != nil {
		return nil, err
	}
	if prior != nil {
		x, e := decode[Execution](prior)
		return &x, e
	}
	if err = advisory(ctx, tx, "provision.consume:"+requestID); err != nil {
		return nil, err
	}
	request, err := platform.LoadProvisionRequestTx(ctx, tx, project, requestID)
	if err != nil {
		return nil, err
	}
	if request.State != "approved" || request.StateVersion != version || request.ExecutionID != nil {
		return nil, conflict("Unconsumed approved request and exact version required")
	}
	if !platform.ProvisionSnapshotValid(request) {
		return nil, conflict("Frozen preparation snapshot digest differs")
	}
	if err = platform.ValidateProvisionRequestMetadataTx(ctx, tx, request, time.Now()); err != nil {
		return nil, err
	}
	// A fresh write rechecks the state under the row lock after waiting.
	now := time.Now().UTC().Truncate(time.Microsecond)
	x := &Execution{ID: platform.RandomID(), ProjectID: project, Mode: "provision", State: "created", StateVersion: 1, DesiredState: "running", Concurrency: 1, CircuitBreakerPercent: 100, CreatedBy: a.User.ID, CreatedAt: now}
	target := Target{ProjectID: project, ResourceKey: request.MachineID, Mode: "provision", Name: request.Snapshot.Target.Hostname, Revision: request.Snapshot.Target.AssetVersion, ModelVersion: request.Snapshot.Target.ModelVersionID, CreatedAt: now, ProvisionRequestID: request.ID, ProvisionSnapshotSHA256: request.SnapshotSHA256}
	err = tx.QueryRowContext(ctx, `INSERT INTO exec_targets(id,project_id,kind,resource_key)VALUES($1,$2,'machine',$3) ON CONFLICT(project_id,kind,resource_key) DO UPDATE SET resource_key=EXCLUDED.resource_key RETURNING id`, platform.RandomID(), project, request.MachineID).Scan(&target.ID)
	if err != nil {
		return nil, err
	}
	task := Task{ID: "provision-prepare", Type: "provision.prepare", TimeoutMS: 60000, Idempotent: true, Retry: 0, Resources: []string{"device"}}
	definition := Definition{APIVersion: "keel.provision-preparation/v1", Inputs: map[string]Input{}, Tasks: []Task{task}}
	flow := FlowVersion{ID: platform.RandomID(), FlowID: platform.RandomID(), ProjectID: project, Name: "Provision preparation " + request.ID[:8], Version: 1, Definition: definition, ContentHash: hash(jsonBytes(definition)), CreatedAt: now}
	x.FlowVersionID = flow.ID
	x.TargetSetHash = hash(jsonBytes([]string{target.ID}))
	x.Parameters = map[string]any{"provision_request_id": request.ID, "provision_snapshot_sha256": request.SnapshotSHA256}
	x.Snapshot = Snapshot{Mode: "provision", Flow: flow, Targets: []Target{target}, Parameters: x.Parameters, TargetSetHash: x.TargetSetHash, FrozenAt: now, Admission: "Independently approved immutable BOM/media/domain-public-key preparation under an exclusive shared machine lease; actual bytes verified. No PXE, installation, host-state or trust projection."}
	if _, err = tx.ExecContext(ctx, "INSERT INTO exec_flows(id,project_id,name,created_by,created_at,updated_at)VALUES($1,$2,$3,$4,$5,$5)", flow.FlowID, project, flow.Name, a.User.ID, now); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO exec_flow_versions(id,flow_id,project_id,version,definition,content_hash,created_by,created_at)VALUES($1,$2,$3,1,$4,$5,$6,$7)", flow.ID, flow.FlowID, project, jsonBytes(definition), flow.ContentHash, a.User.ID, now); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO exec_executions(id,project_id,flow_version_id,mode,parameters,snapshot,target_set_hash,concurrency,circuit_breaker_percent,created_by,created_at)VALUES($1,$2,$3,'provision',$4,$5,$6,1,100,$7,$8)`, x.ID, project, flow.ID, jsonBytes(x.Parameters), jsonBytes(x.Snapshot), x.TargetSetHash, a.User.ID, now); err != nil {
		return nil, err
	}
	result, err := tx.ExecContext(ctx, "UPDATE provision_requests SET state='preparing',state_version=state_version+1,execution_id=$2,updated_at=$3 WHERE id=$1 AND state='approved' AND state_version=$4 AND execution_id IS NULL", request.ID, x.ID, now, version)
	if err != nil {
		return nil, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if count != 1 {
		return nil, conflict("Request has already been consumed")
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO exec_execution_targets(execution_id,target_id,snapshot)VALUES($1,$2,$3)", x.ID, target.ID, jsonBytes(target)); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO exec_task_attempts(id,execution_id,target_id,task_id,task_index,attempt_no,command_id,runner,created_at)VALUES($1,$2,$3,'provision-prepare',0,1,$4,'provision',$5)`, platform.RandomID(), x.ID, target.ID, platform.RandomID(), now); err != nil {
		return nil, err
	}
	if err = event(ctx, tx, x.ID, "execution.created", map[string]any{"mode": "provision", "request_id": request.ID, "installation_performed": false}); err != nil {
		return nil, err
	}
	if err = audit(ctx, tx, a.User.ID, project, "provision.preparation.created", request.ID, trace, map[string]any{"execution_id": x.ID, "snapshot_sha256": request.SnapshotSHA256}); err != nil {
		return nil, err
	}
	if err = remember(ctx, tx, idem, input, x); err != nil {
		return nil, err
	}
	return x, tx.Commit()
}
func (s *Service) provisionFrozenTx(ctx context.Context, tx *sql.Tx, target Target) error {
	if s.ProvisionStore == nil || !s.ProvisionStore.ProvisionPrepareEnabled {
		return failure(503, "PROVISION_PREPARATION_DISABLED", "Preparation worker disabled")
	}
	request, err := platform.LoadProvisionRequestTx(ctx, tx, target.ProjectID, target.ProvisionRequestID)
	if err != nil {
		return err
	}
	if request.State != "preparing" || request.SnapshotSHA256 != target.ProvisionSnapshotSHA256 || request.MachineID != target.ResourceKey || !platform.ProvisionSnapshotValid(request) {
		return conflict("Frozen preparation binding changed")
	}
	return platform.ValidateProvisionRequestMetadataTx(ctx, tx, request, time.Now())
}
func (s *Service) runProvisionPreparationClaim(ctx context.Context, c *Claim) error {
	if s.ProvisionStore == nil || c.Task.Type != "provision.prepare" {
		return invalid("Provision preparation runner unavailable")
	}
	work, cancel := context.WithTimeout(ctx, time.Duration(c.Task.TimeoutMS)*time.Millisecond)
	defer cancel()
	stop := make(chan struct{})
	var heartbeat sync.WaitGroup
	heartbeat.Add(1)
	go func() {
		defer heartbeat.Done()
		ticker := time.NewTicker(c.LeaseDuration / 3)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				beat, done := context.WithTimeout(context.Background(), c.LeaseDuration/2)
				kill, err := s.Heartbeat(beat, c)
				done()
				if err != nil || kill {
					cancel()
					return
				}
			}
		}
	}()
	defer func() { close(stop); heartbeat.Wait() }()
	started := time.Now().UTC().Truncate(time.Microsecond)
	result, reason := "success", "sources_verified"
	tx, err := s.DB.BeginTx(work, nil)
	if err == nil {
		var creator string
		var enabled, admin bool
		err = tx.QueryRowContext(work, `SELECT x.created_by,u.enabled,u.is_admin FROM exec_executions x JOIN users u ON u.id=x.created_by WHERE x.id=$1 FOR SHARE OF u`, c.Attempt.ExecutionID).Scan(&creator, &enabled, &admin)
		actor := &Actor{User: platform.UserSummary{ID: creator, IsAdmin: admin}}
		if err == nil && !enabled {
			err = platform.ErrForbidden
		}
		if err == nil {
			err = s.provisionFrozenTx(work, tx, c.Target)
		}
		if err == nil {
			var request *platform.ProvisionRequest
			request, err = platform.LoadProvisionRequestTx(work, tx, c.Target.ProjectID, c.Target.ProvisionRequestID)
			if err == nil {
				_, err = s.ProvisionStore.VerifyProvisionRequestTx(work, tx, actor, request)
			}
		}
		tx.Rollback()
	}
	if err != nil {
		quarantine, finish := context.WithTimeout(context.Background(), 5*time.Second)
		err = s.ProvisionStore.RecordMediaIntegrityFailure(quarantine, err, "", "worker")
		finish()
		result = "failed"
		reason = "source_validation_failed"
		if work.Err() != nil {
			result = "cancelled"
			reason = "preparation_stopped"
		}
	}
	finished := time.Now().UTC().Truncate(time.Microsecond)
	receipt := Receipt{CommandID: c.Attempt.CommandID, AttemptID: c.Attempt.ID, FencingToken: c.Attempt.FencingToken, Result: result, StartedAt: started, FinishedAt: finished, Output: map[string]any{"schema": "keel.provision-preparation-proof/v1", "request_id": c.Target.ProvisionRequestID, "snapshot_sha256": c.Target.ProvisionSnapshotSHA256, "reason": reason, "installation_performed": false}}
	persist, done := context.WithTimeout(context.Background(), 5*time.Second)
	defer done()
	if err = s.RecordReceipt(persist, receipt); err != nil {
		return err
	}
	return s.Complete(persist, c)
}
func provisionReceiptTarget(ctx context.Context, tx *sql.Tx, a Attempt, r Receipt) (*Target, error) {
	var mode string
	var snapshot []byte
	err := tx.QueryRowContext(ctx, "SELECT x.mode,t.snapshot FROM exec_executions x JOIN exec_execution_targets t ON t.execution_id=x.id WHERE x.id=$1 AND t.target_id=$2", a.ExecutionID, a.TargetID).Scan(&mode, &snapshot)
	if err != nil {
		return nil, err
	}
	if mode != "provision" {
		return nil, nil
	}
	var target Target
	if err = jsonDecodeProvisionTarget(snapshot, &target); err != nil {
		return nil, err
	}
	if r.Output["schema"] != "keel.provision-preparation-proof/v1" || r.Output["request_id"] != target.ProvisionRequestID || r.Output["snapshot_sha256"] != target.ProvisionSnapshotSHA256 || r.Output["installation_performed"] != false {
		return nil, conflict("Preparation proof does not match its frozen target")
	}
	return &target, nil
}
func jsonDecodeProvisionTarget(raw []byte, target *Target) error {
	x, err := decode[Target](raw)
	if err == nil {
		*target = x
	}
	return err
}

func (s *Service) applyProvisionReceipt(ctx context.Context, tx *sql.Tx, a Attempt, r Receipt) error {
	target, err := provisionReceiptTarget(ctx, tx, a, r)
	if err != nil || target == nil {
		return err
	}
	return platform.ApplyProvisionPreparationReceiptTx(ctx, tx, a.ExecutionID, target.ProvisionRequestID, target.ProvisionSnapshotSHA256, r.Result, r.FinishedAt)
}

// Pending cancellation is evidence that no task ever started. Started/unknown
// commands keep the request pinned until an actual durable receipt is reconciled.
func cancelUnstartedProvisionTx(ctx context.Context, tx *sql.Tx, execution string) error {
	var request string
	err := tx.QueryRowContext(ctx, `SELECT id FROM provision_requests WHERE execution_id=$1 AND state='preparing' AND NOT EXISTS(SELECT 1 FROM exec_task_attempts WHERE execution_id=$1 AND started_at IS NOT NULL) FOR UPDATE`, execution).Scan(&request)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE provision_requests SET state='canceled',state_version=state_version+1,updated_at=now() WHERE id=$1`, request); err != nil {
		return err
	}
	return event(ctx, tx, execution, "provision.preparation.canceled_unstarted", map[string]any{"request_id": request, "installation_performed": false})
}
