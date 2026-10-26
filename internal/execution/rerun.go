package execution

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"unicode/utf8"
)

// Reruns are new executions. Their source remains immutable, and unknown
// results must be reconciled before the source can be used for another run.
type RerunInput struct {
	Mode                string   `json:"mode"`
	TargetIDs           []string `json:"target_ids,omitempty"`
	ExpectedVersion     int64    `json:"expected_version"`
	SelectionHash       string   `json:"selection_hash"`
	ChangeReason        string   `json:"change_reason"`
	ConfirmFullWorkflow bool     `json:"confirm_full_workflow,omitempty"`
}
type RerunTarget struct {
	MachineID    string   `json:"machine_id,omitempty"`
	SerialNumber string   `json:"serial_number,omitempty"`
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	States       []string `json:"states"`
}
type RerunExclusion struct {
	TargetID string `json:"target_id"`
	Reason   string `json:"reason"`
}
type RerunCandidates struct {
	Version                          string           `json:"version"`
	ExecutionMode                    string           `json:"execution_mode"`
	RequiresFullWorkflowConfirmation bool             `json:"requires_full_workflow_confirmation"`
	ProjectID                        string           `json:"project_id"`
	SourceExecutionID                string           `json:"source_execution_id"`
	SourceVersion                    int64            `json:"source_version"`
	Mode                             string           `json:"mode"`
	FlowVersionID                    string           `json:"flow_version_id"`
	FlowHash                         string           `json:"flow_hash"`
	Parameters                       map[string]any   `json:"parameters"`
	Targets                          []RerunTarget    `json:"targets"`
	Excluded                         []RerunExclusion `json:"excluded"`
	SelectionHash                    string           `json:"selection_hash"`
}
type rerunOrigin struct {
	SourceID string
	Request  RerunInput
}
type targetTaskState struct {
	TargetID string
	TaskID   string
	State    string
}

func normalizeRerunInput(in RerunInput, requireVersion bool) (RerunInput, error) {
	if in.Mode != "failed" && in.Mode != "targets" {
		return in, invalid("rerun mode must be failed or targets")
	}
	if in.Mode == "failed" && len(in.TargetIDs) > 0 {
		return in, invalid("failed rerun derives targets from the source; do not provide target_ids")
	}
	if in.Mode == "targets" {
		if len(in.TargetIDs) < 1 || len(in.TargetIDs) > 2000 {
			return in, invalid("select 1..2000 source targets")
		}
		ids, e := sortedUnique(in.TargetIDs)
		if e != nil {
			return in, e
		}
		in.TargetIDs = ids
	}
	if requireVersion {
		if decoded, e := hex.DecodeString(in.SelectionHash); e != nil || len(decoded) != 32 {
			return in, invalid("rerun requires the exact preview selection_hash")
		}
		in.ChangeReason = strings.TrimSpace(in.ChangeReason)
		if in.ExpectedVersion < 1 || utf8.RuneCountInString(in.ChangeReason) < 3 || utf8.RuneCountInString(in.ChangeReason) > 1000 {
			return in, invalid("rerun requires current expected_version and a 3..1000 character change_reason")
		}
	}
	return in, nil
}

// selectRerunTargets is shared by the preview and the transaction that creates
// the new execution. It never guesses success from absent attempts or receipts.
func selectRerunTargets(snapshot Snapshot, states []targetTaskState, mode string, selected []string) ([]RerunTarget, []RerunExclusion, error) {
	expected := map[string]bool{}
	for _, task := range snapshot.Flow.Definition.Tasks {
		expected[task.ID] = true
	}
	if len(expected) == 0 {
		return nil, nil, conflict("source workflow has no supported tasks")
	}
	requested := map[string]bool{}
	for _, id := range selected {
		requested[id] = true
	}
	observed := map[string]map[string]string{}
	for _, item := range states {
		if observed[item.TargetID] == nil {
			observed[item.TargetID] = map[string]string{}
		}
		observed[item.TargetID][item.TaskID] = item.State
	}
	targets := []RerunTarget{}
	excluded := []RerunExclusion{}
	known := map[string]bool{}
	for _, target := range snapshot.Targets {
		known[target.ID] = true
		if mode == "targets" && !requested[target.ID] {
			continue
		}
		seen := observed[target.ID]
		failed, unknown, active, missing := false, false, false, len(seen) != len(expected)
		labels := []string{}
		for task := range expected {
			state, ok := seen[task]
			if !ok {
				missing = true
				continue
			}
			labels = append(labels, state)
			switch state {
			case "failed":
				failed = true
			case "unknown":
				unknown = true
			case "pending", "running":
				active = true
			case "success", "cancelled", "skipped":
			default:
				missing = true
			}
		}
		reason := ""
		switch {
		case unknown:
			reason = "result_unknown_requires_reconciliation"
		case active:
			reason = "source_target_not_settled"
		case missing:
			reason = "source_attempt_evidence_incomplete"
		case mode == "failed" && !failed:
			reason = "target_did_not_fail"
		}
		if reason != "" {
			excluded = append(excluded, RerunExclusion{TargetID: target.ID, Reason: reason})
			continue
		}
		sort.Strings(labels)
		targets = append(targets, RerunTarget{ID: target.ID, Name: target.Name, States: labels})
	}
	for id := range requested {
		if !known[id] {
			return nil, nil, invalid("rerun target does not belong to the immutable source target set")
		}
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].ID < targets[j].ID })
	sort.Slice(excluded, func(i, j int) bool { return excluded[i].TargetID < excluded[j].TargetID })
	return targets, excluded, nil
}
func loadRerunCandidatesTx(ctx context.Context, tx *sql.Tx, project, source string, in RerunInput) (*RerunCandidates, *Execution, error) {
	parent, e := scanExecution(tx.QueryRowContext(ctx, "SELECT "+executionColumns+" FROM exec_executions WHERE id=$1 AND project_id=$2 FOR SHARE", source, project))
	if errors.Is(e, sql.ErrNoRows) {
		return nil, nil, failure(404, "NOT_FOUND", "Source execution not found")
	}
	if e != nil {
		return nil, nil, e
	}
	native := validNativeHostRerun(&parent)
	if !native && (parent.Mode != "sim" || parent.Snapshot.Mode != "sim" || parent.Snapshot.Flow.Definition.APIVersion != "keel.sim/v1" || parent.Snapshot.Flow.Definition.Validate() != nil) {
		return nil, nil, failure(409, "RERUN_OWNED_FLOW", "owned or unsupported workflows must be restarted through their approved business entry point")
	}
	if !terminal(parent.State) {
		return nil, nil, failure(409, "RERUN_SOURCE_UNSETTLED", "source execution must be settled; unknown results require reconciliation before rerun")
	}
	if in.ExpectedVersion > 0 && in.ExpectedVersion != parent.StateVersion {
		return nil, nil, conflict("source execution version changed; refresh rerun candidates")
	}
	rows, e := tx.QueryContext(ctx, `SELECT target_id,task_id,CASE WHEN (state IN('success','failed') OR (state IN('cancelled','skipped') AND started_at IS NOT NULL)) AND NOT EXISTS(SELECT 1 FROM exec_runner_receipts r WHERE r.attempt_id=latest.id) THEN 'unknown' ELSE state END FROM (SELECT DISTINCT ON(target_id,task_id) id,target_id,task_id,state,started_at FROM exec_task_attempts WHERE execution_id=$1 ORDER BY target_id,task_id,attempt_no DESC) latest`, source)
	if e != nil {
		return nil, nil, e
	}
	states := []targetTaskState{}
	for rows.Next() {
		var state targetTaskState
		if e = rows.Scan(&state.TargetID, &state.TaskID, &state.State); e != nil {
			rows.Close()
			return nil, nil, e
		}
		states = append(states, state)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, nil, e
	}
	targets, excluded, e := selectRerunTargets(parent.Snapshot, states, in.Mode, in.TargetIDs)
	if e != nil {
		return nil, nil, e
	}
	if native {
		byID := map[string]Target{}
		for _, target := range parent.Snapshot.Targets {
			byID[target.ID] = target
		}
		for i := range targets {
			target := byID[targets[i].ID]
			if !opaqueID.MatchString(target.ResourceKey) {
				return nil, nil, conflict("source host target has no stable machine identity")
			}
			targets[i].MachineID = target.ResourceKey
			err := tx.QueryRowContext(ctx, `SELECT m.serial_number FROM machines m JOIN project_assets a ON a.machine_id=m.id WHERE m.id=$1 AND a.project_id=$2`, target.ResourceKey, project).Scan(&targets[i].SerialNumber)
			if errors.Is(err, sql.ErrNoRows) {
				return nil, nil, conflict("source machine no longer belongs to this project")
			}
			if err != nil {
				return nil, nil, err
			}
		}
	}
	out := &RerunCandidates{Version: "execution-rerun-candidates/v1", ProjectID: project, ExecutionMode: parent.Mode, RequiresFullWorkflowConfirmation: native, SourceExecutionID: source, SourceVersion: parent.StateVersion, Mode: in.Mode, FlowVersionID: parent.Snapshot.Flow.ID, FlowHash: parent.Snapshot.Flow.ContentHash, Parameters: parent.Snapshot.Parameters, Targets: targets, Excluded: excluded}
	out.SelectionHash = hash(jsonBytes(map[string]any{"source": source, "execution_mode": parent.Mode, "version": parent.StateVersion, "mode": in.Mode, "targets": targets, "excluded": excluded, "flow": out.FlowVersionID, "flow_hash": out.FlowHash, "parameters": out.Parameters}))
	return out, &parent, nil
}
func (s *Service) RerunCandidates(ctx context.Context, a *Actor, project, source, mode string, ids []string) (*RerunCandidates, error) {
	in, e := normalizeRerunInput(RerunInput{Mode: mode, TargetIDs: ids}, false)
	if e != nil {
		return nil, e
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = authorize(ctx, tx, a, project, "Admin", "DE", "DL", "SRE"); e != nil {
		return nil, e
	}
	out, _, e := loadRerunCandidatesTx(ctx, tx, project, source, in)
	if e != nil {
		return nil, e
	}
	return out, tx.Commit()
}
func (s *Service) Rerun(ctx context.Context, a *Actor, project, source string, in RerunInput, key, trace string) (*Execution, error) {
	normalized, e := normalizeRerunInput(in, true)
	if e != nil {
		return nil, e
	}
	if !opaqueID.MatchString(source) {
		return nil, invalid("invalid source execution id")
	}
	return s.create(ctx, a, project, CreateInput{}, key, trace, &rerunOrigin{SourceID: source, Request: normalized})
}
func prepareRerunInput(preview *RerunCandidates, parent *Execution, origin *rerunOrigin) (CreateInput, error) {
	if preview.SelectionHash != origin.Request.SelectionHash {
		return CreateInput{}, conflict("rerun selection changed; refresh and confirm the exact target set")
	}
	if origin.Request.Mode == "targets" && len(preview.Excluded) > 0 {
		return CreateInput{}, failure(409, "RERUN_TARGET_BLOCKED", "selected source targets are unknown, unsettled, or lack complete attempt evidence")
	}
	if len(preview.Targets) == 0 {
		return CreateInput{}, failure(409, "RERUN_NO_TARGETS", "no eligible source targets to rerun")
	}
	ids := make([]string, 0, len(preview.Targets))
	for _, target := range preview.Targets {
		ids = append(ids, target.ID)
	}
	return CreateInput{Mode: "sim", FlowVersionID: preview.FlowVersionID, TargetIDs: ids, Parameters: preview.Parameters, Concurrency: parent.Concurrency, CircuitBreakerPercent: parent.CircuitBreakerPercent}, nil
}

func validNativeHostRerun(parent *Execution) bool {
	if parent == nil || (parent.Mode != "ssh" && parent.Mode != "agent") || parent.Snapshot.Mode != parent.Mode || parent.Snapshot.Flow.Definition.APIVersion != "keel.host/v1" || parent.Snapshot.SourceFlowContentHash == "" || len(parent.Snapshot.Parameters) > 0 || len(parent.Snapshot.Catalog) > 0 || hash(jsonBytes(parent.Snapshot.Flow.Definition)) != parent.Snapshot.Flow.ContentHash {
		return false
	}
	definition, err := decode[Definition](jsonBytes(parent.Snapshot.Flow.Definition))
	if err != nil {
		return false
	}
	for i := range definition.Tasks {
		definition.Tasks[i].Script = nil
		definition.Tasks[i].Artifact = nil
	}
	return definition.Validate() == nil && hash(jsonBytes(definition)) == parent.Snapshot.SourceFlowContentHash
}

func (s *Service) createNativeHostRerunTx(ctx context.Context, tx *sql.Tx, a *Actor, project string, origin *rerunOrigin, trace string, preview *RerunCandidates, parent *Execution) (*Execution, error) {
	if !origin.Request.ConfirmFullWorkflow {
		return nil, failure(422, "RERUN_CONFIRMATION_REQUIRED", "explicit confirmation is required to rerun every task of the original host workflow")
	}
	if preview.SelectionHash != origin.Request.SelectionHash {
		return nil, conflict("rerun selection changed; refresh and confirm the exact target set")
	}
	if origin.Request.Mode == "targets" && len(preview.Excluded) > 0 {
		return nil, failure(409, "RERUN_TARGET_BLOCKED", "selected source targets lack settled durable evidence")
	}
	if len(preview.Targets) == 0 {
		return nil, failure(409, "RERUN_NO_TARGETS", "no eligible source targets to rerun")
	}
	machines := make([]string, 0, len(preview.Targets))
	for _, target := range preview.Targets {
		machines = append(machines, target.MachineID)
	}
	sort.Strings(machines)
	in := HostFlowCreateInput{FlowVersionID: parent.Snapshot.Flow.ID, Transport: parent.Mode, MachineIDs: machines, Concurrency: parent.Concurrency, CircuitBreakerPercent: parent.CircuitBreakerPercent}
	if len(machines) > 32 || in.Concurrency < 1 || in.Concurrency > 32 || in.CircuitBreakerPercent < 1 || in.CircuitBreakerPercent > 100 {
		return nil, conflict("source host workflow admission limits changed")
	}
	execution, err := s.createHostFlowTx(ctx, tx, a, project, in, trace, parent, origin)
	return execution, err
}
