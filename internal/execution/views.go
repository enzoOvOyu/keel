package execution

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/enzoOvOyu/keel/internal/platform"
	"strings"
	"time"
)

type TaskTimeline struct {
	TaskID         string         `json:"task_id"`
	TaskType       string         `json:"task_type"`
	FirstStartedAt *time.Time     `json:"first_started_at"`
	LastObservedAt *time.Time     `json:"last_observed_at"`
	Counts         map[string]int `json:"counts"`
}
type ExecutionProgress struct {
	TotalTargets    int `json:"total_targets"`
	CompleteTargets int `json:"complete_targets"`
	SuccessTargets  int `json:"success_targets"`
	FailedTargets   int `json:"failed_targets"`
	UnknownTargets  int `json:"unknown_targets"`
}
type ExecutionOverview struct {
	Version     string            `json:"version"`
	Execution   ExecutionSummary  `json:"execution"`
	FlowID      string            `json:"flow_id"`
	FlowName    string            `json:"flow_name"`
	FlowVersion int64             `json:"flow_version"`
	FlowHash    string            `json:"flow_hash"`
	Counts      map[string]int    `json:"counts"`
	Progress    ExecutionProgress `json:"progress"`
	Timeline    []TaskTimeline    `json:"timeline"`
	EventCursor int64             `json:"event_cursor"`
	ObservedAt  time.Time         `json:"observed_at"`
}

func executionSummary(x Execution) ExecutionSummary {
	return ExecutionSummary{ID: x.ID, ProjectID: x.ProjectID, FlowVersionID: x.FlowVersionID, Mode: x.Mode, State: x.State, StateVersion: x.StateVersion, DesiredState: x.DesiredState, TargetSetHash: x.TargetSetHash, TargetCount: len(x.Snapshot.Targets), Concurrency: x.Concurrency, CircuitBreakerPercent: x.CircuitBreakerPercent, CreatedBy: x.CreatedBy, CreatedAt: x.CreatedAt, StartedAt: x.StartedAt, FinishedAt: x.FinishedAt}
}
func readViewExecution(ctx context.Context, tx *sql.Tx, project, id string) (Execution, error) {
	x, e := scanExecution(tx.QueryRowContext(ctx, "SELECT "+executionColumns+" FROM exec_executions WHERE id=$1 AND project_id=$2", id, project))
	if errors.Is(e, sql.ErrNoRows) {
		return x, failure(404, "NOT_FOUND", "Execution not found")
	}
	return x, e
}

const latestAttemptsSQL = `SELECT DISTINCT ON(target_id,task_id) target_id,task_id,state FROM exec_task_attempts WHERE execution_id=$1 ORDER BY target_id,task_id,attempt_no DESC`

func (s *Service) Overview(ctx context.Context, project, id string) (*ExecutionOverview, error) {
	tx, e := s.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	x, e := readViewExecution(ctx, tx, project, id)
	if e != nil {
		return nil, e
	}
	out := &ExecutionOverview{Version: "execution-overview/v1", Execution: executionSummary(x), FlowID: x.Snapshot.Flow.FlowID, FlowName: x.Snapshot.Flow.Name, FlowVersion: x.Snapshot.Flow.Version, FlowHash: x.Snapshot.Flow.ContentHash, Counts: map[string]int{}, Timeline: []TaskTimeline{}, ObservedAt: time.Now().UTC(), Progress: ExecutionProgress{TotalTargets: len(x.Snapshot.Targets)}}
	if e = tx.QueryRowContext(ctx, "SELECT coalesce(max(sequence),0) FROM exec_outbox WHERE execution_id=$1", id).Scan(&out.EventCursor); e != nil {
		return nil, e
	}
	rows, e := tx.QueryContext(ctx, "SELECT task_id,state,count(*) FROM ("+latestAttemptsSQL+") latest GROUP BY task_id,state", id)
	if e != nil {
		return nil, e
	}
	taskCounts := map[string]map[string]int{}
	for rows.Next() {
		var task, state string
		var n int
		if e = rows.Scan(&task, &state, &n); e != nil {
			rows.Close()
			return nil, e
		}
		if taskCounts[task] == nil {
			taskCounts[task] = map[string]int{}
		}
		taskCounts[task][state] = n
		out.Counts[state] += n
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	query := `WITH latest AS (` + latestAttemptsSQL + `), grouped AS (SELECT target_id,bool_and(state IN('success','failed','cancelled','skipped')) AND count(*)=$2 complete,bool_and(state='success') AND count(*)=$2 success,bool_or(state='failed') failed,bool_or(state='unknown') OR count(*)<>$2 unknown FROM latest GROUP BY target_id) SELECT count(*) FILTER(WHERE complete),count(*) FILTER(WHERE success),count(*) FILTER(WHERE failed),count(*) FILTER(WHERE unknown),count(*) FROM grouped`
	var observedTargets int
	if e = tx.QueryRowContext(ctx, query, id, len(x.Snapshot.Flow.Definition.Tasks)).Scan(&out.Progress.CompleteTargets, &out.Progress.SuccessTargets, &out.Progress.FailedTargets, &out.Progress.UnknownTargets, &observedTargets); e != nil {
		return nil, e
	}
	if observedTargets < out.Progress.TotalTargets {
		out.Progress.UnknownTargets += out.Progress.TotalTargets - observedTargets
	}
	rows, e = tx.QueryContext(ctx, `SELECT task_id,min(started_at),max(coalesce(finished_at,CASE WHEN state='running' THEN $2::timestamptz END)) FROM exec_task_attempts WHERE execution_id=$1 GROUP BY task_id`, id, out.ObservedAt)
	if e != nil {
		return nil, e
	}
	bounds := map[string]TaskTimeline{}
	for rows.Next() {
		var line TaskTimeline
		if e = rows.Scan(&line.TaskID, &line.FirstStartedAt, &line.LastObservedAt); e != nil {
			rows.Close()
			return nil, e
		}
		bounds[line.TaskID] = line
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	for _, task := range x.Snapshot.Flow.Definition.Tasks {
		line := bounds[task.ID]
		line.TaskID = task.ID
		line.TaskType = task.Type
		line.Counts = taskCounts[task.ID]
		if line.Counts == nil {
			line.Counts = map[string]int{}
		}
		out.Timeline = append(out.Timeline, line)
	}
	return out, tx.Commit()
}

type MatrixCell struct {
	TaskID     string     `json:"task_id"`
	AttemptID  string     `json:"attempt_id"`
	State      string     `json:"state"`
	AttemptNo  int        `json:"attempt_no"`
	StartedAt  *time.Time `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
}
type MatrixRow struct {
	Target Target       `json:"target"`
	Cells  []MatrixCell `json:"cells"`
}
type ExecutionMatrix struct {
	Version     string      `json:"version"`
	ProjectID   string      `json:"project_id"`
	ExecutionID string      `json:"execution_id"`
	Mode        string      `json:"mode"`
	Page        int         `json:"page"`
	PageSize    int         `json:"page_size"`
	Total       int         `json:"total"`
	Tasks       []string    `json:"tasks"`
	Items       []MatrixRow `json:"items"`
	EventCursor int64       `json:"event_cursor"`
}

func validMatrixState(state string) bool {
	switch state {
	case "", "pending", "running", "success", "failed", "cancelled", "unknown", "skipped":
		return true
	}
	return false
}
func (s *Service) Matrix(ctx context.Context, project, id string, page, size int, state, keyword string) (*ExecutionMatrix, error) {
	if page < 1 || page > 1000000 || size < 1 || size > 500 || !validMatrixState(state) || len(keyword) > 128 {
		return nil, invalid("invalid target matrix filters")
	}
	tx, e := s.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	x, e := readViewExecution(ctx, tx, project, id)
	if e != nil {
		return nil, e
	}
	out := &ExecutionMatrix{Version: "execution-matrix/v1", ProjectID: project, ExecutionID: id, Mode: x.Mode, Page: page, PageSize: size, Tasks: []string{}, Items: []MatrixRow{}}
	for _, task := range x.Snapshot.Flow.Definition.Tasks {
		out.Tasks = append(out.Tasks, task.ID)
	}
	filter := ` FROM exec_execution_targets t WHERE t.execution_id=$1 AND ($2='' OR strpos(lower(t.snapshot->>'name'),lower($2))>0 OR strpos(t.target_id,$2)>0) AND ($3='' OR EXISTS(SELECT 1 FROM (` + latestAttemptsSQL + `) latest WHERE latest.target_id=t.target_id AND latest.state=$3))`
	if e = tx.QueryRowContext(ctx, "SELECT count(*)"+filter, id, keyword, state).Scan(&out.Total); e != nil {
		return nil, e
	}
	if e = tx.QueryRowContext(ctx, "SELECT coalesce(max(sequence),0) FROM exec_outbox WHERE execution_id=$1", id).Scan(&out.EventCursor); e != nil {
		return nil, e
	}
	rows, e := tx.QueryContext(ctx, "SELECT t.target_id,t.snapshot"+filter+" ORDER BY t.snapshot->>'name',t.target_id LIMIT $4 OFFSET $5", id, keyword, state, size, (page-1)*size)
	if e != nil {
		return nil, e
	}
	ids := []string{}
	index := map[string]int{}
	for rows.Next() {
		var target string
		var raw []byte
		if e = rows.Scan(&target, &raw); e != nil {
			rows.Close()
			return nil, e
		}
		var frozen Target
		if e = json.Unmarshal(raw, &frozen); e != nil {
			rows.Close()
			return nil, e
		}
		index[target] = len(out.Items)
		ids = append(ids, target)
		out.Items = append(out.Items, MatrixRow{Target: frozen, Cells: []MatrixCell{}})
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	rows, e = tx.QueryContext(ctx, `SELECT target_id,task_id,id,state,attempt_no,started_at,finished_at FROM (SELECT DISTINCT ON(target_id,task_id) target_id,task_id,id,state,attempt_no,started_at,finished_at FROM exec_task_attempts WHERE execution_id=$1 AND target_id IN(SELECT jsonb_array_elements_text($2::jsonb)) ORDER BY target_id,task_id,attempt_no DESC) latest ORDER BY target_id,task_id`, id, jsonBytes(ids))
	if e != nil {
		return nil, e
	}
	cells := map[string]map[string]MatrixCell{}
	for rows.Next() {
		var target string
		var cell MatrixCell
		if e = rows.Scan(&target, &cell.TaskID, &cell.AttemptID, &cell.State, &cell.AttemptNo, &cell.StartedAt, &cell.FinishedAt); e != nil {
			rows.Close()
			return nil, e
		}
		if cells[target] == nil {
			cells[target] = map[string]MatrixCell{}
		}
		cells[target][cell.TaskID] = cell
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	for target, i := range index {
		for _, task := range out.Tasks {
			cell, ok := cells[target][task]
			if !ok {
				cell = MatrixCell{TaskID: task, State: "not_scheduled"}
			}
			out.Items[i].Cells = append(out.Items[i].Cells, cell)
		}
	}
	return out, tx.Commit()
}

type JournalEntry struct {
	Event
	Level string `json:"level"`
}
type JournalPage struct {
	Version        string         `json:"version"`
	Kind           string         `json:"kind"`
	Items          []JournalEntry `json:"items"`
	NextCursor     int64          `json:"next_cursor"`
	OldestCursor   int64          `json:"oldest_cursor"`
	ResyncRequired bool           `json:"resync_required"`
	HasMore        bool           `json:"has_more"`
}

func cursorRequiresResync(after, min, max int64) bool {
	return after > max || after > 0 && min > 0 && after < min
}
func (s *Service) Journal(ctx context.Context, project, id string, after int64) (*JournalPage, error) {
	if after < 0 {
		return nil, invalid("event cursor must be nonnegative")
	}
	tx, e := s.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	var exists bool
	if e = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM exec_executions WHERE id=$1 AND project_id=$2)", id, project).Scan(&exists); e != nil {
		return nil, e
	}
	if !exists {
		return nil, failure(404, "NOT_FOUND", "Execution not found")
	}
	var min, max int64
	if e = tx.QueryRowContext(ctx, `SELECT coalesce(min(sequence),0),coalesce(max(sequence),0) FROM exec_outbox WHERE execution_id=$1`, id).Scan(&min, &max); e != nil {
		return nil, e
	}
	out := &JournalPage{Version: "execution-journal/v1", Kind: "lifecycle", Items: []JournalEntry{}, NextCursor: after, OldestCursor: min, ResyncRequired: cursorRequiresResync(after, min, max)}
	if after > 0 && !out.ResyncRequired {
		var belongs bool
		if e = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM exec_outbox WHERE execution_id=$1 AND sequence=$2)", id, after).Scan(&belongs); e != nil {
			return nil, e
		}
		out.ResyncRequired = !belongs
	}
	if out.ResyncRequired {
		out.NextCursor = max
		return out, tx.Commit()
	}
	query := `SELECT sequence,id,execution_id,aggregate_version,event_type,payload,created_at FROM exec_outbox WHERE execution_id=$1 AND sequence>$2 ORDER BY sequence LIMIT 200`
	if after == 0 {
		query = `SELECT sequence,id,execution_id,aggregate_version,event_type,payload,created_at FROM (SELECT * FROM exec_outbox WHERE execution_id=$1 AND sequence>$2 ORDER BY sequence DESC LIMIT 200) tail ORDER BY sequence`
	}
	rows, e := tx.QueryContext(ctx, query, id, after)
	if e != nil {
		return nil, e
	}
	for rows.Next() {
		var event Event
		if e = rows.Scan(&event.Sequence, &event.ID, &event.ExecutionID, &event.AggregateVersion, &event.Type, &event.Payload, &event.CreatedAt); e != nil {
			rows.Close()
			return nil, e
		}
		out.Items = append(out.Items, JournalEntry{Event: event, Level: JournalSeverity(event)})
		out.NextCursor = event.Sequence
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	out.HasMore = out.NextCursor < max
	return out, tx.Commit()
}

type ExecutionOutput struct {
	AttemptID string  `json:"attempt_id"`
	TaskID    string  `json:"task_id"`
	TargetID  string  `json:"target_id"`
	Digest    string  `json:"digest"`
	Receipt   Receipt `json:"receipt"`
}
type ExecutionOutputs struct {
	Version            string            `json:"version"`
	Mode               string            `json:"mode"`
	Parameters         map[string]any    `json:"parameters"`
	Page               int               `json:"page"`
	PageSize           int               `json:"page_size"`
	Total              int               `json:"total"`
	Items              []ExecutionOutput `json:"items"`
	ArtifactsSupported bool              `json:"artifacts_supported"`
}

func (s *Service) Outputs(ctx context.Context, project, id string, page, size int) (*ExecutionOutputs, error) {
	if page < 1 || page > 1000000 || size < 1 || size > 100 {
		return nil, invalid("invalid output pagination")
	}
	tx, e := s.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	x, e := readViewExecution(ctx, tx, project, id)
	if e != nil {
		return nil, e
	}
	out := &ExecutionOutputs{Version: "execution-outputs/v1", Mode: x.Mode, Parameters: x.Snapshot.Parameters, Page: page, PageSize: size, Items: []ExecutionOutput{}}
	filter := ` FROM exec_runner_receipts r JOIN exec_task_attempts a ON a.id=r.attempt_id WHERE a.execution_id=$1`
	if e = tx.QueryRowContext(ctx, "SELECT count(*)"+filter, id).Scan(&out.Total); e != nil {
		return nil, e
	}
	rows, e := tx.QueryContext(ctx, "SELECT a.id,a.task_id,a.target_id,r.digest,r.payload"+filter+" ORDER BY r.observed_at DESC,a.id LIMIT $2 OFFSET $3", id, size, (page-1)*size)
	if e != nil {
		return nil, e
	}
	for rows.Next() {
		var item ExecutionOutput
		var raw []byte
		if e = rows.Scan(&item.AttemptID, &item.TaskID, &item.TargetID, &item.Digest, &raw); e != nil {
			rows.Close()
			return nil, e
		}
		if e = json.Unmarshal(raw, &item.Receipt); e != nil {
			rows.Close()
			return nil, e
		}
		out.Items = append(out.Items, item)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	return out, tx.Commit()
}
func (s *Service) ExportOutput(ctx context.Context, a *Actor, project, id, attempt, trace string) (*ExecutionOutput, error) {
	if a == nil {
		return nil, platform.ErrUnauthenticated
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = platform.RecheckSessionTx(ctx, tx, a); e != nil {
		return nil, e
	}
	if !a.User.IsAdmin {
		var member bool
		if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM memberships WHERE user_id=$1 AND project_id=$2)`, a.User.ID, project).Scan(&member); e != nil {
			return nil, e
		}
		if !member {
			return nil, failure(404, "NOT_FOUND", "Project not found")
		}
	}
	var item ExecutionOutput
	var raw []byte
	e = tx.QueryRowContext(ctx, `SELECT a.id,a.task_id,a.target_id,r.digest,r.payload FROM exec_runner_receipts r JOIN exec_task_attempts a ON a.id=r.attempt_id JOIN exec_executions e ON e.id=a.execution_id WHERE e.id=$1 AND e.project_id=$2 AND a.id=$3`, id, project, attempt).Scan(&item.AttemptID, &item.TaskID, &item.TargetID, &item.Digest, &raw)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, failure(404, "NOT_FOUND", "Output receipt not found")
	}
	if e != nil {
		return nil, e
	}
	if e = json.Unmarshal(raw, &item.Receipt); e != nil {
		return nil, e
	}
	if e = audit(ctx, tx, a.User.ID, project, "execution.output.export", attempt, trace, map[string]any{"execution_id": id, "digest": item.Digest}); e != nil {
		return nil, e
	}
	return &item, tx.Commit()
}

type ExecutionRelations struct {
	Version             string         `json:"version"`
	ProjectID           string         `json:"project_id"`
	ExecutionID         string         `json:"execution_id"`
	Flow                FlowVersion    `json:"flow"`
	Parameters          map[string]any `json:"parameters"`
	TargetSetHash       string         `json:"target_set_hash"`
	FrozenAt            time.Time      `json:"frozen_at"`
	KeaDeploymentID     string         `json:"kea_deployment_id"`
	PushJobID           string         `json:"push_job_id"`
	UpstreamExecutionID string         `json:"upstream_execution_id"`
	IssueIDs            []string       `json:"issue_ids"`
	Warnings            []string       `json:"warnings"`
}

func (s *Service) Relations(ctx context.Context, project, id string) (*ExecutionRelations, error) {
	tx, e := s.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	x, e := readViewExecution(ctx, tx, project, id)
	if e != nil {
		return nil, e
	}
	out := &ExecutionRelations{Version: "execution-relations/v1", ProjectID: project, ExecutionID: id, Flow: x.Snapshot.Flow, Parameters: x.Snapshot.Parameters, TargetSetHash: x.Snapshot.TargetSetHash, FrozenAt: x.Snapshot.FrozenAt, IssueIDs: []string{}, Warnings: []string{"issue_and_subflow_links_not_implemented"}}
	if x.Mode == "kea" {
		e = tx.QueryRowContext(ctx, `SELECT id FROM ztp_kea_deployments WHERE project_id=$1 AND execution_id=$2`, project, id).Scan(&out.KeaDeploymentID)
	}
	if x.Mode == "fabric" {
		e = tx.QueryRowContext(ctx, `SELECT id FROM network_push_jobs WHERE project_id=$1 AND (prepare_execution_id=$2 OR push_execution_id=$2)`, project, id).Scan(&out.PushJobID)
	}
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return nil, e
	}
	return out, tx.Commit()
}
func JournalSeverity(event Event) string {
	if strings.Contains(event.Type, "unknown") || strings.Contains(event.Type, "blocked") {
		return "warning"
	}
	var payload map[string]any
	_ = json.Unmarshal(event.Payload, &payload)
	if payload["result"] == "failed" {
		return "error"
	}
	return "info"
}

type ExecutionAttemptDetail struct {
	Attempt Attempt `json:"attempt"`
	Target  Target  `json:"target"`
	Task    Task    `json:"task"`
	Mode    string  `json:"mode"`
}

func (s *Service) AttemptDetail(ctx context.Context, project, id, attempt string) (*ExecutionAttemptDetail, error) {
	tx, e := s.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	x, e := readViewExecution(ctx, tx, project, id)
	if e != nil {
		return nil, e
	}
	value, e := scanAttempt(tx.QueryRowContext(ctx, "SELECT "+attemptColumns+" FROM exec_task_attempts WHERE id=$1 AND execution_id=$2", attempt, id))
	if errors.Is(e, sql.ErrNoRows) {
		return nil, failure(404, "NOT_FOUND", "Task attempt not found")
	}
	if e != nil {
		return nil, e
	}
	out := &ExecutionAttemptDetail{Attempt: value, Mode: x.Mode}
	var raw []byte
	e = tx.QueryRowContext(ctx, "SELECT snapshot FROM exec_execution_targets WHERE execution_id=$1 AND target_id=$2", id, value.TargetID).Scan(&raw)
	if e != nil {
		return nil, e
	}
	if e = json.Unmarshal(raw, &out.Target); e != nil {
		return nil, e
	}
	for _, task := range x.Snapshot.Flow.Definition.Tasks {
		if task.ID == value.TaskID {
			out.Task = task
			break
		}
	}
	e = tx.QueryRowContext(ctx, "SELECT payload FROM exec_runner_receipts WHERE attempt_id=$1", attempt).Scan(&raw)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return nil, e
	}
	if e == nil {
		var receipt Receipt
		if e = json.Unmarshal(raw, &receipt); e != nil {
			return nil, e
		}
		out.Attempt.Receipt = &receipt
	}
	return out, tx.Commit()
}
