// Package execution implements a durable workflow kernel with isolated simulation and Kea adapters.
// It does not execute arbitrary shell commands or claim hardware test evidence.
package execution

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/enzoOvOyu/keel/internal/taskrunner"
	"gopkg.in/yaml.v3"
	"io"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

type Service struct {
	agentReceiptMu       sync.Mutex
	agentReceiptCursorAt time.Time
	agentReceiptCursorID string
	AgentRunner          taskrunner.Runner
	// AgentAdmissionOnly enables API admission from authenticated connection
	// metadata. Workers must use their own live AgentRunner instead.
	AgentAdmissionOnly bool
	SSHRunner          taskrunner.Runner
	MediaStore         *platform.Store
	ProvisionStore     *platform.Store
	DB                 *sql.DB
	KeaStore           *platform.Store
	NetworkStore       *platform.Store
}

func New(db *sql.DB) *Service { return &Service{DB: db} }

type Actor = platform.AuthSession
type Input struct {
	Type     string   `json:"type" yaml:"type"`
	Required bool     `json:"required" yaml:"required"`
	Default  any      `json:"default,omitempty" yaml:"default,omitempty"`
	Enum     []string `json:"enum,omitempty" yaml:"enum,omitempty"`
}
type SimSpec struct {
	Outcome       string `json:"outcome" yaml:"outcome"`
	DurationMS    int    `json:"duration_ms" yaml:"duration_ms"`
	FailFirst     int    `json:"fail_first,omitempty" yaml:"fail_first,omitempty"`
	Seed          int64  `json:"seed" yaml:"seed"`
	Uncancellable bool   `json:"uncancellable,omitempty" yaml:"uncancellable,omitempty"`
}
type Task struct {
	ID         string   `json:"id" yaml:"id"`
	Type       string   `json:"type" yaml:"type"`
	DependsOn  []string `json:"depends_on,omitempty" yaml:"depends_on,omitempty"`
	Retry      int      `json:"retry" yaml:"retry"`
	Idempotent bool     `json:"idempotent" yaml:"idempotent"`
	TimeoutMS  int      `json:"timeout_ms" yaml:"timeout_ms"`
	Resources  []string `json:"resources" yaml:"resources"`
	Sim        SimSpec  `json:"sim" yaml:"sim"`
}
type Definition struct {
	APIVersion string           `json:"api_version" yaml:"api_version"`
	Inputs     map[string]Input `json:"inputs" yaml:"inputs"`
	Tasks      []Task           `json:"tasks" yaml:"tasks"`
}
type PublishInput struct {
	Name            string      `json:"name"`
	Definition      *Definition `json:"definition,omitempty"`
	YAMLSource      string      `json:"yaml_source,omitempty"`
	ExpectedVersion int64       `json:"expected_version,omitempty"`
}
type FlowVersion struct {
	ID          string     `json:"id"`
	FlowID      string     `json:"flow_id"`
	ProjectID   string     `json:"project_id"`
	Name        string     `json:"name"`
	Version     int64      `json:"version"`
	Definition  Definition `json:"definition"`
	YAMLSource  string     `json:"yaml_source"`
	ContentHash string     `json:"content_hash"`
	CreatedAt   time.Time  `json:"created_at"`
}
type Target struct {
	ProvisionInstallationID string                        `json:"provision_installation_id,omitempty"`
	ProvisionRequestID      string                        `json:"provision_request_id,omitempty"`
	ProvisionSnapshotSHA256 string                        `json:"provision_snapshot_sha256,omitempty"`
	Agent                   *taskrunner.Binding           `json:"agent,omitempty"`
	SSHInstallKey           *taskrunner.InstallCredential `json:"ssh_install_key,omitempty"`
	SSHTrustExpiresAt       *time.Time                    `json:"ssh_trust_expires_at,omitempty"`
	SSHTrustRequestID       string                        `json:"ssh_trust_request_id,omitempty"`
	SSH                     *taskrunner.Binding           `json:"ssh,omitempty"`
	ResourceKey             string                        `json:"resource_key,omitempty"`
	ID                      string                        `json:"id"`
	ProjectID               string                        `json:"project_id"`
	Name                    string                        `json:"name"`
	Seed                    int64                         `json:"seed"`
	Revision                int64                         `json:"revision"`
	ModelVersion            string                        `json:"model_version"`
	CreatedAt               time.Time                     `json:"created_at"`
	Mode                    string                        `json:"mode"`
}
type SimTarget = Target

type RegisterInput struct {
	Prefix string `json:"prefix"`
	Count  int    `json:"count"`
	Seed   int64  `json:"seed"`
}
type CreateInput struct {
	FlowVersionID         string         `json:"flow_version_id"`
	TargetIDs             []string       `json:"target_ids"`
	Parameters            map[string]any `json:"parameters"`
	Concurrency           int            `json:"concurrency"`
	CircuitBreakerPercent int            `json:"circuit_breaker_percent"`
	Mode                  string         `json:"mode"`
}
type RerunSource struct {
	ExecutionID   string `json:"execution_id"`
	StateVersion  int64  `json:"state_version"`
	Mode          string `json:"mode"`
	SelectionHash string `json:"selection_hash"`
	ChangeReason  string `json:"change_reason"`
}
type Snapshot struct {
	Catalog       []platform.CatalogSelection `json:"catalog,omitempty"`
	RerunSource   *RerunSource                `json:"rerun_source,omitempty"`
	Mode          string                      `json:"mode"`
	Flow          FlowVersion                 `json:"flow"`
	Targets       []SimTarget                 `json:"targets"`
	Parameters    map[string]any              `json:"parameters"`
	TargetSetHash string                      `json:"target_set_hash"`
	Admission     string                      `json:"admission"`
	FrozenAt      time.Time                   `json:"frozen_at"`
}
type Execution struct {
	ID                    string         `json:"id"`
	ProjectID             string         `json:"project_id"`
	FlowVersionID         string         `json:"flow_version_id"`
	Mode                  string         `json:"mode"`
	State                 string         `json:"state"`
	StateVersion          int64          `json:"state_version"`
	DesiredState          string         `json:"desired_state"`
	Parameters            map[string]any `json:"parameters"`
	Snapshot              Snapshot       `json:"snapshot"`
	TargetSetHash         string         `json:"target_set_hash"`
	Concurrency           int            `json:"concurrency"`
	CircuitBreakerPercent int            `json:"circuit_breaker_percent"`
	CreatedBy             string         `json:"created_by"`
	CreatedAt             time.Time      `json:"created_at"`
	StartedAt             *time.Time     `json:"started_at"`
	FinishedAt            *time.Time     `json:"finished_at"`
}
type Attempt struct {
	Receipt      *Receipt        `json:"receipt"`
	ID           string          `json:"id"`
	ExecutionID  string          `json:"execution_id"`
	TargetID     string          `json:"target_id"`
	TaskID       string          `json:"task_id"`
	TaskIndex    int             `json:"task_index"`
	AttemptNo    int             `json:"attempt_no"`
	CommandID    string          `json:"command_id"`
	State        string          `json:"state"`
	StateVersion int64           `json:"state_version"`
	LeaseOwner   string          `json:"lease_owner"`
	LeaseUntil   *time.Time      `json:"lease_until"`
	FencingToken int64           `json:"fencing_token"`
	Result       json.RawMessage `json:"result"`
	StartedAt    *time.Time      `json:"started_at"`
	FinishedAt   *time.Time      `json:"finished_at"`
}
type ExecutionSummary struct {
	TrustVerification     bool       `json:"trust_verification"`
	ID                    string     `json:"id"`
	ProjectID             string     `json:"project_id"`
	FlowVersionID         string     `json:"flow_version_id"`
	Mode                  string     `json:"mode"`
	State                 string     `json:"state"`
	StateVersion          int64      `json:"state_version"`
	DesiredState          string     `json:"desired_state"`
	TargetSetHash         string     `json:"target_set_hash"`
	TargetCount           int        `json:"target_count"`
	Concurrency           int        `json:"concurrency"`
	CircuitBreakerPercent int        `json:"circuit_breaker_percent"`
	CreatedBy             string     `json:"created_by"`
	CreatedAt             time.Time  `json:"created_at"`
	StartedAt             *time.Time `json:"started_at"`
	FinishedAt            *time.Time `json:"finished_at"`
}

type Detail struct {
	AttemptsTotal int            `json:"attempts_total"`
	Execution     Execution      `json:"execution"`
	Attempts      []Attempt      `json:"attempts"`
	Counts        map[string]int `json:"counts"`
}
type ControlInput struct {
	Action          string `json:"action"`
	ExpectedVersion int64  `json:"expected_version"`
}
type ReconcileInput struct {
	Result          string `json:"result"`
	Evidence        string `json:"evidence"`
	ExpectedVersion int64  `json:"expected_version"`
}
type Claim struct {
	Attempt       Attempt        `json:"attempt"`
	Task          Task           `json:"task"`
	Target        SimTarget      `json:"target"`
	Parameters    map[string]any `json:"parameters"`
	LeaseDuration time.Duration  `json:"-"`
}
type Receipt struct {
	CommandID    string         `json:"command_id"`
	AttemptID    string         `json:"attempt_id"`
	FencingToken int64          `json:"fencing_token"`
	Result       string         `json:"result"`
	Output       map[string]any `json:"output"`
	StartedAt    time.Time      `json:"started_at"`
	FinishedAt   time.Time      `json:"finished_at"`
}
type Event struct {
	Sequence         int64           `json:"sequence"`
	ID               string          `json:"id"`
	ExecutionID      string          `json:"execution_id"`
	AggregateVersion int64           `json:"aggregate_version"`
	Type             string          `json:"type"`
	Payload          json.RawMessage `json:"payload"`
	CreatedAt        time.Time       `json:"created_at"`
}

var identifier = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
var inputIdentifier = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,118}$`)
var opaqueID = regexp.MustCompile(`^[a-f0-9]{32}$`)

func failure(status int, code, message string) error {
	return &platform.Error{Status: status, Code: code, Message: message}
}
func invalid(message string) error  { return failure(400, "VALIDATION_ERROR", message) }
func conflict(message string) error { return failure(409, "STATE_CONFLICT", message) }
func hash(v []byte) string          { h := sha256.Sum256(v); return hex.EncodeToString(h[:]) }
func jsonBytes(v any) []byte        { b, _ := json.Marshal(v); return b }
func terminal(v string) bool {
	return v == "success" || v == "failed" || v == "killed" || v == "warning"
}
func (in PublishInput) Parse() (Definition, error) {
	var d Definition
	if (in.Definition == nil) == (strings.TrimSpace(in.YAMLSource) == "") {
		return d, invalid("provide exactly one definition or yaml_source")
	}
	if in.Definition != nil {
		d = *in.Definition
	} else {
		if len(in.YAMLSource) > 49152 {
			return d, invalid("YAML source is too large")
		}
		var node yaml.Node
		if yaml.Unmarshal([]byte(in.YAMLSource), &node) != nil {
			return d, invalid("invalid workflow YAML")
		}
		if !safeYAML(&node, 0) {
			return d, invalid("YAML aliases or excessive depth are unsupported")
		}
		dec := yaml.NewDecoder(strings.NewReader(in.YAMLSource))
		dec.KnownFields(true)
		if e := dec.Decode(&d); e != nil {
			return d, invalid("invalid or unsupported workflow YAML")
		}
		var extra any
		if dec.Decode(&extra) != io.EOF {
			return d, invalid("one YAML document is required")
		}
	}
	return d, d.Validate()
}
func (d Definition) Validate() error {
	if d.APIVersion != "keel.sim/v1" {
		return invalid("only keel.sim/v1 workflows are supported")
	}
	if len(d.Tasks) < 1 || len(d.Tasks) > 32 || len(d.Inputs) > 32 {
		return invalid("require 1..32 tasks and at most 32 inputs")
	}
	for k, in := range d.Inputs {
		if !inputIdentifier.MatchString(k) {
			return invalid("invalid input key")
		}
		if in.Type != "string" && in.Type != "number" && in.Type != "boolean" && in.Type != "enum" {
			return invalid("inputs support public scalar values only; secrets are not accepted")
		}
		for _, part := range []string{"password", "secret", "token", "credential", "private_key"} {
			if strings.Contains(strings.ToLower(k), part) {
				return invalid("secret inputs are not permitted in simulation snapshots")
			}
		}
		if in.Type == "enum" && (len(in.Enum) == 0 || len(in.Enum) > 64) {
			return invalid("enum requires 1..64 choices")
		}
	}
	ids := map[string]Task{}
	for _, t := range d.Tasks {
		if !identifier.MatchString(t.ID) {
			return invalid("invalid task identifier")
		}
		if _, ok := ids[t.ID]; ok {
			return invalid("duplicate task identifier")
		}
		ids[t.ID] = t
		if t.Type != "sim.read" && t.Type != "sim.write" {
			return invalid("only safe sim.read and sim.write task adapters are enabled")
		}
		if t.Retry < 0 || t.Retry > 3 || (t.Retry > 0 && !t.Idempotent) {
			return invalid("retry must be 0..3 and requires idempotent=true")
		}
		if t.TimeoutMS < 50 || t.TimeoutMS > 60000 || t.Sim.DurationMS < 0 || t.Sim.DurationMS > 60000 {
			return invalid("task duration/timeout outside simulation bounds")
		}
		if t.Sim.Outcome != "success" && t.Sim.Outcome != "failed" && t.Sim.Outcome != "drop_receipt" {
			return invalid("unsupported simulated outcome")
		}
		if t.Sim.FailFirst < 0 || t.Sim.FailFirst > 4 {
			return invalid("fail_first must be 0..4")
		}
		seen := map[string]bool{}
		if len(t.Resources) == 0 || len(t.Resources) > 3 {
			return invalid("task must declare machine/gpu/nic resources")
		}
		for _, r := range t.Resources {
			if r != "machine" && r != "gpu" && r != "nic" || seen[r] {
				return invalid("invalid or duplicate resource")
			}
			seen[r] = true
		}
		if !seen["machine"] {
			return invalid("all tasks must lock the simulated machine resource")
		}
	}
	visiting := map[string]bool{}
	done := map[string]bool{}
	var visit func(string) error
	visit = func(id string) error {
		if visiting[id] {
			return invalid("task dependency cycle")
		}
		if done[id] {
			return nil
		}
		visiting[id] = true
		deps := map[string]bool{}
		for _, dep := range ids[id].DependsOn {
			if _, ok := ids[dep]; !ok || deps[dep] {
				return invalid("missing or duplicate dependency")
			}
			deps[dep] = true
			if e := visit(dep); e != nil {
				return e
			}
		}
		visiting[id] = false
		done[id] = true
		return nil
	}
	for id := range ids {
		if e := visit(id); e != nil {
			return e
		}
	}
	_, e := d.Parameters(map[string]any{}, false)
	return e
}
func (d Definition) Parameters(given map[string]any, require bool) (map[string]any, error) {
	out := map[string]any{}
	for k := range given {
		if _, ok := d.Inputs[k]; !ok {
			return nil, invalid("unknown parameter")
		}
	}
	for k, in := range d.Inputs {
		v, ok := given[k]
		if !ok {
			v = in.Default
		}
		if v == nil {
			if require && in.Required {
				return nil, invalid("required input missing: " + k)
			}
			continue
		}
		valid := false
		switch in.Type {
		case "string":
			s, ok := v.(string)
			valid = ok && len(s) <= 4096
		case "enum":
			s, ok := v.(string)
			if ok {
				for _, choice := range in.Enum {
					valid = valid || s == choice
				}
			}
		case "number":
			switch v.(type) {
			case float64, float32, int, int64, int32, json.Number:
				valid = true
			}
		case "boolean":
			_, valid = v.(bool)
		}
		if !valid {
			return nil, invalid("invalid parameter: " + k)
		}
		out[k] = v
	}
	if len(jsonBytes(out)) > 16384 {
		return nil, invalid("parameter snapshot exceeds 16 KiB")
	}
	return out, nil
}

// authorize locks user and session before project/member/domain rows. This matches
// platform account revocation lock order and pins authority until the write commits.
func authorize(ctx context.Context, tx *sql.Tx, a *Actor, projectID string, roles ...string) error {
	if a == nil {
		return platform.ErrUnauthenticated
	}
	if e := platform.RecheckSessionTx(ctx, tx, a); e != nil {
		return e
	}
	admin := a.User.IsAdmin
	var e error
	var state string
	e = tx.QueryRowContext(ctx, "SELECT state FROM projects WHERE id=$1 FOR SHARE", projectID).Scan(&state)
	if errors.Is(e, sql.ErrNoRows) {
		return failure(404, "NOT_FOUND", "Project not found")
	}
	if e != nil {
		return e
	}
	if state == "archived" || state == "delivered" {
		return conflict("project lifecycle does not permit execution changes")
	}
	if admin {
		return nil
	}
	var role string
	e = tx.QueryRowContext(ctx, "SELECT role FROM memberships WHERE project_id=$1 AND user_id=$2 FOR SHARE", projectID, a.User.ID).Scan(&role)
	if errors.Is(e, sql.ErrNoRows) {
		return failure(404, "NOT_FOUND", "Project not found")
	}
	if e != nil {
		return e
	}
	for _, allowed := range roles {
		if allowed == role {
			return nil
		}
	}
	return platform.ErrForbidden
}
func advisory(ctx context.Context, tx *sql.Tx, key string) error {
	_, e := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", key)
	return e
}
func idempotency(ctx context.Context, tx *sql.Tx, a *Actor, scope, key string, input any) (string, []byte, error) {
	if len(key) < 1 || len(key) > 128 || strings.TrimSpace(key) != key {
		return "", nil, invalid("Idempotency-Key is required (1..128 characters)")
	}
	for _, r := range key {
		if r < 33 || r > 126 {
			return "", nil, invalid("invalid Idempotency-Key")
		}
	}
	id := hash([]byte(a.User.ID + ":" + scope + ":" + key))
	if e := advisory(ctx, tx, id); e != nil {
		return "", nil, e
	}
	var priorHash string
	var prior []byte
	e := tx.QueryRowContext(ctx, "SELECT request_hash,response FROM exec_mutations WHERE id=$1", id).Scan(&priorHash, &prior)
	if e == nil {
		if priorHash != hash(jsonBytes(input)) {
			return "", nil, failure(409, "IDEMPOTENCY_CONFLICT", "key was used with different content")
		}
		return id, prior, nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return "", nil, e
	}
	return id, nil, nil
}
func remember(ctx context.Context, tx *sql.Tx, id string, input, response any) error {
	_, e := tx.ExecContext(ctx, "INSERT INTO exec_mutations VALUES($1,$2,$3,now())", id, hash(jsonBytes(input)), jsonBytes(response))
	return e
}
func audit(ctx context.Context, tx *sql.Tx, actor, projectID, action, target, trace string, metadata any) error {
	_, e := tx.ExecContext(ctx, "INSERT INTO audits(id,actor_id,project_id,action,target_id,trace_id,metadata,created_at) VALUES($1,NULLIF($2,''),$3,$4,$5,$6,$7,now())", platform.RandomID(), actor, projectID, action, target, trace, jsonBytes(metadata))
	return e
}
func event(ctx context.Context, tx *sql.Tx, eid, kind string, payload any) error {
	_, e := tx.ExecContext(ctx, "INSERT INTO exec_outbox(id,execution_id,aggregate_version,event_type,payload,created_at) SELECT $1,id,state_version,$2,$3,now() FROM exec_executions WHERE id=$4", platform.RandomID(), kind, jsonBytes(payload), eid)
	return e
}
func decode[T any](raw []byte) (T, error) { var out T; e := json.Unmarshal(raw, &out); return out, e }
func sortedUnique(ids []string) ([]string, error) {
	out := append([]string{}, ids...)
	sort.Strings(out)
	for i, id := range out {
		if !opaqueID.MatchString(id) || (i > 0 && id == out[i-1]) {
			return nil, invalid("invalid or duplicate target ID")
		}
	}
	return out, nil
}
func taskByID(d Definition, id string) (Task, error) {
	for _, t := range d.Tasks {
		if t.ID == id {
			return t, nil
		}
	}
	return Task{}, fmt.Errorf("immutable task definition missing")
}
func sameJSON(a, b []byte) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	return bytes.Equal(jsonBytes(x), jsonBytes(y))
}

func safeYAML(n *yaml.Node, depth int) bool {
	if depth > 16 || n.Kind == yaml.AliasNode {
		return false
	}
	for _, child := range n.Content {
		if !safeYAML(child, depth+1) {
			return false
		}
	}
	return true
}
