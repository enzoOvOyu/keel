package execution

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

type FlowValidationInput struct {
	YAMLSource string         `json:"yaml_source"`
	Parameters map[string]any `json:"parameters"`
}
type FlowValidation struct {
	Version     string         `json:"version"`
	Definition  Definition     `json:"definition"`
	ContentHash string         `json:"content_hash"`
	TaskOrder   []string       `json:"task_order"`
	Parameters  map[string]any `json:"parameters"`
	Warnings    []string       `json:"warnings"`
}

// ValidateFlow reuses exactly the executable parser and scalar-input validator.
// It does not publish a version, dispatch work, or touch the database.
func ValidateFlow(in FlowValidationInput) (*FlowValidation, error) {
	d, e := (PublishInput{YAMLSource: in.YAMLSource}).Parse()
	if e != nil {
		return nil, e
	}
	parameters, e := d.Parameters(in.Parameters, false)
	if e != nil {
		return nil, e
	}
	order := []string{}
	done := map[string]bool{}
	var visit func(string)
	tasks := map[string]Task{}
	for _, task := range d.Tasks {
		tasks[task.ID] = task
	}
	visit = func(id string) {
		if done[id] {
			return
		}
		for _, dep := range tasks[id].DependsOn {
			visit(dep)
		}
		done[id] = true
		order = append(order, id)
	}
	for _, task := range d.Tasks {
		visit(task.ID)
	}
	warnings := []string{"simulation_only_no_hardware_evidence"}
	if d.APIVersion == "keel.host/v1" {
		warnings = []string{"bounded_readonly_host_flow_requires_explicit_native_admission", "no_hardware_acceptance_evidence"}
	}
	return &FlowValidation{Version: "flow-validation/v1", Definition: d, ContentHash: hash(jsonBytes(d)), TaskOrder: order, Parameters: parameters, Warnings: warnings}, nil
}

type FlowRevisionDetail struct {
	Revision  FlowVersion `json:"revision"`
	CreatedBy string      `json:"created_by"`
}
type FlowChange struct {
	Path   string          `json:"path"`
	Kind   string          `json:"kind"`
	Before json.RawMessage `json:"before"`
	After  json.RawMessage `json:"after"`
}
type FlowDiff struct {
	Version string       `json:"version"`
	Before  FlowVersion  `json:"before"`
	After   FlowVersion  `json:"after"`
	Changes []FlowChange `json:"changes"`
}

func DiffFlowDefinitions(before, after Definition) []FlowChange {
	out := []FlowChange{}
	add := func(path string, left, right any) {
		l, r := jsonBytes(left), jsonBytes(right)
		if string(l) == string(r) {
			return
		}
		kind := "changed"
		if left == nil {
			kind = "added"
		}
		if right == nil {
			kind = "removed"
		}
		out = append(out, FlowChange{Path: path, Kind: kind, Before: l, After: r})
	}
	add("api_version", before.APIVersion, after.APIVersion)
	// Stable declaration order, followed by new keys in sorted order, makes reports reproducible.
	names := map[string]bool{}
	for name := range before.Inputs {
		names[name] = true
	}
	for name := range after.Inputs {
		names[name] = true
	}
	keys := []string{}
	for name := range names {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	for _, name := range keys {
		left, lok := before.Inputs[name]
		right, rok := after.Inputs[name]
		var l, r any
		if lok {
			l = left
		}
		if rok {
			r = right
		}
		add("inputs/"+name, l, r)
	}
	tasks := map[string]Task{}
	for _, task := range after.Tasks {
		tasks[task.ID] = task
	}
	seen := map[string]bool{}
	for _, left := range before.Tasks {
		seen[left.ID] = true
		right, ok := tasks[left.ID]
		var r any
		if ok {
			r = right
		}
		add("tasks/"+left.ID, left, r)
	}
	for _, right := range after.Tasks {
		if !seen[right.ID] {
			add("tasks/"+right.ID, nil, right)
		}
	}
	leftOrder, rightOrder := []string{}, []string{}
	for _, task := range before.Tasks {
		leftOrder = append(leftOrder, task.ID)
	}
	for _, task := range after.Tasks {
		rightOrder = append(rightOrder, task.ID)
	}
	add("task_order", leftOrder, rightOrder)
	return out
}
func (s *Service) FlowRevision(ctx context.Context, project, flow, revision string) (*FlowRevisionDetail, error) {
	out := &FlowRevisionDetail{}
	v, e := scanFlow(s.DB.QueryRowContext(ctx, "SELECT "+flowColumns+" FROM exec_flow_versions v JOIN exec_flows f ON f.id=v.flow_id WHERE v.project_id=$1 AND v.flow_id=$2 AND v.id=$3", project, flow, revision))
	if errors.Is(e, sql.ErrNoRows) {
		return nil, failure(404, "NOT_FOUND", "Flow revision not found")
	}
	if e != nil {
		return nil, e
	}
	out.Revision = v
	e = s.DB.QueryRowContext(ctx, "SELECT created_by FROM exec_flow_versions WHERE id=$1 AND project_id=$2", revision, project).Scan(&out.CreatedBy)
	return out, e
}
func (s *Service) FlowDiff(ctx context.Context, project, flow, before, after string) (*FlowDiff, error) {
	left, e := s.FlowRevision(ctx, project, flow, before)
	if e != nil {
		return nil, e
	}
	right, e := s.FlowRevision(ctx, project, flow, after)
	if e != nil {
		return nil, e
	}
	return &FlowDiff{Version: "flow-diff/v1", Before: left.Revision, After: right.Revision, Changes: DiffFlowDefinitions(left.Revision.Definition, right.Revision.Definition)}, nil
}

type RollbackFlowInput struct {
	SourceVersionID string `json:"source_version_id"`
	ExpectedVersion int64  `json:"expected_version"`
	ChangeReason    string `json:"change_reason"`
}

func (s *Service) RollbackFlow(ctx context.Context, a *Actor, project, flow string, in RollbackFlowInput, key, trace string) (*FlowVersion, error) {
	if !opaqueID.MatchString(in.SourceVersionID) || in.ExpectedVersion < 1 || utf8.RuneCountInString(strings.TrimSpace(in.ChangeReason)) < 3 || utf8.RuneCountInString(in.ChangeReason) > 1000 {
		return nil, invalid("rollback requires source_version_id, current expected_version and a 3..1000 character reason")
	}
	source, e := s.FlowRevision(ctx, project, flow, in.SourceVersionID)
	if e != nil {
		return nil, e
	}
	publication := PublishInput{Name: source.Revision.Name, ExpectedVersion: in.ExpectedVersion}
	if strings.TrimSpace(source.Revision.YAMLSource) != "" {
		publication.YAMLSource = source.Revision.YAMLSource
	} else {
		publication.Definition = &source.Revision.Definition
	}
	return s.publishVersion(ctx, a, project, flow, publication, key, trace, in.SourceVersionID, strings.TrimSpace(in.ChangeReason))
}
func verifyRollbackSource(ctx context.Context, tx *sql.Tx, project, flow, source string, definition Definition) error {
	var raw []byte
	e := tx.QueryRowContext(ctx, "SELECT definition FROM exec_flow_versions WHERE id=$1 AND project_id=$2 AND flow_id=$3 FOR SHARE", source, project, flow).Scan(&raw)
	if errors.Is(e, sql.ErrNoRows) {
		return failure(404, "NOT_FOUND", "Rollback source not found")
	}
	if e != nil {
		return e
	}
	var frozen Definition
	if e = json.Unmarshal(raw, &frozen); e != nil {
		return e
	}
	if hash(jsonBytes(frozen)) != hash(jsonBytes(definition)) {
		return fmt.Errorf("rollback source definition changed")
	}
	return nil
}
