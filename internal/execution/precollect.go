package execution

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/enzoOvOyu/keel/internal/precollect"
	"github.com/enzoOvOyu/keel/internal/taskrunner"
)

type PrecollectCreateTarget struct {
	AssetID           string `json:"asset_id"`
	ExpectedScopeHash string `json:"expected_scope_hash"`
}
type PrecollectCreateInput struct {
	Targets []PrecollectCreateTarget `json:"targets"`
	Reason  string                   `json:"reason"`
}

// Domain-owned allowlisted SSH work. A general SSH/Agent/Flow API cannot select
// host.precollect without this BOM/model/trust scope and retained evidence chain.
func (s *Service) CreatePrecollect(ctx context.Context, a *Actor, project string, in PrecollectCreateInput, key, trace string) (*Execution, error) {
	if s.MediaStore == nil || !s.MediaStore.PrecollectEnabled || s.SSHRunner == nil {
		return nil, failure(409, "PRECOLLECT_DISABLED", "Read-only precollection is disabled")
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if len(in.Targets) < 1 || len(in.Targets) > 200 || len(in.Reason) < 3 || len(in.Reason) > 1000 {
		return nil, invalid("One to 200 exact previewed targets and a reason required")
	}
	sort.Slice(in.Targets, func(i, j int) bool { return in.Targets[i].AssetID < in.Targets[j].AssetID })
	for i, t := range in.Targets {
		if !opaqueID.MatchString(t.AssetID) || len(t.ExpectedScopeHash) != 64 || i > 0 && t.AssetID == in.Targets[i-1].AssetID {
			return nil, invalid("Unique asset IDs with exact scope hashes required")
		}
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
	idem, prior, e := idempotency(ctx, tx, a, "validation.precollect:"+project, key, in)
	if e != nil {
		return nil, e
	}
	if prior != nil {
		out, e := decode[Execution](prior)
		return &out, e
	}
	targets := []Target{}
	scopes := []*platform.PrecollectScope{}
	for _, input := range in.Targets {
		scope, e := s.MediaStore.PreparePrecollectScopeTx(ctx, tx, project, input.AssetID, nil, true)
		if e != nil {
			return nil, e
		}
		if !scope.Ready {
			return nil, failure(409, scope.Code, "Precollection requires the complete current BOM/model/BMC/SSH scope")
		}
		if scope.ScopeHash != input.ExpectedScopeHash {
			return nil, failure(409, "PRECOLLECT_SCOPE_CHANGED", "Preview scope changed before admission")
		}
		target, e := s.sshTargetForPrecollectTx(ctx, tx, project, scope.MachineID, true)
		if e != nil {
			return nil, e
		}
		if target.SSH == nil || target.SSH.ConfigHash != scope.SSHBindingHash || target.SSH.HostKey != scope.SSHHostKey {
			return nil, conflict("Precollection SSH binding differs from preview")
		}
		target.Precollect = scope
		if e = tx.QueryRowContext(ctx, `INSERT INTO exec_targets(id,project_id,kind,resource_key) VALUES($1,$2,'machine',$3) ON CONFLICT(project_id,kind,resource_key) DO UPDATE SET resource_key=excluded.resource_key RETURNING id`, platform.RandomID(), project, scope.MachineID).Scan(&target.ID); e != nil {
			return nil, e
		}
		targets = append(targets, target)
		scopes = append(scopes, scope)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	task := Task{ID: "precollect", Type: "host.precollect", Idempotent: true, Retry: 0, TimeoutMS: 30000, Resources: []string{"device", "ssh"}}
	def := Definition{APIVersion: "keel.precollect/v1", Inputs: map[string]Input{}, Tasks: []Task{task}}
	flow := FlowVersion{ID: platform.RandomID(), FlowID: platform.RandomID(), ProjectID: project, Name: "Precollect " + platform.RandomID()[:8], Version: 1, Definition: def, ContentHash: hash(jsonBytes(def)), CreatedAt: now}
	if _, e = tx.ExecContext(ctx, `INSERT INTO exec_flows(id,project_id,name,created_by,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$5)`, flow.FlowID, project, flow.Name, a.User.ID, now); e != nil {
		return nil, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO exec_flow_versions(id,flow_id,project_id,version,definition,content_hash,created_by,created_at) VALUES($1,$2,$3,1,$4,$5,$6,$7)`, flow.ID, flow.FlowID, project, jsonBytes(def), flow.ContentHash, a.User.ID, now); e != nil {
		return nil, e
	}
	params := map[string]any{"validation_kind": "precollect", "rule_version": precollect.RuleVersion, "reason": in.Reason}
	out := &Execution{ID: platform.RandomID(), ProjectID: project, FlowVersionID: flow.ID, Mode: "ssh", State: "created", StateVersion: 1, DesiredState: "running", Parameters: params, TargetSetHash: hash(jsonBytes(scopes)), Concurrency: min(4, len(targets)), CircuitBreakerPercent: 100, CreatedBy: a.User.ID, CreatedAt: now}
	out.Snapshot = Snapshot{Mode: "ssh", Flow: flow, Targets: targets, Parameters: params, TargetSetHash: out.TargetSetHash, FrozenAt: now, Admission: "Read-only allowlisted host precollection; exact BOM/model/physical identities/BMC proof and pinned SSH; no interface up, LLDP start, GPUrail/RoCE configuration or mapping publication. G1 is evaluated separately only from retained stopped original evidence."}
	if _, e = tx.ExecContext(ctx, `INSERT INTO exec_executions(id,project_id,flow_version_id,mode,parameters,snapshot,target_set_hash,concurrency,circuit_breaker_percent,created_by,created_at) VALUES($1,$2,$3,'ssh',$4,$5,$6,$7,100,$8,$9)`, out.ID, project, flow.ID, jsonBytes(params), jsonBytes(out.Snapshot), out.TargetSetHash, out.Concurrency, a.User.ID, now); e != nil {
		return nil, e
	}
	for _, target := range targets {
		scope := target.Precollect
		if _, e = tx.ExecContext(ctx, `INSERT INTO exec_execution_targets(execution_id,target_id,snapshot) VALUES($1,$2,$3)`, out.ID, target.ID, jsonBytes(target)); e != nil {
			return nil, e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO exec_task_attempts(id,execution_id,target_id,task_id,task_index,attempt_no,command_id,runner,created_at) VALUES($1,$2,$3,'precollect',0,1,$4,'ssh',$5)`, platform.RandomID(), out.ID, target.ID, platform.RandomID(), now); e != nil {
			return nil, e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO validation_precollect_requests(id,project_id,asset_id,machine_id,execution_id,scope_hash,scope,created_by,reason,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, platform.RandomID(), project, scope.AssetID, scope.MachineID, out.ID, scope.ScopeHash, jsonBytes(scope), a.User.ID, in.Reason, now); e != nil {
			return nil, e
		}
	}
	if e = event(ctx, tx, out.ID, "execution.created", map[string]any{"mode": "ssh", "operation": "host.precollect", "target_count": len(targets)}); e != nil {
		return nil, e
	}
	if e = audit(ctx, tx, a.User.ID, project, "validation.precollect.request", out.ID, trace, map[string]any{"scope_hash": out.TargetSetHash, "target_count": len(targets), "reason": in.Reason}); e != nil {
		return nil, e
	}
	if e = remember(ctx, tx, idem, in, out); e != nil {
		return nil, e
	}
	return out, tx.Commit()
}
func (s *Service) precollectFrozenTx(ctx context.Context, tx *sql.Tx, t Target) error {
	if s.MediaStore == nil || !s.MediaStore.PrecollectEnabled || t.Precollect == nil {
		return failure(409, "PRECOLLECT_DISABLED", "Precollection adapter is unavailable")
	}
	current, e := s.MediaStore.PreparePrecollectScopeTx(ctx, tx, t.ProjectID, t.Precollect.AssetID, t.Precollect, true)
	if e != nil {
		return e
	}
	if !current.Ready || current.ScopeHash != t.Precollect.ScopeHash {
		return failure(409, "PRECOLLECT_SCOPE_CHANGED", "Frozen BOM/model/physical/BMC facts changed or expired")
	}
	target, e := s.sshTargetForPrecollectTx(ctx, tx, t.ProjectID, t.ResourceKey, true)
	if e != nil {
		return e
	}
	if t.SSH == nil || target.SSH == nil || *t.SSH != *target.SSH || t.Revision != target.Revision || t.ModelVersion != target.ModelVersion {
		return conflict("Frozen precollection SSH or asset identity changed")
	}
	return nil
}
func (s *Service) recordPrecollectObservation(ctx context.Context, o taskrunner.Observation) error {
	if s.MediaStore == nil || s.SSHRunner == nil || o.Validate(o.Command) != nil {
		return taskrunner.ErrRejected
	}
	output := map[string]any{"operation": "host.precollect", "mode": "ssh", "transport": "ssh", "source": "host", "hardware_evidence": false, "stopped": true, "observation": o.Output}
	if o.State == "success" {
		id, sha, e := s.MediaStore.PrecollectProofForCommand(ctx, o.Command)
		if errors.Is(e, sql.ErrNoRows) {
			var meta struct {
				Size   int64  `json:"precollect_size"`
				SHA256 string `json:"precollect_sha256"`
			}
			if json.Unmarshal(jsonBytes(o.Output), &meta) != nil || meta.Size < 1 || meta.Size > precollect.MaxBundleBytes {
				return taskrunner.ErrRejected
			}
			raw := make([]byte, 0, meta.Size)
			for offset := int64(0); offset < meta.Size; offset += precollect.ChunkBytes {
				chunk, e := s.SSHRunner.Do(ctx, taskrunner.Request{Action: "read", Command: o.Command, ChunkOffset: offset, ChunkSHA256: meta.SHA256})
				if e != nil {
					return e
				}
				if chunk.Command != o.Command || chunk.State != "accepted" {
					return taskrunner.ErrRejected
				}
				var part struct {
					Offset      int64  `json:"precollect_chunk_offset"`
					Size        int64  `json:"precollect_size"`
					SHA256      string `json:"precollect_sha256"`
					ChunkSHA256 string `json:"chunk_sha256"`
					Chunk       string `json:"chunk"`
				}
				if json.Unmarshal(jsonBytes(chunk.Output), &part) != nil {
					return taskrunner.ErrRejected
				}
				body, e := base64.StdEncoding.Strict().DecodeString(part.Chunk)
				if e != nil || int64(len(body)) != min(int64(precollect.ChunkBytes), meta.Size-offset) || part.Offset != offset || part.Size != meta.Size || part.SHA256 != meta.SHA256 || precollect.Hash(body) != part.ChunkSHA256 {
					return taskrunner.ErrRejected
				}
				raw = append(raw, body...)
			}
			if precollect.Hash(raw) != meta.SHA256 {
				return taskrunner.ErrRejected
			}
			id, sha, e = s.MediaStore.RecordPrecollect(ctx, o, raw)
		}
		if e != nil {
			return e
		}
		output["precollect_record_id"] = id
		output["evidence_hash"] = sha
	}
	return s.RecordReceipt(ctx, Receipt{CommandID: o.Command.CommandID, AttemptID: o.Command.AttemptID, FencingToken: o.Command.FencingToken, Result: o.State, StartedAt: o.StartedAt, FinishedAt: o.FinishedAt, Output: output})
}
func (s *Service) precollectReceiptAllowedTx(ctx context.Context, tx *sql.Tx, r Receipt) error {
	var domain bool
	e := tx.QueryRowContext(ctx, `SELECT coalesce(x.parameters->>'validation_kind'='precollect',false) FROM exec_executions x JOIN exec_task_attempts a ON a.execution_id=x.id WHERE a.id=$1`, r.AttemptID).Scan(&domain)
	if e != nil {
		return e
	}
	if !domain {
		return nil
	}
	if r.Output["stopped"] != true {
		return conflict("Precollection receipt lacks stopped proof")
	}
	if r.Result != "success" {
		return nil
	}
	var valid bool
	e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM validation_precollect_records WHERE id=$1 AND attempt_id=$2 AND command_id=$3 AND fencing_token=$4 AND raw_sha256=$5)`, r.Output["precollect_record_id"], r.AttemptID, r.CommandID, r.FencingToken, r.Output["evidence_hash"]).Scan(&valid)
	if e != nil {
		return e
	}
	if !valid {
		return conflict("Successful precollection requires its original immutable raw evidence")
	}
	return nil
}

// Reading the original stopped artifact may release an unknown attempt even
// after its model/BOM/BMC proof expires. Requiring fresh capture prerequisites
// here would deadlock recovery. Connection identity and current trust must
// still match; this path can only query/read the original dispatched command.
func (s *Service) precollectRecoveryBindingTx(ctx context.Context, tx *sql.Tx, t Target) error {
	if s.SSHRunner == nil || t.SSH == nil || t.Precollect == nil {
		return taskrunner.ErrRejected
	}
	current, e := s.SSHRunner.Binding(t.ProjectID, t.ResourceKey)
	if e != nil || current != *t.SSH {
		return taskrunner.ErrRejected
	}
	var trusted bool
	e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM machines m JOIN project_assets a ON a.machine_id=m.id WHERE a.project_id=$1 AND m.id=$2 AND m.ssh_trust='established' AND m.ssh_trust_source='ssh' AND m.host_key_fingerprint=$3)`, t.ProjectID, t.ResourceKey, t.SSH.HostKey).Scan(&trusted)
	if e != nil {
		return e
	}
	if !trusted {
		return failure(409, "SSH_TRUST_REQUIRED", "Original pinned trusted SSH binding required for receipt recovery")
	}
	return platform.EnforceSSHDomainBinding(ctx, tx, s.SSHRunner, t.ProjectID, t.ResourceKey)
}
