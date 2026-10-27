package platform

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/enzoOvOyu/keel/internal/precollect"
	"github.com/enzoOvOyu/keel/internal/taskrunner"
)

type PrecollectRecord struct {
	ID             string                  `json:"id"`
	ProjectID      string                  `json:"project_id"`
	AssetID        string                  `json:"asset_id"`
	MachineID      string                  `json:"machine_id"`
	ExecutionID    string                  `json:"execution_id"`
	ExecutionState string                  `json:"execution_state"`
	Scope          *PrecollectScope        `json:"scope,omitempty"`
	CreatedAt      time.Time               `json:"created_at"`
	Origin         string                  `json:"origin"`
	CollectedAt    *time.Time              `json:"collected_at"`
	RawSHA256      string                  `json:"raw_sha256"`
	RawSize        int64                   `json:"raw_size"`
	Report         *precollect.Report      `json:"report,omitempty"`
	Gate           *ValidationGateEvidence `json:"gate,omitempty"`
}
type ValidationGateEvidence struct {
	EffectiveVerdict    string                `json:"effective_verdict"`
	WaiverID            string                `json:"waiver_id"`
	EffectiveValidUntil *time.Time            `json:"effective_valid_until"`
	ID                  string                `json:"id"`
	Gate                string                `json:"gate"`
	PrecollectID        string                `json:"precollect_id"`
	ScopeHash           string                `json:"scope_hash"`
	RevisionFingerprint string                `json:"revision_fingerprint"`
	Verdict             string                `json:"verdict"`
	Validity            string                `json:"validity"`
	InvalidationReason  string                `json:"invalidation_reason"`
	RuleVersion         string                `json:"rule_version"`
	ObservedAt          time.Time             `json:"observed_at"`
	ValidUntil          time.Time             `json:"valid_until"`
	Evaluation          precollect.Evaluation `json:"evaluation"`
}

func (s *Store) PrecollectProofForCommand(ctx context.Context, command taskrunner.TaskCommand) (string, string, error) {
	var id, hash string
	e := s.DB.QueryRowContext(ctx, `SELECT r.id,r.raw_sha256 FROM validation_precollect_records r JOIN validation_precollect_requests q ON q.id=r.id WHERE r.attempt_id=$1 AND r.command_id=$2 AND r.fencing_token=$3 AND q.project_id=$4 AND q.machine_id=$5`, command.AttemptID, command.CommandID, command.FencingToken, command.ProjectID, command.MachineID).Scan(&id, &hash)
	return id, hash, e
}

// Only the authenticated original SSH observation reaches this worker consumer.
// There is deliberately no HTTP evidence upload or arbitrary report endpoint.
func (s *Store) RecordPrecollect(ctx context.Context, o taskrunner.Observation, raw []byte) (string, string, error) {
	if s.MediaObjects == nil || o.Command.Operation != "host.precollect" || o.State != "success" || o.Validate(o.Command) != nil {
		return "", "", apiError(409, "PRECOLLECT_DISABLED", "Precollection evidence adapter is unavailable")
	}
	bundle, e := precollect.Decode(raw, time.Now())
	if e != nil {
		return "", "", e
	}
	var metadata struct {
		Size int64 `json:"precollect_size"`
	}
	if json.Unmarshal(networkJSON(o.Output), &metadata) != nil || metadata.Size != int64(len(raw)) {
		return "", "", apiError(409, "PRECOLLECT_EVIDENCE_MISMATCH", "Original raw size differs from stopped observation")
	}
	sha := precollect.Hash(raw)
	if o.Output["precollect_sha256"] != sha || bundle.StartedAt.Before(o.StartedAt.Add(-5*time.Second)) || bundle.CollectedAt.After(o.FinishedAt.Add(5*time.Second)) {
		return "", "", apiError(409, "PRECOLLECT_EVIDENCE_MISMATCH", "Original stopped observation and complete raw bundle required")
	}
	report, e := precollect.Normalize(bundle)
	if e != nil {
		return "", "", e
	}
	var id string
	var scopeJSON []byte
	var created time.Time
	e = s.DB.QueryRowContext(ctx, `SELECT q.id,q.scope,q.created_at FROM validation_precollect_requests q JOIN exec_task_attempts a ON a.execution_id=q.execution_id JOIN exec_targets t ON t.id=a.target_id AND t.resource_key=q.machine_id WHERE a.id=$1 AND a.command_id=$2 AND a.fencing_token=$3 AND q.project_id=$4 AND q.machine_id=$5`, o.Command.AttemptID, o.Command.CommandID, o.Command.FencingToken, o.Command.ProjectID, o.Command.MachineID).Scan(&id, &scopeJSON, &created)
	if e != nil {
		return "", "", e
	}
	if bundle.StartedAt.Before(created.Add(-5 * time.Second)) {
		return "", "", apiError(409, "PRECOLLECT_EVIDENCE_OLD", "Capture predates its original request")
	}
	var scope PrecollectScope
	if e = json.Unmarshal(scopeJSON, &scope); e != nil {
		return "", "", e
	}
	key := "validation/precollect/" + scope.ProjectID + "/" + scope.AssetID + "/" + id + "/" + sha + ".json"
	version, e := s.MediaObjects.Put(ctx, key, bytes.NewReader(raw), int64(len(raw)))
	if e != nil || version == "" || version == "null" {
		return "", "", apiError(409, "PRECOLLECT_RAW_UNAVAILABLE", "Versioned precollection raw evidence is unavailable")
	}
	if e = s.verifyPrecollectRaw(ctx, key, version, sha, int64(len(raw))); e != nil {
		return "", "", e
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return "", "", e
	}
	defer tx.Rollback()
	if e = lockKey(ctx, tx, "precollect-record:"+id); e != nil {
		return "", "", e
	}
	var old string
	e = tx.QueryRowContext(ctx, `SELECT raw_sha256 FROM validation_precollect_records WHERE id=$1`, id).Scan(&old)
	if e == nil {
		if old != sha {
			return "", "", apiError(409, "PRECOLLECT_EVIDENCE_CONFLICT", "Original command evidence is immutable")
		}
		return id, sha, nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return "", "", e
	}
	var valid bool
	e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM exec_task_attempts a JOIN exec_resource_leases device ON device.attempt_id=a.id AND device.fencing_token=a.fencing_token AND device.resource_id='device:machine:'||$4 AND device.state='held' AND device.expires_at>clock_timestamp() JOIN exec_resource_leases ssh ON ssh.attempt_id=a.id AND ssh.fencing_token=a.fencing_token AND ssh.resource_id='ssh:machine:'||$4 AND ssh.state='held' AND ssh.expires_at>clock_timestamp() WHERE a.id=$1 AND a.command_id=$2 AND a.fencing_token=$3 AND a.state IN('running','unknown') AND (a.state='unknown' OR a.lease_until>clock_timestamp()))`, o.Command.AttemptID, o.Command.CommandID, o.Command.FencingToken, o.Command.MachineID).Scan(&valid)
	// Explicit unknown reconciliation may retain the original stopped proof after
	// its leases expired; it never recollects and does not declare G1 at this point.
	if e != nil {
		return "", "", e
	}
	if !valid {
		var unknown bool
		e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM exec_task_attempts WHERE id=$1 AND command_id=$2 AND fencing_token=$3 AND state='unknown')`, o.Command.AttemptID, o.Command.CommandID, o.Command.FencingToken).Scan(&unknown)
		if e != nil {
			return "", "", e
		}
		if !unknown {
			return "", "", apiError(409, "PRECOLLECT_FENCE_LOST", "Original fenced attempt no longer owns capture authority")
		}
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO validation_precollect_records(id,attempt_id,command_id,fencing_token,origin,collected_at,raw_key,raw_version,raw_sha256,raw_size,report,received_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,statement_timestamp())`, id, o.Command.AttemptID, o.Command.CommandID, o.Command.FencingToken, bundle.Origin, bundle.CollectedAt, key, version, sha, len(raw), networkJSON(report))
	if e != nil {
		return "", "", e
	}
	evaluation := precollect.Evaluate(report, scope.Expectation, scope.BMC != nil && scope.BMC.Origin == "real" && scope.BMC.Address != "", time.Now(), time.Duration(scope.GateTTLSeconds)*time.Second)
	seenPorts := map[string]bool{}
	for _, p := range evaluation.Ports {
		if seenPorts[p.PhysicalPortID] {
			continue
		}
		seenPorts[p.PhysicalPortID] = true
		_, e = tx.ExecContext(ctx, `INSERT INTO validation_precollect_ports(record_id,physical_port_id,component_id,observation) VALUES($1,$2,$3,$4)`, id, p.PhysicalPortID, p.ComponentID, networkJSON(p))
		if e != nil {
			return "", "", e
		}
	}
	return id, sha, tx.Commit()
}
func (s *Store) verifyPrecollectRaw(ctx context.Context, key, version, sha string, size int64) error {
	if s.MediaObjects == nil {
		return apiError(409, "PRECOLLECT_RAW_UNAVAILABLE", "Retained raw evidence storage is unavailable")
	}
	reader, e := s.MediaObjects.Open(ctx, key, version)
	if e != nil {
		return apiError(409, "PRECOLLECT_RAW_UNAVAILABLE", "Retained raw version is unavailable")
	}
	defer reader.Close()
	raw, e := io.ReadAll(io.LimitReader(reader, precollect.MaxBundleBytes+1))
	if e != nil || int64(len(raw)) != size || precollect.Hash(raw) != sha {
		return apiError(409, "PRECOLLECT_RAW_CORRUPT", "Retained raw version differs from its immutable digest")
	}
	return nil
}
func (s *Store) ReconcilePrecollect(ctx context.Context, limit int) (int, error) {
	if !s.PrecollectEnabled {
		return 0, nil
	}
	if limit < 1 || limit > 20 {
		limit = 10
	}
	rows, e := s.DB.QueryContext(ctx, `SELECT r.id FROM validation_precollect_records r JOIN validation_precollect_requests q ON q.id=r.id JOIN exec_executions x ON x.id=q.execution_id JOIN exec_task_attempts a ON a.id=r.attempt_id AND a.state='success' JOIN exec_runner_receipts receipt ON receipt.attempt_id=a.id AND receipt.command_id=r.command_id AND receipt.fencing_token=r.fencing_token AND receipt.result=a.state WHERE x.state IN('success','failed') AND x.desired_state='running' AND receipt.payload#>>'{output,precollect_record_id}'=r.id AND receipt.payload#>>'{output,evidence_hash}'=r.raw_sha256 AND receipt.payload#>>'{output,stopped}'='true' AND NOT EXISTS(SELECT 1 FROM validation_gate_evidence g WHERE g.precollect_id=r.id) ORDER BY r.received_at,r.id LIMIT $1`, limit)
	if e != nil {
		return 0, e
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return 0, e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return 0, e
	}
	n := 0
	for _, id := range ids {
		done, e := s.finalizePrecollect(ctx, id)
		if e != nil {
			return n, e
		}
		if done {
			n++
		}
	}
	return n, nil
}
func (s *Store) finalizePrecollect(ctx context.Context, id string) (bool, error) {
	var key, version, sha string
	var size int64
	var pinnedScope []byte
	e := s.DB.QueryRowContext(ctx, `SELECT r.raw_key,r.raw_version,r.raw_sha256,r.raw_size,q.scope FROM validation_precollect_records r JOIN validation_precollect_requests q ON q.id=r.id WHERE r.id=$1`, id).Scan(&key, &version, &sha, &size, &pinnedScope)
	if e != nil {
		return false, e
	}
	rawError := s.verifyPrecollectRaw(ctx, key, version, sha, size)
	var source PrecollectScope
	if e = json.Unmarshal(pinnedScope, &source); e != nil {
		return false, e
	}
	if rawError == nil {
		rawError = s.verifyPrecollectBMC(ctx, source.BMC)
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return false, e
	}
	defer tx.Rollback()
	if e = lockKey(ctx, tx, "keel-admin-initialization"); e != nil {
		return false, e
	}
	var exists bool
	if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM validation_gate_evidence WHERE precollect_id=$1)`, id).Scan(&exists); e != nil {
		return false, e
	}
	if exists {
		return false, nil
	}
	var project, asset, machine, creator, receiptDigest string
	var scopeJSON, reportJSON []byte
	e = tx.QueryRowContext(ctx, `SELECT q.project_id,q.asset_id,q.machine_id,q.created_by,q.scope,r.report,receipt.digest FROM validation_precollect_requests q JOIN validation_precollect_records r ON r.id=q.id JOIN exec_executions x ON x.id=q.execution_id JOIN exec_task_attempts a ON a.id=r.attempt_id JOIN exec_runner_receipts receipt ON receipt.attempt_id=a.id AND receipt.command_id=r.command_id AND receipt.fencing_token=r.fencing_token WHERE q.id=$1 AND x.state IN('success','failed') AND a.state='success' AND receipt.result=a.state AND x.desired_state='running' AND receipt.payload#>>'{output,precollect_record_id}'=r.id AND receipt.payload#>>'{output,evidence_hash}'=r.raw_sha256 AND receipt.payload#>>'{output,stopped}'='true'`, id).Scan(&project, &asset, &machine, &creator, &scopeJSON, &reportJSON, &receiptDigest)
	if errors.Is(e, sql.ErrNoRows) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	var scope PrecollectScope
	var report precollect.Report
	if e = json.Unmarshal(scopeJSON, &scope); e != nil {
		return false, e
	}
	if e = json.Unmarshal(reportJSON, &report); e != nil {
		return false, e
	}
	current, currentError := s.PreparePrecollectScopeTx(ctx, tx, project, asset, &scope, true)
	now := time.Now().UTC()
	evaluation := precollect.Evaluate(&report, scope.Expectation, scope.BMC != nil && scope.BMC.Origin == "real" && scope.BMC.Address != "", now, time.Duration(scope.GateTTLSeconds)*time.Second)
	block := func(code string) {
		evaluation.Verdict = "BLOCKED"
		evaluation.Findings = append(evaluation.Findings, precollect.Finding{Code: code, Field: "scope", Verdict: "BLOCKED"})
	}
	if rawError != nil {
		block("PRECOLLECT_RAW_UNAVAILABLE")
	}
	if currentError != nil {
		var domain *Error
		if !errors.As(currentError, &domain) {
			return false, currentError
		}
	}
	if currentError != nil || current == nil || !current.Ready || current.ScopeHash != scope.ScopeHash {
		block("PRECOLLECT_SCOPE_CHANGED")
	}
	var authority bool
	e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users u JOIN projects p ON p.id=$2 WHERE u.id=$1 AND u.enabled AND p.state NOT IN('archived','delivered') AND (u.is_admin OR EXISTS(SELECT 1 FROM memberships m WHERE m.project_id=p.id AND m.user_id=u.id AND m.role IN('Admin','DE','DL','SRE'))))`, creator, project).Scan(&authority)
	if e != nil {
		return false, e
	}
	if !authority {
		block("PRECOLLECT_AUTHORITY_CHANGED")
	}
	until := report.CollectedAt.Add(time.Duration(scope.GateTTLSeconds) * time.Second)
	if evaluation.Verdict == "PASS" && scope.BMC != nil {
		expiry := scope.BMC.CollectedAt.Add(time.Duration(scope.AssetTTLSeconds) * time.Second)
		if expiry.Before(until) {
			until = expiry
		}
	}
	fingerprint := precollect.Hash(networkJSON(map[string]any{"scope_hash": scope.ScopeHash, "raw_sha256": sha, "receipt_digest": receiptDigest, "rule_version": precollect.RuleVersion, "observed_at": report.CollectedAt, "valid_until": until}))
	gateID := RandomID()
	_, e = tx.ExecContext(ctx, `INSERT INTO validation_gate_evidence(id,project_id,asset_id,machine_id,gate,precollect_id,scope_hash,revision_fingerprint,rule_version,verdict,evaluation,observed_at,valid_until,created_at) VALUES($1,$2,$3,$4,'G1',$5,$6,$7,$8,$9,$10,$11,$12,statement_timestamp())`, gateID, project, asset, machine, id, scope.ScopeHash, fingerprint, precollect.RuleVersion, evaluation.Verdict, networkJSON(evaluation), report.CollectedAt, until)
	if e != nil {
		return false, e
	}
	if evaluation.Verdict == "PASS" {
		_, e = tx.ExecContext(ctx, `UPDATE project_assets SET state='collected',updated_at=statement_timestamp() WHERE id=$1 AND project_id=$2 AND version=$3 AND state IN('provisioned','trust_established')`, asset, project, scope.AssetVersion)
		if e != nil {
			return false, e
		}
	}
	if e = appendAudit(ctx, tx, creator, project, "validation.g1.evaluate", gateID, "precollect:"+id, map[string]any{"verdict": evaluation.Verdict, "scope_hash": scope.ScopeHash, "source_record": id, "fingerprint": fingerprint}); e != nil {
		return false, e
	}
	return true, tx.Commit()
}

// The BMC serial and address used by G1 must still have their pinned original
// bytes. Other missing L2/L3 features retain the BMC collection's own quality;
// verifying these identity sources does not promote that collection to complete.
func (s *Store) verifyPrecollectBMC(ctx context.Context, proof *PrecollectBMCProof) error {
	if proof == nil {
		return apiError(409, "PRECOLLECT_BMC_EVIDENCE_REQUIRED", "Pinned BMC source required")
	}
	rows, e := s.DB.QueryContext(ctx, `SELECT raw_key,raw_version,raw_sha256,raw_size,resource_type FROM bmc_collection_resources WHERE collection_id=$1 AND (resource_type LIKE '#ComputerSystem.%' OR (resource_type LIKE '#EthernetInterface.%' AND resource_path LIKE '/redfish/v1/Managers/%')) ORDER BY resource_path LIMIT 129`, proof.ID)
	if e != nil {
		return e
	}
	type reference struct {
		key, version, sha, kind string
		size                    int64
	}
	refs := []reference{}
	for rows.Next() {
		var r reference
		if e = rows.Scan(&r.key, &r.version, &r.sha, &r.size, &r.kind); e != nil {
			rows.Close()
			return e
		}
		refs = append(refs, r)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	if len(refs) > 128 {
		return apiError(409, "PRECOLLECT_BMC_EVIDENCE_LIMIT", "BMC identity source exceeds bounded evidence scope")
	}
	system, manager := false, false
	for _, r := range refs {
		if e = s.verifyPrecollectRaw(ctx, r.key, r.version, r.sha, r.size); e != nil {
			return e
		}
		if strings.HasPrefix(r.kind, "#ComputerSystem.") {
			system = true
		} else {
			manager = true
		}
	}
	if !system || !manager {
		return apiError(409, "PRECOLLECT_BMC_EVIDENCE_REQUIRED", "Retained BMC serial and address resources required")
	}
	return nil
}
