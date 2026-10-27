package execution

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"time"

	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/enzoOvOyu/keel/internal/wiring"
)

// WiringAdapters are server-owned fixed adapters. A browser cannot select an
// endpoint, command, fixture, source label or privileged driver.
type WiringAdapters interface {
	HostOrigin() string
	SwitchOrigin() string
	Open(*platform.WiringCommand) (wiring.Driver, wiring.RecollectionClient, error)
}

func (s *Service) CreateWiringExecution(ctx context.Context, a *Actor, project string, in platform.WiringRunInput, key, trace string) (*Execution, error) {
	if s.WiringStore == nil || !s.WiringStore.WiringEnabled {
		return nil, failure(409, "WIRING_DISABLED", "Wiring execution is disabled")
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = advisory(ctx, tx, "keel-admin-initialization"); e != nil {
		return nil, e
	}
	if e = authorize(ctx, tx, a, project, "Admin", "DE", "DL", "NE", "SRE"); e != nil {
		return nil, e
	}
	idem, prior, e := idempotency(ctx, tx, a, "wiring.execute:"+project, key, in)
	if e != nil {
		return nil, e
	}
	if prior != nil {
		out, e := decode[Execution](prior)
		return &out, e
	}
	scopes, e := s.WiringStore.PrepareWiringRunTx(ctx, tx, a, project, in)
	if e != nil {
		return nil, e
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	flowID, versionID := platform.RandomID(), platform.RandomID()
	task := Task{ID: "wiring-capture", Type: "wiring.capture", Retry: 0, Idempotent: false, TimeoutMS: 45000, Resources: []string{"device", "ssh"}}
	def := Definition{APIVersion: "keel.wiring/v1", Inputs: map[string]Input{}, Tasks: []Task{task}}
	flow := FlowVersion{ID: versionID, FlowID: flowID, ProjectID: project, Name: "Wiring " + in.BatchID + " " + versionID, Version: 1, Definition: def, ContentHash: hash(jsonBytes(def)), CreatedAt: now}
	if _, e = tx.ExecContext(ctx, `INSERT INTO exec_flows(id,project_id,name,created_by,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$5)`, flowID, project, flow.Name, a.User.ID, now); e != nil {
		return nil, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO exec_flow_versions(id,flow_id,project_id,version,definition,content_hash,created_by,created_at) VALUES($1,$2,$3,1,$4,$5,$6,$7)`, versionID, flowID, project, jsonBytes(def), flow.ContentHash, a.User.ID, now); e != nil {
		return nil, e
	}
	targets := make([]Target, 0, len(scopes))
	for _, scope := range scopes {
		var targetID string
		if e = tx.QueryRowContext(ctx, `INSERT INTO exec_targets(id,project_id,kind,resource_key) VALUES($1,$2,'machine',$3) ON CONFLICT(project_id,kind,resource_key) DO UPDATE SET resource_key=EXCLUDED.resource_key RETURNING id`, platform.RandomID(), project, scope.MachineID).Scan(&targetID); e != nil {
			return nil, e
		}
		targets = append(targets, Target{ID: targetID, ProjectID: project, Name: scope.Expectation.Hostname, Revision: 1, ModelVersion: "wiring/v1", Mode: "fabric", ResourceKey: scope.MachineID, CreatedAt: now})
	}
	params := map[string]any{"validation_kind": "wiring", "wiring_batch_id": in.BatchID}
	targetHash := hash(jsonBytes(scopes))
	out := &Execution{ID: platform.RandomID(), ProjectID: project, FlowVersionID: versionID, Mode: "fabric", State: "created", StateVersion: 1, DesiredState: "running", Parameters: params, TargetSetHash: targetHash, Concurrency: 8, CircuitBreakerPercent: 100, CreatedBy: a.User.ID, CreatedAt: now}
	out.Snapshot = Snapshot{Mode: "fabric", Flow: flow, Targets: targets, Parameters: params, TargetSetHash: targetHash, Admission: "Frozen Step 1; per-host readiness refresh; exclusive host and switch leases; original command only; no automatic retry or G2", FrozenAt: now}
	if _, e = tx.ExecContext(ctx, `INSERT INTO exec_executions(id,project_id,flow_version_id,mode,parameters,snapshot,target_set_hash,concurrency,circuit_breaker_percent,created_by,created_at) VALUES($1,$2,$3,'fabric',$4,$5,$6,8,100,$7,$8)`, out.ID, project, versionID, jsonBytes(params), jsonBytes(out.Snapshot), targetHash, a.User.ID, now); e != nil {
		return nil, e
	}
	for _, target := range targets {
		if _, e = tx.ExecContext(ctx, `INSERT INTO exec_execution_targets(execution_id,target_id,snapshot) VALUES($1,$2,$3)`, out.ID, target.ID, jsonBytes(target)); e != nil {
			return nil, e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO exec_task_attempts(id,execution_id,target_id,task_id,task_index,attempt_no,command_id,runner,created_at) VALUES($1,$2,$3,'wiring-capture',0,1,$4,'fabric',$5)`, platform.RandomID(), out.ID, target.ID, platform.RandomID(), now); e != nil {
			return nil, e
		}
	}
	if e = s.WiringStore.AttachWiringRunTx(ctx, tx, a, out.ID, in, scopes); e != nil {
		return nil, e
	}
	if e = event(ctx, tx, out.ID, "execution.created", map[string]any{"mode": "fabric", "validation_kind": "wiring", "batch_id": in.BatchID}); e != nil {
		return nil, e
	}
	if e = audit(ctx, tx, a.User.ID, project, "wiring.execute", out.ID, trace, map[string]any{"batch_id": in.BatchID, "targets": len(targets), "reason": in.Reason}); e != nil {
		return nil, e
	}
	if e = remember(ctx, tx, idem, in, out); e != nil {
		return nil, e
	}
	return out, tx.Commit()
}
func (s *Service) runWiringClaim(ctx context.Context, c *Claim) error {
	if s.WiringStore == nil || s.WiringAdapters == nil {
		return errors.New("wiring adapters disabled")
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
	binding := wiring.ReadbackBinding{ProjectID: c.Target.ProjectID, ExecutionID: c.Attempt.ExecutionID, AttemptID: c.Attempt.ID, CommandID: c.Attempt.CommandID, FencingToken: c.Attempt.FencingToken, Origin: s.WiringAdapters.SwitchOrigin()}
	command, e := s.WiringStore.RegisterWiringRunCommand(work, binding, c.Target.ResourceKey, s.WiringAdapters.HostOrigin())
	if e != nil {
		return s.finishWiringNonstart(ctx, c, binding, "WIRING_DISPATCH_REJECTED", e)
	}
	driver, client, e := s.WiringAdapters.Open(command)
	if e != nil {
		return s.finishWiringNonstart(ctx, c, binding, "WIRING_ADAPTER_REJECTED", e)
	}
	started := time.Now().UTC()
	record, e := s.WiringStore.RunWiringCommand(work, binding, driver, client)
	if e != nil || record == nil {
		if errors.Is(e, wiring.ErrCaptureRejected) {
			return s.finishWiringNonstart(ctx, c, binding, "WIRING_LOCAL_PRESTART_REJECTED", e)
		}
		return e
	} // Unknown work keeps original resources.
	// Reverse-read pinned originals before the common receipt can settle leases.
	record, _, e = s.WiringStore.ReadWiringCapture(work, binding)
	if e != nil {
		return e
	}
	result := "failed"
	if record.Evaluation.Step2 == "PASS" && record.Evaluation.Step3 == "PASS" {
		result = "success"
	}
	receipt := Receipt{CommandID: binding.CommandID, AttemptID: binding.AttemptID, FencingToken: binding.FencingToken, Result: result, StartedAt: started, FinishedAt: time.Now().UTC(), Output: map[string]any{"wiring_raw_sha256": record.RawSHA256, "step2": record.Evaluation.Step2, "step3": record.Evaluation.Step3, "host_origin": command.HostOrigin, "switch_origin": binding.Origin}}
	finish, done := context.WithTimeout(context.Background(), 5*time.Second)
	defer done()
	if e = s.RecordReceipt(finish, receipt); e != nil {
		return e
	}
	return s.Complete(finish, c)
}

// A generic fabric receipt must never settle a wiring attempt without the
// exact durable host stop proof and original capture evaluation.
func (s *Service) wiringReceiptAllowedTx(ctx context.Context, tx *sql.Tx, r Receipt) error {
	var kind string
	e := tx.QueryRowContext(ctx, `SELECT coalesce(x.parameters->>'validation_kind','') FROM exec_task_attempts a JOIN exec_executions x ON x.id=a.execution_id WHERE a.id=$1`, r.AttemptID).Scan(&kind)
	if e != nil || kind != "wiring" {
		return e
	}
	if s.WiringStore == nil {
		return failure(409, "WIRING_DISABLED", "Wiring evidence consumer disabled")
	}
	if code, ok := r.Output["wiring_nonstart_code"].(string); ok {
		var original string
		if e = tx.QueryRowContext(ctx, `SELECT code FROM validation_wiring_nonstarts WHERE command_id=$1 AND attempt_id=$2 AND fencing_token=$3`, r.CommandID, r.AttemptID, r.FencingToken).Scan(&original); e != nil {
			return e
		}
		if code != original || r.Result != "failed" {
			return conflict("Nonstart receipt differs from immutable local tombstone")
		}
		return nil
	}
	var sha, step2, step3, hostOrigin, switchOrigin string
	e = tx.QueryRowContext(ctx, `SELECT p.raw_sha256,p.evaluation->>'step2',p.evaluation->>'step3',c.command->>'host_origin',c.origin FROM validation_wiring_capture_records p JOIN validation_wiring_commands c ON c.command_id=p.command_id JOIN validation_wiring_host_proofs h ON h.command_id=c.command_id WHERE c.command_id=$1 AND c.attempt_id=$2 AND c.fencing_token=$3 AND h.outcome='stopped'`, r.CommandID, r.AttemptID, r.FencingToken).Scan(&sha, &step2, &step3, &hostOrigin, &switchOrigin)
	if e != nil {
		return e
	}
	want := "failed"
	if step2 == "PASS" && step3 == "PASS" {
		want = "success"
	}
	if r.Result != want || r.Output["wiring_raw_sha256"] != sha || r.Output["step2"] != step2 || r.Output["step3"] != step3 || r.Output["host_origin"] != hostOrigin || r.Output["switch_origin"] != switchOrigin {
		return conflict("Wiring receipt differs from the original stopped capture")
	}
	return nil
}

func (s *Service) applyWiringReceipt(ctx context.Context, tx *sql.Tx, a Attempt, r Receipt) error {
	var kind string
	if e := tx.QueryRowContext(ctx, `SELECT coalesce(parameters->>'validation_kind','') FROM exec_executions WHERE id=$1`, a.ExecutionID).Scan(&kind); e != nil {
		return e
	}
	if kind != "wiring" {
		return nil
	}
	if e := s.wiringReceiptAllowedTx(ctx, tx, r); e != nil {
		return e
	}
	if _, ok := r.Output["wiring_nonstart_code"].(string); ok {
		return nil
	}
	return s.WiringStore.ApplyWiringCaptureTx(ctx, tx, a.ID, a.CommandID, a.FencingToken)
}

func (s *Service) finishWiringNonstart(ctx context.Context, c *Claim, b wiring.ReadbackBinding, code string, cause error) error {
	// This helper is invoked only on the local prestart path. The durable start
	// barrier is authoritative if a concurrent dispatch or uncertain Begin exists.
	finish, done := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer done()
	if e := s.WiringStore.RecordWiringNonstart(finish, b, code); e != nil {
		return cause
	}
	now := time.Now().UTC()
	r := Receipt{CommandID: b.CommandID, AttemptID: b.AttemptID, FencingToken: b.FencingToken, Result: "failed", StartedAt: now, FinishedAt: now, Output: map[string]any{"wiring_nonstart_code": code}}
	if e := s.RecordReceipt(finish, r); e != nil {
		return e
	}
	return s.Complete(finish, c)
}

// RefreshWiringReceipts only queries original commands and retains receipts.
// It never calls Begin/POST, creates a retry or releases quarantined leases;
// ordinary explicit Execution reconciliation remains required afterwards.
func (s *Service) RefreshWiringReceipts(ctx context.Context, limit int) (int, error) {
	if s.WiringStore == nil || !s.WiringStore.WiringEnabled {
		return 0, nil
	}
	if limit < 1 || limit > 32 {
		return 0, invalid("Wiring recovery batch must be 1..32")
	}
	rows, e := s.DB.QueryContext(ctx, `SELECT a.id,a.execution_id,a.command_id,a.fencing_token,x.project_id,t.resource_key,coalesce(c.origin,'') FROM exec_task_attempts a JOIN exec_executions x ON x.id=a.execution_id JOIN exec_targets t ON t.id=a.target_id LEFT JOIN validation_wiring_commands c ON c.command_id=a.command_id WHERE a.state='unknown' AND x.parameters->>'validation_kind'='wiring' AND NOT EXISTS(SELECT 1 FROM exec_runner_receipts r WHERE r.command_id=a.command_id) ORDER BY a.created_at,a.id LIMIT $1`, limit)
	if e != nil {
		return 0, e
	}
	type original struct {
		binding wiring.ReadbackBinding
		machine string
	}
	items := []original{}
	for rows.Next() {
		var item original
		if e = rows.Scan(&item.binding.AttemptID, &item.binding.ExecutionID, &item.binding.CommandID, &item.binding.FencingToken, &item.binding.ProjectID, &item.machine, &item.binding.Origin); e != nil {
			rows.Close()
			return 0, e
		}
		items = append(items, item)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return 0, e
	}
	count := 0
	for _, item := range items {
		b := item.binding
		code := "WIRING_LOCAL_NOT_DISPATCHED"
		var prior string
		queryErr := s.DB.QueryRowContext(ctx, `SELECT code FROM validation_wiring_nonstarts WHERE command_id=$1`, b.CommandID).Scan(&prior)
		if queryErr == nil {
			code = prior
		} else if !errors.Is(queryErr, sql.ErrNoRows) {
			return count, queryErr
		}
		if e = s.WiringStore.RecordWiringNonstart(ctx, b, code); e == nil {
			now := time.Now().UTC()
			r := Receipt{CommandID: b.CommandID, AttemptID: b.AttemptID, FencingToken: b.FencingToken, Result: "failed", StartedAt: now, FinishedAt: now, Output: map[string]any{"wiring_nonstart_code": code}}
			if e = s.RecordReceipt(ctx, r); e != nil {
				return count, e
			}
			count++
			continue
		}
		if s.WiringAdapters == nil {
			continue
		}
		command, err := s.WiringStore.WiringOriginalCommand(ctx, b)
		if err != nil {
			continue
		}
		_, client, err := s.WiringAdapters.Open(command)
		if err != nil {
			continue
		}
		record, err := s.WiringStore.QueryWiringCommand(ctx, b, client)
		if err != nil || record == nil {
			continue
		}
		result := "failed"
		if record.Evaluation.Step2 == "PASS" && record.Evaluation.Step3 == "PASS" {
			result = "success"
		}
		r := Receipt{CommandID: b.CommandID, AttemptID: b.AttemptID, FencingToken: b.FencingToken, Result: result, StartedAt: record.StoppedAt, FinishedAt: record.StoppedAt, Output: map[string]any{"wiring_raw_sha256": record.RawSHA256, "step2": record.Evaluation.Step2, "step3": record.Evaluation.Step3, "host_origin": command.HostOrigin, "switch_origin": b.Origin}}
		if e = s.RecordReceipt(ctx, r); e != nil {
			return count, e
		}
		count++
	}
	return count, nil
}
