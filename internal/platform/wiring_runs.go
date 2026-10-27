package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/enzoOvOyu/keel/internal/wiring"
)

// WiringRunInput selects an existing Step 1 batch. A new handshake authorizes
// admission only; every host receives another handshake at actual dispatch.
type WiringRunInput struct {
	BatchID     string `json:"batch_id"`
	HandshakeID string `json:"handshake_id"`
	FactsHash   string `json:"facts_hash"`
	Reason      string `json:"reason"`
}
type WiringRunScope struct {
	PolicyHash     string             `json:"policy_hash"`
	GateTTLSeconds int64              `json:"gate_ttl_seconds"`
	ProjectID      string             `json:"project_id"`
	BatchID        string             `json:"batch_id"`
	AssetID        string             `json:"asset_id"`
	MachineID      string             `json:"machine_id"`
	ParentID       string             `json:"parent_id"`
	TopologyID     string             `json:"topology_id"`
	IdentityHash   string             `json:"identity_hash"`
	Expectation    wiring.Expectation `json:"expectation"`
}

// Fresh sample IDs, their deadlines and NP gate display strings are not physical
// identities. Keep G1/topology/model/BOM, selected ports, management, Golden and
// collector configuration anchored while permitting a new collection sample.
func wiringIdentityHash(f *WiringFacts) string {
	type device struct {
		ID, Serial, ManagementHash, GoldenHash string
		Ports                                  []string
	}
	devices := make([]device, 0, len(f.Switches))
	for _, d := range f.Switches {
		devices = append(devices, device{d.ID, d.Serial, d.ManagementHash, d.GoldenHash, d.Ports})
	}
	return digest(string(networkJSON(struct {
		Project, Package, Collector string
		Hosts                       []WiringHost
		Devices                     []device
	}{f.ProjectID, f.NetworkPackageID, f.CollectorHash, f.Targets, devices})))
}
func wiringHostFacts(f *WiringFacts, asset string) *WiringFacts {
	out := *f
	out.Targets = nil
	out.Switches = nil
	wanted := map[string][]string{}
	for _, h := range f.Targets {
		if h.AssetID == asset {
			out.Targets = append(out.Targets, h)
			for _, p := range h.Ports {
				wanted[p.SwitchID] = append(wanted[p.SwitchID], p.SwitchPort)
			}
		}
	}
	for _, d := range f.Switches {
		if ports, ok := wanted[d.ID]; ok {
			sort.Strings(ports)
			d.Ports = ports
			out.Switches = append(out.Switches, d)
		}
	}
	return &out
}
func (s *Store) PrepareWiringRunTx(ctx context.Context, tx *sql.Tx, a *AuthSession, project string, in WiringRunInput) ([]WiringRunScope, error) {
	if !s.WiringEnabled {
		return nil, apiError(409, "WIRING_DISABLED", "Wiring execution is disabled")
	}
	if !validEntityID(in.BatchID) || !validEntityID(in.HandshakeID) || len(in.FactsHash) != 64 || utf8.RuneCountInString(strings.TrimSpace(in.Reason)) < 3 || utf8.RuneCountInString(in.Reason) > 1000 {
		return nil, apiError(400, "VALIDATION_ERROR", "Existing batch, fresh handshake, facts hash and reason required")
	}
	if e := wiringScopePermissionTx(ctx, tx, a, project, true); e != nil {
		return nil, e
	}
	batch, e := s.wiringBatchTx(ctx, tx, project, in.BatchID)
	if e != nil {
		return nil, e
	}
	h, e := s.wiringHandshakeTx(ctx, tx, project, in.HandshakeID, true)
	if e != nil {
		return nil, e
	}
	if !h.Ready || h.Facts == nil || h.Facts.FactsHash != in.FactsHash || len(h.Facts.Targets) == 0 || len(h.Facts.Targets) > len(batch.Targets) {
		return nil, apiError(409, "WIRING_SCOPE_CHANGED", "Admission requires a fresh handshake for explicit frozen batch hosts")
	}
	current, e := s.wiringFactsTx(ctx, tx, project, wiringTargetsFromFacts(h.Facts), true)
	if e != nil {
		return nil, e
	}
	if !current.Ready || current.FactsHash != in.FactsHash || !h.ValidUntil.After(time.Now()) {
		return nil, apiError(409, "WIRING_SCOPE_CHANGED", "Readiness changed during admission")
	}
	duration, policy, e := wiringReportPolicyTx(ctx, tx, project)
	if e != nil {
		return nil, e
	}
	scopes := make([]WiringRunScope, 0, len(batch.Targets))
	for _, t := range batch.Targets {
		host := wiringHostFacts(current, t.AssetID)
		if len(host.Targets) == 0 {
			continue
		}
		expected, err := WiringExpectation(host, t.AssetID)
		if err != nil {
			return nil, err
		}
		expected.ScopeHash = t.Expectation.ScopeHash
		if string(networkJSON(expected)) != string(networkJSON(t.Expectation)) {
			return nil, apiError(409, "WIRING_SCOPE_CHANGED", "Physical identities differ from Step 1")
		}
		var parent string
		for _, r := range t.Reports {
			if r.Step == 1 {
				if r.EffectiveVerdict != "PASS" {
					return nil, apiError(409, "WIRING_STEP1_REQUIRED", "Current passing Step 1 required")
				}
				parent = r.ID
			}
		}
		if parent == "" {
			return nil, apiError(409, "WIRING_STEP1_REQUIRED", "Current passing Step 1 required")
		}
		scopes = append(scopes, WiringRunScope{ProjectID: project, BatchID: in.BatchID, AssetID: t.AssetID, MachineID: t.MachineID, ParentID: parent, TopologyID: t.TopologyID, IdentityHash: wiringIdentityHash(host), Expectation: t.Expectation, PolicyHash: policy, GateTTLSeconds: int64(duration / time.Second)})
	}
	if len(scopes) != len(current.Targets) {
		return nil, apiError(409, "WIRING_SCOPE_CHANGED", "Selected hosts must belong to the existing Step 1 batch")
	}
	return scopes, nil
}
func (s *Store) AttachWiringRunTx(ctx context.Context, tx *sql.Tx, a *AuthSession, execution string, in WiringRunInput, scopes []WiringRunScope) error {
	if len(scopes) == 0 || len(scopes) > 200 {
		return apiError(400, "VALIDATION_ERROR", "Bounded explicit wiring scope required")
	}
	if _, e := tx.ExecContext(ctx, `INSERT INTO validation_wiring_runs(execution_id,project_id,batch_id,handshake_id,created_by,reason) VALUES($1,$2,$3,$4,$5,$6)`, execution, scopes[0].ProjectID, in.BatchID, in.HandshakeID, a.User.ID, strings.TrimSpace(in.Reason)); e != nil {
		return e
	}
	for _, scope := range scopes {
		if _, e := tx.ExecContext(ctx, `INSERT INTO validation_wiring_run_targets(execution_id,asset_id,machine_id,scope) VALUES($1,$2,$3,$4)`, execution, scope.AssetID, scope.MachineID, networkJSON(scope)); e != nil {
			return e
		}
	}
	return nil
}
func loadWiringRunScopeTx(ctx context.Context, tx *sql.Tx, execution, machine string) (*WiringRunScope, error) {
	var raw []byte
	e := tx.QueryRowContext(ctx, `SELECT scope FROM validation_wiring_run_targets WHERE execution_id=$1 AND machine_id=$2`, execution, machine).Scan(&raw)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if e != nil {
		return nil, e
	}
	var out WiringRunScope
	if json.Unmarshal(raw, &out) != nil || out.Expectation.Validate() != nil || out.MachineID != machine {
		return nil, wiring.ErrCaptureRejected
	}
	return &out, nil
}
func wiringRunResources(scope *WiringRunScope) []string {
	set := map[string]bool{"device:machine:" + scope.MachineID: true, "ssh:machine:" + scope.MachineID: true}
	for _, p := range scope.Expectation.Ports {
		set["device:switch:"+p.SwitchID] = true
		set["ssh:switch:"+p.SwitchID] = true
	}
	out := make([]string, 0, len(set))
	for r := range set {
		out = append(out, r)
	}
	sort.Strings(out)
	return out
}
func (s *Store) WiringRunResourcesTx(ctx context.Context, tx *sql.Tx, execution, machine string) ([]string, error) {
	if !s.WiringEnabled {
		return nil, apiError(409, "WIRING_DISABLED", "Wiring execution is disabled")
	}
	scope, e := loadWiringRunScopeTx(ctx, tx, execution, machine)
	if e != nil {
		return nil, e
	}
	return wiringRunResources(scope), nil
}

// Authorize against the accepted execution owner, not a fabricated UI session.
// Logout does not revoke a job; account disable, role removal, project closure,
// cancellation or loss of any original exclusive lease prevents dispatch.
func (s *Store) wiringDispatchFactsTx(ctx context.Context, tx *sql.Tx, b wiring.ReadbackBinding, machine string) (*WiringRunScope, *WiringFacts, string, error) {
	scope, e := loadWiringRunScopeTx(ctx, tx, b.ExecutionID, machine)
	if e != nil {
		return nil, nil, "", e
	}
	var owner string
	var allowed bool
	e = tx.QueryRowContext(ctx, `SELECT x.created_by, u.enabled AND (u.is_admin OR EXISTS(SELECT 1 FROM memberships m WHERE m.project_id=x.project_id AND m.user_id=u.id AND m.role IN('Admin','DE','DL','NE','SRE'))) AND p.state NOT IN('archived','delivered') AND x.desired_state='running' AND x.mode='fabric' AND x.parameters->>'validation_kind'='wiring' AND a.state='running' AND a.lease_until>clock_timestamp() AND keel_wiring_exclusive_scope(a.id,a.fencing_token,$6,$7,true) FROM exec_executions x JOIN users u ON u.id=x.created_by JOIN projects p ON p.id=x.project_id JOIN exec_task_attempts a ON a.execution_id=x.id JOIN exec_targets t ON t.id=a.target_id WHERE x.id=$1 AND x.project_id=$2 AND a.id=$3 AND a.command_id=$4 AND a.fencing_token=$5 AND t.kind='machine' AND t.resource_key=$6`, b.ExecutionID, b.ProjectID, b.AttemptID, b.CommandID, b.FencingToken, machine, networkJSON(scope.Expectation.Ports)).Scan(&owner, &allowed)
	if e != nil {
		return nil, nil, "", e
	}
	if !allowed {
		return nil, nil, "", ErrForbidden
	}
	batch, e := s.wiringBatchTx(ctx, tx, b.ProjectID, scope.BatchID)
	if e != nil {
		return nil, nil, "", e
	}
	currentParent := ""
	for _, t := range batch.Targets {
		if t.AssetID == scope.AssetID {
			for _, r := range t.Reports {
				if r.Step == 1 && r.EffectiveVerdict == "PASS" {
					currentParent = r.ID
				}
			}
		}
	}
	if currentParent != scope.ParentID {
		return nil, nil, "", apiError(409, "WIRING_STEP1_REQUIRED", "The original passing Step 1 is no longer current")
	}
	ports := make([]string, 0, len(scope.Expectation.Ports))
	for _, p := range scope.Expectation.Ports {
		ports = append(ports, p.PhysicalPortID)
	}
	facts, e := s.wiringFactsTx(ctx, tx, b.ProjectID, []WiringTargetInput{{AssetID: scope.AssetID, PhysicalPortIDs: ports}}, true)
	if e != nil {
		return nil, nil, "", e
	}
	if !facts.Ready || wiringIdentityHash(facts) != scope.IdentityHash {
		return nil, nil, "", apiError(409, "WIRING_SCOPE_CHANGED", "Current dispatch identities or network prerequisites differ from admission")
	}
	duration, policy, e := wiringReportPolicyTx(ctx, tx, b.ProjectID)
	if e != nil {
		return nil, nil, "", e
	}
	if policy != scope.PolicyHash || int64(duration/time.Second) != scope.GateTTLSeconds {
		return nil, nil, "", apiError(409, "WIRING_POLICY_CHANGED", "Gate evidence policy changed after admission")
	}
	return scope, facts, owner, nil
}

// RegisterWiringRunCommand refreshes readiness outside DB locks and registers
// the exact original command atomically with its new one-host handshake.
func (s *Store) RegisterWiringRunCommand(ctx context.Context, b wiring.ReadbackBinding, machine, hostOrigin string) (*WiringCommand, error) {
	if !s.WiringEnabled || s.FabricProbes == nil {
		return nil, apiError(409, "WIRING_DISABLED", "Wiring execution is disabled")
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	_, before, _, e := s.wiringDispatchFactsTx(ctx, tx, b, machine)
	tx.Rollback()
	if e != nil {
		return nil, e
	}
	started := time.Now().UTC()
	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	collector := s.FabricProbes.CheckCollector(probeCtx)
	cancel()
	if collector.State != "ready" {
		return nil, apiError(409, "WIRING_COLLECTOR_OFFLINE", "Collector did not prove ready before dispatch")
	}
	tx, e = s.DB.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", "keel-admin-initialization"); e != nil {
		return nil, e
	}
	if e = lockKey(ctx, tx, "keel-admin-initialization"); e != nil {
		return nil, e
	}
	scope, current, owner, e := s.wiringDispatchFactsTx(ctx, tx, b, machine)
	if e != nil {
		return nil, e
	}
	if current.FactsHash != before.FactsHash {
		return nil, apiError(409, "WIRING_SCOPE_CHANGED", "Readiness changed during collector probe")
	}
	deadline := started.Add(time.Duration(current.TTLSeconds) * time.Second)
	if current.EvidenceValidUntil.Before(deadline) {
		deadline = current.EvidenceValidUntil
	}
	if !deadline.After(time.Now()) {
		return nil, apiError(409, "WIRING_HANDSHAKE_EXPIRED", "Dispatch readiness expired")
	}
	handshake := RandomID()
	_, e = tx.ExecContext(ctx, `INSERT INTO validation_wiring_handshakes(id,project_id,facts_hash,facts,collector,readiness_at,valid_until,created_by,reason,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'Execution dispatch readiness refresh',clock_timestamp())`, handshake, b.ProjectID, current.FactsHash, networkJSON(current), networkJSON(collector), started, deadline, owner)
	if e != nil {
		return nil, e
	}
	command, e := s.RegisterWiringCommandTx(ctx, tx, scope.BatchID, scope.AssetID, handshake, hostOrigin, b)
	if e != nil {
		return nil, e
	}
	if e = appendAudit(ctx, tx, owner, b.ProjectID, "wiring.command.register", b.CommandID, "", map[string]any{"execution_id": b.ExecutionID, "asset_id": scope.AssetID, "handshake_id": handshake, "origin": b.Origin, "host_origin": hostOrigin}); e != nil {
		return nil, e
	}
	return command, tx.Commit()
}
