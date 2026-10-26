package execution

import (
	"context"
	"database/sql"
	"errors"
	"github.com/enzoOvOyu/keel/internal/platform"
	"time"
)

const executionColumns = "id,project_id,flow_version_id,mode,state,state_version,desired_state,parameters,snapshot,target_set_hash,concurrency,circuit_breaker_percent,created_by,created_at,started_at,finished_at"

func scanExecution(r scanner) (Execution, error) {
	var x Execution
	var params, snapshot []byte
	e := r.Scan(&x.ID, &x.ProjectID, &x.FlowVersionID, &x.Mode, &x.State, &x.StateVersion, &x.DesiredState, &params, &snapshot, &x.TargetSetHash, &x.Concurrency, &x.CircuitBreakerPercent, &x.CreatedBy, &x.CreatedAt, &x.StartedAt, &x.FinishedAt)
	if e == nil {
		x.Parameters, e = decode[map[string]any](params)
	}
	if e == nil {
		x.Snapshot, e = decode[Snapshot](snapshot)
	}
	return x, e
}
func (s *Service) Create(ctx context.Context, a *Actor, projectID string, in CreateInput, key, trace string) (*Execution, error) {
	return s.create(ctx, a, projectID, in, key, trace, nil)
}

func (s *Service) create(ctx context.Context, a *Actor, projectID string, in CreateInput, key, trace string, origin *rerunOrigin) (execution *Execution, resultErr error) {
	var rawInput any = in
	scope := "execution.create:" + projectID
	if origin != nil {
		rawInput = origin
		scope = "execution.rerun:" + projectID + ":" + origin.SourceID
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer func() {
		tx.Rollback()
		if s.MediaStore != nil && a != nil {
			resultErr = s.MediaStore.RecordMediaIntegrityFailure(ctx, resultErr, a.User.ID, trace)
		}
	}()
	if e = authorize(ctx, tx, a, projectID, "Admin", "DE", "DL", "SRE"); e != nil {
		return nil, e
	}
	idem, prior, e := idempotency(ctx, tx, a, scope, key, rawInput)
	if e != nil {
		return nil, e
	}
	if prior != nil {
		x, e := decode[Execution](prior)
		return &x, e
	}
	if origin != nil {
		in, e = prepareRerunTx(ctx, tx, projectID, origin)
		if e != nil {
			return nil, e
		}
	}
	if in.Mode != "sim" || len(in.TargetIDs) < 1 || len(in.TargetIDs) > 2000 {
		return nil, invalid("mode=sim and 1..2000 registered target IDs are required")
	}
	ids, e := sortedUnique(in.TargetIDs)
	if e != nil {
		return nil, e
	}
	if in.Concurrency == 0 {
		in.Concurrency = 16
	}
	policy, e := platform.ResolveExecutionCircuitBreakerTx(ctx, tx, projectID)
	if e != nil {
		return nil, e
	}
	if in.CircuitBreakerPercent == 0 {
		in.CircuitBreakerPercent = policy.DefaultPercent
	}
	if in.Concurrency < 1 || in.Concurrency > 256 || in.CircuitBreakerPercent < 1 || in.CircuitBreakerPercent > 100 {
		return nil, invalid("invalid concurrency or circuit breaker percentage")
	}
	flow, e := scanFlow(tx.QueryRowContext(ctx, "SELECT "+flowColumns+" FROM exec_flow_versions v JOIN exec_flows f ON f.id=v.flow_id WHERE v.id=$1 AND v.project_id=$2", in.FlowVersionID, projectID))
	if errors.Is(e, sql.ErrNoRows) {
		return nil, failure(404, "NOT_FOUND", "Flow version not found")
	}
	if e != nil {
		return nil, e
	}
	if flow.Definition.APIVersion != "keel.sim/v1" {
		return nil, invalid("domain-owned workflows require their domain admission endpoint")
	}
	params, e := flow.Definition.Parameters(in.Parameters, true)
	if e != nil {
		return nil, e
	}
	catalog, e := platform.ValidateCatalogTx(ctx, tx, projectID, params)
	if e != nil {
		return nil, e
	}
	if !catalog.Valid {
		i := catalog.Issues[0]
		return nil, failure(422, i.Code, i.Message)
	}
	if s.MediaStore == nil {
		for _, selection := range catalog.Selections {
			if selection.Media != nil {
				return nil, failure(503, "MEDIA_STORAGE_UNAVAILABLE", "Media storage is not configured")
			}
		}
	} else if e = s.MediaStore.VerifyCatalogArtifactsTx(ctx, tx, catalog.Selections); e != nil {
		return nil, e
	}
	rows, e := tx.QueryContext(ctx, "SELECT id,project_id,name,seed,revision,model_version,created_at FROM exec_sim_targets WHERE project_id=$1 AND id IN(SELECT jsonb_array_elements_text($2::jsonb)) ORDER BY id FOR SHARE", projectID, jsonBytes(ids))
	if e != nil {
		return nil, e
	}
	targets := []SimTarget{}
	for rows.Next() {
		var t SimTarget
		t.Mode = "sim"
		if e = rows.Scan(&t.ID, &t.ProjectID, &t.Name, &t.Seed, &t.Revision, &t.ModelVersion, &t.CreatedAt); e != nil {
			rows.Close()
			return nil, e
		}
		targets = append(targets, t)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	if len(targets) != len(ids) {
		return nil, failure(404, "TARGET_SCOPE_MISMATCH", "one or more simulation targets are outside the project")
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	x := &Execution{ID: platform.RandomID(), ProjectID: projectID, FlowVersionID: flow.ID, Mode: "sim", State: "created", StateVersion: 1, DesiredState: "running", Parameters: params, TargetSetHash: hash(jsonBytes(ids)), Concurrency: in.Concurrency, CircuitBreakerPercent: in.CircuitBreakerPercent, CreatedBy: a.User.ID, CreatedAt: now}
	x.Snapshot = Snapshot{CircuitBreakerPolicy: policy, Catalog: catalog.Selections, Mode: "sim", Flow: flow, Targets: targets, Parameters: params, TargetSetHash: x.TargetSetHash, Admission: "simulation only; not hardware gate evidence", FrozenAt: now}
	if origin != nil {
		// Preserve source target identities/revisions rather than silently changing the rerun.
		source, err := scanExecution(tx.QueryRowContext(ctx, "SELECT "+executionColumns+" FROM exec_executions WHERE id=$1 AND project_id=$2", origin.SourceID, projectID))
		if err != nil {
			return nil, err
		}
		frozen := map[string]Target{}
		for _, target := range source.Snapshot.Targets {
			frozen[target.ID] = target
		}
		for i, target := range targets {
			previous, ok := frozen[target.ID]
			if !ok || hash(jsonBytes(previous)) != hash(jsonBytes(target)) {
				return nil, conflict("source target revision changed; create a reviewed execution instead")
			}
			targets[i] = previous
		}
		if hash(jsonBytes(catalog.Selections)) != hash(jsonBytes(source.Snapshot.Catalog)) && (len(catalog.Selections) > 0 || len(source.Snapshot.Catalog) > 0) {
			return nil, conflict("catalog definition or approved media changed; create a reviewed execution instead")
		}
		if flow.ContentHash != source.Snapshot.Flow.ContentHash || hash(jsonBytes(params)) != hash(jsonBytes(source.Snapshot.Parameters)) {
			return nil, conflict("source flow or parameters changed")
		}
		x.Snapshot.RerunSource = &RerunSource{ExecutionID: origin.SourceID, StateVersion: origin.Request.ExpectedVersion, Mode: origin.Request.Mode, SelectionHash: origin.Request.SelectionHash, ChangeReason: origin.Request.ChangeReason}
	}
	_, e = tx.ExecContext(ctx, "INSERT INTO exec_executions(id,project_id,flow_version_id,parameters,snapshot,target_set_hash,concurrency,circuit_breaker_percent,created_by,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)", x.ID, projectID, flow.ID, jsonBytes(params), jsonBytes(x.Snapshot), x.TargetSetHash, x.Concurrency, x.CircuitBreakerPercent, a.User.ID, now)
	if e != nil {
		return nil, e
	}
	type targetRow struct {
		ExecutionID string    `json:"execution_id"`
		TargetID    string    `json:"target_id"`
		Snapshot    SimTarget `json:"snapshot"`
	}
	tr := []targetRow{}
	attempts := []map[string]any{}
	for _, t := range targets {
		tr = append(tr, targetRow{x.ID, t.ID, t})
		for i, task := range flow.Definition.Tasks {
			attempts = append(attempts, map[string]any{"id": platform.RandomID(), "execution_id": x.ID, "target_id": t.ID, "task_id": task.ID, "task_index": i, "attempt_no": 1, "command_id": platform.RandomID(), "created_at": now})
		}
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO exec_execution_targets SELECT execution_id,target_id,snapshot FROM jsonb_to_recordset($1::jsonb) AS x(execution_id varchar,target_id varchar,snapshot jsonb)", jsonBytes(tr)); e != nil {
		return nil, e
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO exec_task_attempts(id,execution_id,target_id,task_id,task_index,attempt_no,command_id,created_at) SELECT id,execution_id,target_id,task_id,task_index,attempt_no,command_id,created_at FROM jsonb_to_recordset($1::jsonb) AS x(id varchar,execution_id varchar,target_id varchar,task_id varchar,task_index integer,attempt_no integer,command_id varchar,created_at timestamptz)", jsonBytes(attempts)); e != nil {
		return nil, e
	}
	if e = event(ctx, tx, x.ID, "execution.created", map[string]any{"mode": "sim", "target_count": len(targets)}); e != nil {
		return nil, e
	}
	if e = audit(ctx, tx, a.User.ID, projectID, "execution.create", x.ID, trace, map[string]any{"mode": "sim", "flow_version": flow.ID, "target_hash": x.TargetSetHash, "target_count": len(targets)}); e != nil {
		return nil, e
	}
	if origin != nil {
		if e = event(ctx, tx, x.ID, "execution.rerun", x.Snapshot.RerunSource); e != nil {
			return nil, e
		}
		if e = audit(ctx, tx, a.User.ID, projectID, "execution.rerun", x.ID, trace, x.Snapshot.RerunSource); e != nil {
			return nil, e
		}
	}
	if e = remember(ctx, tx, idem, rawInput, x); e != nil {
		return nil, e
	}
	return x, tx.Commit()
}

const attemptColumns = "id,execution_id,target_id,task_id,task_index,attempt_no,command_id,state,state_version,COALESCE(lease_owner,''),lease_until,COALESCE(fencing_token,0),result,started_at,finished_at"

func scanAttempt(r scanner) (Attempt, error) {
	var a Attempt
	var raw []byte
	e := r.Scan(&a.ID, &a.ExecutionID, &a.TargetID, &a.TaskID, &a.TaskIndex, &a.AttemptNo, &a.CommandID, &a.State, &a.StateVersion, &a.LeaseOwner, &a.LeaseUntil, &a.FencingToken, &raw, &a.StartedAt, &a.FinishedAt)
	a.Result = raw
	return a, e
}
func (s *Service) Get(ctx context.Context, projectID, id string) (*Detail, error) {
	x, e := scanExecution(s.DB.QueryRowContext(ctx, "SELECT "+executionColumns+" FROM exec_executions WHERE id=$1 AND project_id=$2", id, projectID))
	if errors.Is(e, sql.ErrNoRows) {
		return nil, failure(404, "NOT_FOUND", "Execution not found")
	}
	if e != nil {
		return nil, e
	}
	page, e := s.Attempts(ctx, projectID, id, platform.Pagination{Page: 1, PageSize: 100})
	if e != nil {
		return nil, e
	}
	out := &Detail{Execution: x, Attempts: page.Items, AttemptsTotal: page.Total, Counts: map[string]int{}}
	rows, e := s.DB.QueryContext(ctx, "SELECT state,count(*) FROM (SELECT DISTINCT ON(target_id,task_id) state FROM exec_task_attempts WHERE execution_id=$1 ORDER BY target_id,task_id,attempt_no DESC) latest GROUP BY state", id)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	for rows.Next() {
		var state string
		var n int
		if e = rows.Scan(&state, &n); e != nil {
			return nil, e
		}
		out.Counts[state] = n
	}
	return out, rows.Err()
}
func (s *Service) Attempts(ctx context.Context, projectID, id string, p platform.Pagination) (platform.Page[Attempt], error) {
	out := platform.NewPage[Attempt](p)
	if e := p.Validate(); e != nil {
		return out, e
	}
	var exists bool
	if e := s.DB.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM exec_executions WHERE id=$1 AND project_id=$2)", id, projectID).Scan(&exists); e != nil {
		return out, e
	}
	if !exists {
		return out, failure(404, "NOT_FOUND", "Execution not found")
	}
	if e := s.DB.QueryRowContext(ctx, "SELECT count(*) FROM exec_task_attempts WHERE execution_id=$1", id).Scan(&out.Total); e != nil {
		return out, e
	}
	rows, e := s.DB.QueryContext(ctx, "SELECT "+attemptColumns+" FROM exec_task_attempts WHERE execution_id=$1 ORDER BY target_id,task_index,attempt_no LIMIT $2 OFFSET $3", id, p.PageSize, p.Offset())
	if e != nil {
		return out, e
	}
	defer rows.Close()
	ids := []string{}
	index := map[string]int{}
	for rows.Next() {
		a, e := scanAttempt(rows)
		if e != nil {
			return out, e
		}
		index[a.ID] = len(out.Items)
		ids = append(ids, a.ID)
		out.Items = append(out.Items, a)
	}
	if e = rows.Err(); e != nil {
		return out, e
	}
	rows.Close()
	receipts, e := s.DB.QueryContext(ctx, "SELECT attempt_id,payload FROM exec_runner_receipts WHERE attempt_id IN(SELECT jsonb_array_elements_text($1::jsonb))", jsonBytes(ids))
	if e != nil {
		return out, e
	}
	defer receipts.Close()
	for receipts.Next() {
		var id string
		var data []byte
		if e = receipts.Scan(&id, &data); e != nil {
			return out, e
		}
		r, e := decode[Receipt](data)
		if e != nil {
			return out, e
		}
		out.Items[index[id]].Receipt = &r
	}
	return out, receipts.Err()
}
func (s *Service) List(ctx context.Context, projectID string, p platform.Pagination) (platform.Page[ExecutionSummary], error) {
	out := platform.NewPage[ExecutionSummary](p)
	if e := p.Validate(); e != nil {
		return out, e
	}
	if e := s.DB.QueryRowContext(ctx, "SELECT count(*) FROM exec_executions WHERE project_id=$1", projectID).Scan(&out.Total); e != nil {
		return out, e
	}
	rows, e := s.DB.QueryContext(ctx, "SELECT id,project_id,flow_version_id,mode,state,state_version,desired_state,target_set_hash,jsonb_array_length(snapshot->'targets'),concurrency,circuit_breaker_percent,created_by,created_at,started_at,finished_at FROM exec_executions WHERE project_id=$1 ORDER BY created_at DESC,id DESC LIMIT $2 OFFSET $3", projectID, p.PageSize, p.Offset())
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var x ExecutionSummary
		if e = rows.Scan(&x.ID, &x.ProjectID, &x.FlowVersionID, &x.Mode, &x.State, &x.StateVersion, &x.DesiredState, &x.TargetSetHash, &x.TargetCount, &x.Concurrency, &x.CircuitBreakerPercent, &x.CreatedBy, &x.CreatedAt, &x.StartedAt, &x.FinishedAt); e != nil {
			return out, e
		}
		out.Items = append(out.Items, x)
	}
	return out, rows.Err()
}

func (s *Service) Control(ctx context.Context, a *Actor, projectID, id string, in ControlInput, key, trace string) (*Execution, error) {
	if in.Action != "pause" && in.Action != "resume" && in.Action != "cancel" {
		return nil, invalid("action must be pause, resume, or cancel")
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = authorize(ctx, tx, a, projectID, "Admin", "DE", "DL", "SRE", "NDE"); e != nil {
		return nil, e
	}
	idem, prior, e := idempotency(ctx, tx, a, "execution.control:"+id, key, in)
	if e != nil {
		return nil, e
	}
	if prior != nil {
		x, e := decode[Execution](prior)
		return &x, e
	}
	x, e := scanExecution(tx.QueryRowContext(ctx, "SELECT "+executionColumns+" FROM exec_executions WHERE id=$1 AND project_id=$2 FOR UPDATE", id, projectID))
	if errors.Is(e, sql.ErrNoRows) {
		return nil, failure(404, "NOT_FOUND", "Execution not found")
	}
	if e != nil {
		return nil, e
	}
	if x.Mode == "kea" || x.Mode == "fabric" {
		if e = authorize(ctx, tx, a, projectID, "Admin", "NDE"); e != nil {
			return nil, e
		}
	} else {
		if e = authorize(ctx, tx, a, projectID, "Admin", "DE", "DL", "SRE"); e != nil {
			return nil, e
		}
	}
	for _, target := range x.Snapshot.Targets {
		if target.SSHTrustRequestID != "" {
			if e = authorize(ctx, tx, a, projectID, "Admin", "DL", "SRE"); e != nil {
				return nil, e
			}
			break
		}
	}
	if x.StateVersion != in.ExpectedVersion {
		return nil, conflict("execution state changed")
	}
	if terminal(x.State) {
		return nil, conflict("execution is already terminal")
	}
	desired := "paused"
	if in.Action == "resume" {
		if x.State != "paused" {
			return nil, conflict("only a fully paused execution can resume")
		}
		desired = "running"
	}
	if in.Action == "cancel" {
		desired = "killed"
	}
	if x.DesiredState == "killed" && in.Action != "cancel" {
		return nil, conflict("cancelling cannot be resumed")
	}
	if _, e = tx.ExecContext(ctx, "UPDATE exec_executions SET desired_state=$2,state_version=state_version+1 WHERE id=$1", id, desired); e != nil {
		return nil, e
	}
	if desired == "killed" {
		if _, e = tx.ExecContext(ctx, "UPDATE exec_task_attempts SET state='cancelled',state_version=state_version+1,finished_at=now() WHERE execution_id=$1 AND state='pending'", id); e != nil {
			return nil, e
		}
	}
	if desired == "killed" && x.Mode == "ssh" {
		if _, e = tx.ExecContext(ctx, `UPDATE ssh_trust_requests SET state='failed',state_version=state_version+1 WHERE execution_id=$1 AND state='consumed' AND NOT EXISTS(SELECT 1 FROM exec_task_attempts WHERE execution_id=$1 AND state IN('running','unknown'))`, id); e != nil {
			return nil, e
		}
	}
	if in.Action == "resume" {
		if _, e = tx.ExecContext(ctx, "UPDATE exec_executions SET breaker_ack_failures=(SELECT count(DISTINCT target_id) FROM exec_task_attempts a WHERE execution_id=$1 AND state='failed' AND NOT EXISTS(SELECT 1 FROM exec_task_attempts n WHERE n.execution_id=a.execution_id AND n.target_id=a.target_id AND n.task_id=a.task_id AND n.attempt_no>a.attempt_no)) WHERE id=$1", id); e != nil {
			return nil, e
		}
	}
	if e = settle(ctx, tx, id); e != nil {
		return nil, e
	}
	x, e = scanExecution(tx.QueryRowContext(ctx, "SELECT "+executionColumns+" FROM exec_executions WHERE id=$1", id))
	if e != nil {
		return nil, e
	}
	if e = event(ctx, tx, id, "execution.control", in); e != nil {
		return nil, e
	}
	if e = audit(ctx, tx, a.User.ID, projectID, "execution."+in.Action, id, trace, map[string]any{"mode": x.Mode, "state": x.State, "version": x.StateVersion}); e != nil {
		return nil, e
	}
	if e = remember(ctx, tx, idem, in, x); e != nil {
		return nil, e
	}
	return &x, tx.Commit()
}

// Read only control fields from the frozen execution snapshot. Settlement and
// receipt application do not need to transfer/decode every target or result.
// Callers retain the execution-row lock and all lease/fencing checks.
type executionControl struct {
	State, DesiredState, Mode                                string
	CircuitBreakerPercent, TargetCount, AcknowledgedFailures int
	Definition                                               Definition
}

func loadExecutionControl(ctx context.Context, tx *sql.Tx, id string) (executionControl, error) {
	var out executionControl
	var definition []byte
	err := tx.QueryRowContext(ctx, `SELECT state,desired_state,mode,circuit_breaker_percent,breaker_ack_failures,
 jsonb_array_length(snapshot->'targets'),snapshot#>'{flow,definition}' FROM exec_executions WHERE id=$1`, id).
		Scan(&out.State, &out.DesiredState, &out.Mode, &out.CircuitBreakerPercent, &out.AcknowledgedFailures, &out.TargetCount, &definition)
	if err != nil {
		return out, err
	}
	out.Definition, err = decode[Definition](definition)
	return out, err
}

// settle is called with the execution row locked. No completion state can outrun
// unacknowledged in-flight work or a quarantined resource lease.
func settle(ctx context.Context, tx *sql.Tx, id string) error {
	x, e := loadExecutionControl(ctx, tx, id)
	if e != nil {
		return e
	}
	rows, e := tx.QueryContext(ctx, "SELECT DISTINCT ON(target_id,task_id) id,target_id,task_id,state FROM exec_task_attempts WHERE execution_id=$1 ORDER BY target_id,task_id,attempt_no DESC", id)
	if e != nil {
		return e
	}
	latest := map[string]Attempt{}
	for rows.Next() {
		var a Attempt
		if e = rows.Scan(&a.ID, &a.TargetID, &a.TaskID, &a.State); e != nil {
			rows.Close()
			return e
		}
		latest[a.TargetID+":"+a.TaskID] = a
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for pass := 0; pass < len(x.Definition.Tasks); pass++ {
		changed := false
		for k, a := range latest {
			if a.State != "pending" {
				continue
			}
			task, _ := taskByID(x.Definition, a.TaskID)
			blocked := false
			for _, dep := range task.DependsOn {
				st := latest[a.TargetID+":"+dep].State
				if st == "failed" || st == "cancelled" || st == "skipped" {
					blocked = true
				}
			}
			if blocked {
				if _, e = tx.ExecContext(ctx, "UPDATE exec_task_attempts SET state='skipped',state_version=state_version+1,finished_at=now() WHERE id=$1 AND state='pending'", a.ID); e != nil {
					return e
				}
				a.State = "skipped"
				latest[k] = a
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	counts := map[string]int{}
	failedTargets := map[string]bool{}
	for _, a := range latest {
		counts[a.State]++
		if a.State == "failed" {
			failedTargets[a.TargetID] = true
		}
	}
	desired := x.DesiredState
	acknowledged := x.AcknowledgedFailures
	if desired == "running" && counts["pending"] > 0 && len(failedTargets) > acknowledged && 100*len(failedTargets) > x.CircuitBreakerPercent*x.TargetCount {
		desired = "paused"
		if e = event(ctx, tx, id, "execution.circuit_breaker", map[string]any{"failed_targets": len(failedTargets), "total_targets": x.TargetCount, "threshold_percent": x.CircuitBreakerPercent}); e != nil {
			return e
		}
	}
	state := "scheduled"
	finished := false
	switch {
	case counts["unknown"] > 0:
		state = "reconciling"
	case desired == "killed" && counts["running"] > 0:
		state = "cancelling"
	case desired == "killed":
		state = "killed"
		finished = true
	case counts["pending"] == 0 && counts["running"] == 0:
		state = "success"
		if counts["failed"]+counts["skipped"]+counts["cancelled"] > 0 {
			state = "failed"
		}
		finished = true
	case desired == "paused" && counts["running"] > 0:
		state = "pausing"
	case desired == "paused":
		state = "paused"
	case counts["running"] > 0:
		state = "running"
	}
	if state == "killed" && x.Mode == "provision" {
		if e = cancelUnstartedProvisionTx(ctx, tx, id); e != nil {
			return e
		}
	}
	if x.State != state || x.DesiredState != desired {
		if _, e = tx.ExecContext(ctx, "UPDATE exec_executions SET state=$2,desired_state=$3,state_version=state_version+1,finished_at=CASE WHEN $4 THEN now() ELSE NULL END WHERE id=$1", id, state, desired, finished); e != nil {
			return e
		}
		return event(ctx, tx, id, "execution.state_changed", map[string]any{"state": state, "mode": x.Mode})
	}
	return nil
}
func (s *Service) Events(ctx context.Context, projectID, id string, after int64) ([]Event, error) {
	var exists bool
	if e := s.DB.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM exec_executions WHERE id=$1 AND project_id=$2)", id, projectID).Scan(&exists); e != nil {
		return nil, e
	}
	if !exists {
		return nil, failure(404, "NOT_FOUND", "Execution not found")
	}
	rows, e := s.DB.QueryContext(ctx, "SELECT sequence,id,execution_id,aggregate_version,event_type,payload,created_at FROM exec_outbox WHERE execution_id=$1 AND sequence>$2 ORDER BY sequence LIMIT 200", id, after)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var v Event
		if e = rows.Scan(&v.Sequence, &v.ID, &v.ExecutionID, &v.AggregateVersion, &v.Type, &v.Payload, &v.CreatedAt); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Service) ExecutionMode(ctx context.Context, projectID, id string) (string, error) {
	var mode string
	e := s.DB.QueryRowContext(ctx, "SELECT mode FROM exec_executions WHERE project_id=$1 AND id=$2", projectID, id).Scan(&mode)
	if errors.Is(e, sql.ErrNoRows) {
		return "", failure(404, "NOT_FOUND", "Execution not found")
	}
	return mode, e
}
