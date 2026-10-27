package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/enzoOvOyu/keel/internal/monitor"
)

type MonitorPolicyRevision struct {
	ID        string         `json:"id"`
	Version   int64          `json:"version"`
	Policy    monitor.Policy `json:"policy"`
	CreatedBy string         `json:"created_by"`
	CreatedAt time.Time      `json:"created_at"`
	Reason    string         `json:"reason"`
}
type MonitorPolicyInput struct {
	ExpectedVersion int64          `json:"expected_version"`
	Policy          monitor.Policy `json:"policy"`
	Reason          string         `json:"reason"`
}
type MonitorPolicies struct {
	Enabled                    bool                    `json:"enabled"`
	Items                      []MonitorPolicyRevision `json:"items"`
	ActiveDevices              int                     `json:"active_devices"`
	PayloadShareBytesPerSecond int                     `json:"payload_share_bytes_per_second"`
	GlobalBytesPerSecond       int                     `json:"global_bytes_per_second"`
}

func monitorStage(state string) string {
	switch state {
	case "collected", "lldp", "mapped":
		return "M2"
	case "configured":
		return "M3"
	case "netverified", "tested":
		return "M4"
	case "maintained":
		return "maintained"
	default:
		return "M1"
	}
}
func monitorPolicyTx(ctx context.Context, tx *sql.Tx, project, stage string) (*MonitorPolicyRevision, error) {
	out := &MonitorPolicyRevision{ID: "default-" + stage + "-v1", Policy: monitor.DefaultPolicy(stage)}
	var raw []byte
	e := tx.QueryRowContext(ctx, `SELECT id,version,policy,created_by,created_at,reason FROM monitor_policies WHERE project_id=$1 AND stage=$2 ORDER BY version DESC LIMIT 1`, project, stage).Scan(&out.ID, &out.Version, &raw, &out.CreatedBy, &out.CreatedAt, &out.Reason)
	if errors.Is(e, sql.ErrNoRows) {
		return out, nil
	}
	if e != nil {
		return nil, e
	}
	if e = json.Unmarshal(raw, &out.Policy); e != nil {
		return nil, e
	}
	return out, nil
}
func monitorDeviceCountTx(ctx context.Context, tx *sql.Tx) (int, error) {
	var count int
	e := tx.QueryRowContext(ctx, `SELECT count(*) FROM project_assets a JOIN projects p ON p.id=a.project_id WHERE a.state<>'maintained' AND p.state NOT IN('archived','delivered')`).Scan(&count)
	if count == 0 {
		count = 1
	}
	return count, e
}
func (s *Store) MonitorPolicies(ctx context.Context, a *AuthSession, project string) (*MonitorPolicies, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireProjectMember(ctx, tx, a, project); e != nil {
		return nil, e
	}
	out := &MonitorPolicies{Enabled: s.MonitorEnabled, Items: []MonitorPolicyRevision{}, GlobalBytesPerSecond: monitor.GlobalBytesPerSecond}
	out.ActiveDevices, e = monitorDeviceCountTx(ctx, tx)
	if e != nil {
		return nil, e
	}
	out.PayloadShareBytesPerSecond, e = monitor.Allocate(out.ActiveDevices)
	if e != nil {
		return nil, apiError(409, "MONITOR_BUDGET_EXCEEDED", "Monitoring admission exceeds the global payload reservation")
	}
	for _, stage := range []string{"M1", "M2", "M3", "M4", "maintained"} {
		p, e := monitorPolicyTx(ctx, tx, project, stage)
		if e != nil {
			return nil, e
		}
		out.Items = append(out.Items, *p)
	}
	return out, tx.Commit()
}
func (s *Store) SaveMonitorPolicy(ctx context.Context, a *AuthSession, project string, in MonitorPolicyInput, key, trace string) (*MonitorPolicyRevision, error) {
	in.Reason = strings.TrimSpace(in.Reason)
	if in.Policy.Validate() != nil || in.ExpectedVersion < 0 || utf8.RuneCountInString(in.Reason) < 3 || utf8.RuneCountInString(in.Reason) > 1000 || strings.TrimSpace(key) == "" || len(key) > 128 {
		return nil, apiError(400, "VALIDATION_ERROR", "Valid stage policy, current version, reason and Idempotency-Key required")
	}
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireProjectMember(ctx, tx, a, project); e != nil {
		return nil, e
	}
	role, e := issueRoleTx(ctx, tx, a, project)
	if e != nil {
		return nil, e
	}
	if role != "Admin" {
		return nil, ErrForbidden
	}
	var state string
	if e = tx.QueryRowContext(ctx, `SELECT state FROM projects WHERE id=$1 FOR SHARE`, project).Scan(&state); e != nil {
		return nil, e
	}
	if state == "archived" || state == "delivered" {
		return nil, apiError(409, "PROJECT_READ_ONLY", "Project is read-only")
	}
	if e = lockKey(ctx, tx, "monitor-policy:"+project+":"+in.Policy.Stage); e != nil {
		return nil, e
	}
	identity := digest("monitor.policy:" + project + ":" + a.User.ID + ":" + key)
	if e = lockKey(ctx, tx, "monitor-policy-request:"+identity); e != nil {
		return nil, e
	}
	hash := digest(string(networkJSON(in)))
	prior, e := priorWiringHandshakeTx(ctx, tx, identity, hash)
	if e != nil {
		return nil, e
	}
	if prior != "" {
		var out MonitorPolicyRevision
		var raw []byte
		e = tx.QueryRowContext(ctx, `SELECT id,version,policy,created_by,created_at,reason FROM monitor_policies WHERE id=$1 AND project_id=$2`, prior, project).Scan(&out.ID, &out.Version, &raw, &out.CreatedBy, &out.CreatedAt, &out.Reason)
		if e != nil {
			return nil, e
		}
		if e = json.Unmarshal(raw, &out.Policy); e != nil {
			return nil, e
		}
		return &out, tx.Commit()
	}
	current, e := monitorPolicyTx(ctx, tx, project, in.Policy.Stage)
	if e != nil {
		return nil, e
	}
	if current.Version != in.ExpectedVersion {
		return nil, errVersion
	}
	count, e := monitorDeviceCountTx(ctx, tx)
	if e != nil {
		return nil, e
	}
	if _, e = monitor.Allocate(count); e != nil {
		return nil, apiError(409, "MONITOR_BUDGET_EXCEEDED", "Monitoring admission exceeds the global payload reservation")
	}
	out := &MonitorPolicyRevision{ID: RandomID(), Version: current.Version + 1, Policy: in.Policy, CreatedBy: a.User.ID, CreatedAt: time.Now().UTC(), Reason: in.Reason}
	if _, e = tx.ExecContext(ctx, `INSERT INTO monitor_policies(id,project_id,stage,version,policy,created_by,reason,created_at)VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, out.ID, project, in.Policy.Stage, out.Version, networkJSON(out.Policy), out.CreatedBy, out.Reason, out.CreatedAt); e != nil {
		return nil, e
	}
	if e = appendAudit(ctx, tx, a.User.ID, project, "monitor.policy.publish", out.ID, trace, map[string]any{"stage": in.Policy.Stage, "version": out.Version, "reason": in.Reason}); e != nil {
		return nil, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO idempotencies(id,actor_id,request_hash,response,created_at)VALUES($1,$2,$3,$4,clock_timestamp())`, identity, a.User.ID, hash, networkJSON(map[string]string{"id": out.ID})); e != nil {
		return nil, e
	}
	return out, tx.Commit()
}
func (s *Store) monitorAssignmentTx(ctx context.Context, tx *sql.Tx, credential *AgentCredential, connection string) (*monitor.Assignment, error) {
	var state, projectState string
	if e := tx.QueryRowContext(ctx, `SELECT a.state,p.state FROM project_assets a JOIN projects p ON p.id=a.project_id WHERE a.project_id=$1 AND a.machine_id=$2`, credential.ProjectID, credential.MachineID).Scan(&state, &projectState); e != nil {
		return nil, e
	}
	stage := monitorStage(state)
	if projectState == "archived" || projectState == "delivered" {
		stage = "maintained"
	}
	var windowExecution string
	if stage != "maintained" {
		e := tx.QueryRowContext(ctx, `SELECT execution.id FROM exec_executions execution JOIN exec_execution_targets selected ON selected.execution_id=execution.id JOIN exec_targets target ON target.id=selected.target_id JOIN exec_flow_versions flow ON flow.id=execution.flow_version_id WHERE execution.project_id=$1 AND execution.mode IN('agent','ssh') AND execution.state='running' AND execution.desired_state='running' AND execution.finished_at IS NULL AND target.project_id=$1 AND target.kind='machine' AND target.resource_key=$2 AND selected.snapshot->>'resource_key'=$2 AND execution.parameters->>'monitor_profile' IN('G5','G6') AND flow.definition#>>'{inputs,monitor_profile,type}'='string' AND execution.parameters->>'monitor_profile' IN(SELECT jsonb_array_elements_text(flow.definition#>'{inputs,monitor_profile,enum}')) ORDER BY execution.started_at DESC,execution.id LIMIT 1`, credential.ProjectID, credential.MachineID).Scan(&windowExecution)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return nil, e
		}
		if windowExecution != "" {
			stage = "M4"
		}
	}
	revision, e := monitorPolicyTx(ctx, tx, credential.ProjectID, stage)
	if e != nil {
		return nil, e
	}
	count, e := monitorDeviceCountTx(ctx, tx)
	if e != nil {
		return nil, e
	}
	share, e := monitor.Allocate(count)
	if e != nil {
		return nil, apiError(429, "MONITOR_BUDGET_EXCEEDED", "Global monitoring payload admission exhausted")
	}
	if share > revision.Policy.SteadyBytesPerSecond {
		share = revision.Policy.SteadyBytesPerSecond
	}
	assignment := &monitor.Assignment{BasePolicyID: revision.ID, ValidUntil: time.Now().UTC().Add(30 * time.Second), Version: monitor.Version, PolicyID: revision.ID, Policy: revision.Policy, PayloadBytesPerSecond: share, BurstBytes: monitor.MaxFrameBytes, InitialJitterMillis: int(monitor.Jitter(credential.MachineID, 10*time.Second) / time.Millisecond)}
	if windowExecution != "" {
		assignment.ExecutionID = windowExecution
		assignment.WindowUntil = &assignment.ValidUntil
		assignment.Policy.Intervals["D1"] = 10
		assignment.Policy.Intervals["D2"] = 10
		assignment.Policy.Intervals["D5"] = 15
		assignment.PolicyID = "window-" + digest(revision.ID + ":" + windowExecution)[:56]
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO monitor_assignment_versions(credential_id,policy_id,assignment,created_at)VALUES($1,$2,$3,clock_timestamp())ON CONFLICT DO NOTHING`, credential.ID, assignment.PolicyID, networkJSON(assignment)); e != nil {
		return nil, e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO monitor_assignments(credential_id,project_id,machine_id,connection_id,assignment,assigned_at)VALUES($1,$2,$3,$4,$5,clock_timestamp()) ON CONFLICT(credential_id)DO UPDATE SET connection_id=EXCLUDED.connection_id,assignment=EXCLUDED.assignment,assigned_at=EXCLUDED.assigned_at`, credential.ID, credential.ProjectID, credential.MachineID, connection, networkJSON(assignment))
	return assignment, e
}
