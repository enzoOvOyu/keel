package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/enzoOvOyu/keel/internal/fabricboot"
	"github.com/enzoOvOyu/keel/internal/netbaseline"
	"github.com/enzoOvOyu/keel/internal/wiring"
)

// WiringCommand is registered by the shared Execution owner after it holds the
// entire host/switch resource set. Registration commits before Driver.Begin.
// It consumes a NEW one-host readiness handshake; the earlier Step 1 lease is
// historical evidence and cannot authorize later hardware work.
type WiringCommand struct {
	HostOrigin  string                 `json:"host_origin"`
	BatchID     string                 `json:"batch_id"`
	AssetID     string                 `json:"asset_id"`
	HandshakeID string                 `json:"handshake_id"`
	ParentID    string                 `json:"parent_id"`
	Binding     wiring.ReadbackBinding `json:"binding"`
	Expectation wiring.Expectation     `json:"expectation"`
	Plan        wiring.Plan            `json:"plan"`
}

func (s *Store) RegisterWiringCommandTx(ctx context.Context, tx *sql.Tx, batch, asset, handshake, hostOrigin string, binding wiring.ReadbackBinding) (*WiringCommand, error) {
	if hostOrigin != "real" && hostOrigin != "sim" || !s.WiringEnabled || !validEntityID(batch) || !validEntityID(asset) || !validEntityID(handshake) {
		return nil, apiError(409, "WIRING_DISABLED", "A frozen wiring target and fresh readiness handshake are required")
	}
	var exists bool
	if e := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM validation_wiring_commands WHERE command_id=$1)`, binding.CommandID).Scan(&exists); e != nil {
		return nil, e
	}
	if exists {
		return nil, wiring.ErrOutcomeUnknown
	} // Never authorize a second Begin.
	var expectedRaw []byte
	var topology string
	out := &WiringCommand{HostOrigin: hostOrigin, BatchID: batch, AssetID: asset, HandshakeID: handshake, Binding: binding}
	e := tx.QueryRowContext(ctx, `SELECT t.expectation,t.topology_id,r.id FROM validation_wiring_batch_targets t JOIN validation_wiring_step_reports r ON r.batch_id=t.batch_id AND r.asset_id=t.asset_id AND r.step=1 AND r.verdict='PASS' WHERE t.batch_id=$1 AND t.asset_id=$2 AND t.project_id=$3 AND NOT EXISTS(SELECT 1 FROM validation_wiring_step_reports n WHERE n.batch_id=r.batch_id AND n.asset_id=r.asset_id AND n.step=1 AND n.version>r.version)`, batch, asset, binding.ProjectID).Scan(&expectedRaw, &topology, &out.ParentID)
	if e != nil {
		return nil, e
	}
	if e = json.Unmarshal(expectedRaw, &out.Expectation); e != nil || out.Expectation.Validate() != nil {
		return nil, wiring.ErrCaptureRejected
	}
	lease, e := s.wiringHandshakeTx(ctx, tx, binding.ProjectID, handshake, true)
	if e != nil {
		return nil, e
	}
	if !lease.Ready || lease.Collector.State != "ready" || lease.Facts == nil || len(lease.Facts.Targets) != 1 || lease.Facts.Targets[0].AssetID != asset || lease.Facts.Targets[0].TopologyID != topology {
		return nil, apiError(409, "WIRING_HANDSHAKE_NOT_CURRENT", "Dispatch requires a fresh exact target handshake and the original passing Step 1")
	}
	locked, e := s.wiringFactsTx(ctx, tx, binding.ProjectID, wiringTargetsFromFacts(lease.Facts), true)
	if e != nil {
		return nil, e
	}
	if !locked.Ready || locked.FactsHash != lease.Facts.FactsHash || !lease.ValidUntil.After(time.Now()) {
		return nil, apiError(409, "WIRING_SCOPE_CHANGED", "Readiness changed while acquiring the frozen target")
	}
	current, e := WiringExpectation(lease.Facts, asset)
	if e != nil {
		return nil, e
	}
	// Dynamic network collection/Golden freshness belongs to this new handshake.
	// Physical identities and the Step 1 topology must still match the old batch.
	current.ScopeHash = out.Expectation.ScopeHash
	if string(networkJSON(current)) != string(networkJSON(out.Expectation)) {
		return nil, apiError(409, "WIRING_SCOPE_CHANGED", "Frozen physical identities changed; start a new Step 1 batch")
	}
	plan, e := wiring.BuildPlan(out.Expectation, binding.CommandID)
	if e != nil {
		return nil, e
	}
	if _, e = wiring.BuildReadbackRequests(binding, wiring.ReadbackScope{PlanDigest: plan.Digest, CommandID: binding.CommandID, ScopeHash: out.Expectation.ScopeHash, AdvertisingAt: time.Now().UTC(), Ports: out.Expectation.Ports}); e != nil {
		return nil, e
	}
	out.Plan = *plan
	_, e = tx.ExecContext(ctx, `INSERT INTO validation_wiring_commands(command_id,attempt_id,execution_id,project_id,batch_id,asset_id,handshake_id,parent_id,fencing_token,origin,plan_digest,command,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,statement_timestamp())`, binding.CommandID, binding.AttemptID, binding.ExecutionID, binding.ProjectID, batch, asset, handshake, out.ParentID, binding.FencingToken, binding.Origin, plan.Digest, networkJSON(out))
	if e != nil {
		return nil, e
	}
	return out, nil
}

// WiringReadbackLedger is a persistence adapter, not another task engine. Its
// transactions end before mTLS calls. The shared attempt remains the sole owner
// of authorization, exclusive leases, cancellation and unknown reconciliation.
type WiringReadbackLedger struct{ Store *Store }

var _ wiring.ReadbackLedger = (*WiringReadbackLedger)(nil)

func loadWiringCommandTx(ctx context.Context, tx *sql.Tx, binding wiring.ReadbackBinding) (*WiringCommand, error) {
	var raw []byte
	e := tx.QueryRowContext(ctx, `SELECT command FROM validation_wiring_commands WHERE command_id=$1 AND attempt_id=$2 AND execution_id=$3 AND project_id=$4 AND fencing_token=$5 AND origin=$6`, binding.CommandID, binding.AttemptID, binding.ExecutionID, binding.ProjectID, binding.FencingToken, binding.Origin).Scan(&raw)
	if e != nil {
		return nil, e
	}
	var out WiringCommand
	if json.Unmarshal(raw, &out) != nil || out.HostOrigin != "real" && out.HostOrigin != "sim" || string(networkJSON(out.Binding)) != string(networkJSON(binding)) || out.Expectation.Validate() != nil {
		return nil, wiring.ErrCaptureRejected
	}
	plan, e := wiring.BuildPlan(out.Expectation, binding.CommandID)
	if e != nil || string(networkJSON(plan)) != string(networkJSON(out.Plan)) {
		return nil, wiring.ErrCaptureRejected
	}
	return &out, nil
}
func (l *WiringReadbackLedger) Begin(ctx context.Context, binding wiring.ReadbackBinding, request fabricboot.CollectorRecollectRequest) (wiring.ReadbackDecision, error) {
	var decision wiring.ReadbackDecision
	if l == nil || l.Store == nil || !l.Store.WiringEnabled || request.Validate() != nil {
		return decision, wiring.ErrCaptureRejected
	}
	tx, e := l.Store.DB.BeginTx(ctx, nil)
	if e != nil {
		return decision, e
	}
	defer tx.Rollback()
	if e = lockKey(ctx, tx, "wiring-command:"+binding.CommandID); e != nil {
		return decision, e
	}
	command, e := loadWiringCommandTx(ctx, tx, binding)
	if e != nil {
		return decision, e
	}
	requests, e := wiring.BuildReadbackRequests(binding, wiring.ReadbackScope{PlanDigest: command.Plan.Digest, CommandID: binding.CommandID, ScopeHash: command.Expectation.ScopeHash, AdvertisingAt: request.NotBefore, Ports: command.Expectation.Ports})
	if e != nil {
		return decision, e
	}
	belongs := false
	for _, r := range requests {
		if string(networkJSON(r)) == string(networkJSON(request)) {
			belongs = true
			break
		}
	}
	if !belongs {
		return decision, wiring.ErrCaptureRejected
	}
	var original []byte
	var hash string
	e = tx.QueryRowContext(ctx, `SELECT request,content_hash FROM validation_wiring_readback_dispatches WHERE child_command_id=$1 AND command_id=$2`, request.CommandID, binding.CommandID).Scan(&original, &hash)
	if e == nil {
		var prior fabricboot.CollectorRecollectRequest
		if json.Unmarshal(original, &prior) != nil || netbaseline.ContentHash(prior) != hash || string(networkJSON(prior)) != string(networkJSON(request)) {
			return decision, wiring.ErrCaptureRejected
		}
		var result []byte
		e = tx.QueryRowContext(ctx, `SELECT response,content_hash FROM validation_wiring_readback_results WHERE child_command_id=$1`, request.CommandID).Scan(&result, &hash)
		if e == nil {
			var response fabricboot.CollectorRecollectResponse
			if json.Unmarshal(result, &response) != nil || netbaseline.ContentHash(response) != hash || response.Validate(request, time.Now().UTC()) != nil {
				return decision, wiring.ErrOutcomeUnknown
			}
			decision.Result = &response
		} else if !errors.Is(e, sql.ErrNoRows) {
			return decision, e
		}
		return decision, tx.Commit() // Existing intent permits only query, never POST.
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return decision, e
	}
	// The first child fixes the live advertising window for every later chunk.
	_, e = tx.ExecContext(ctx, `INSERT INTO validation_wiring_readback_windows(command_id,advertising_at) VALUES($1,$2) ON CONFLICT(command_id) DO NOTHING`, binding.CommandID, request.NotBefore)
	if e != nil {
		return decision, e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO validation_wiring_readback_dispatches(child_command_id,command_id,request,content_hash,committed_at) VALUES($1,$2,$3,$4,statement_timestamp())`, request.CommandID, binding.CommandID, networkJSON(request), netbaseline.ContentHash(request))
	if e != nil {
		return decision, e
	}
	if e = tx.Commit(); e != nil {
		return decision, e
	}
	return wiring.ReadbackDecision{Send: true}, nil
}
func (l *WiringReadbackLedger) Complete(ctx context.Context, binding wiring.ReadbackBinding, request fabricboot.CollectorRecollectRequest, response fabricboot.CollectorRecollectResponse) error {
	if l == nil || l.Store == nil || !l.Store.WiringEnabled || request.Validate() != nil {
		return wiring.ErrCaptureRejected
	}
	tx, e := l.Store.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = lockKey(ctx, tx, "wiring-command:"+binding.CommandID); e != nil {
		return e
	}
	if _, e = loadWiringCommandTx(ctx, tx, binding); e != nil {
		return e
	}
	validationAt := time.Now().UTC()
	var queryRaw []byte
	var queryHash string
	queryErr := tx.QueryRowContext(ctx, `SELECT observation,content_hash FROM validation_wiring_readback_queries WHERE child_command_id=$1`, request.CommandID).Scan(&queryRaw, &queryHash)
	if queryErr == nil {
		var observation fabricboot.CollectorRecollectObservation
		if json.Unmarshal(queryRaw, &observation) != nil || netbaseline.ContentHash(observation) != queryHash || observation.State != "completed" || observation.Validate(request, time.Now().UTC()) != nil || string(networkJSON(observation.Result)) != string(networkJSON(response)) {
			return wiring.ErrOutcomeUnknown
		}
		validationAt = *observation.FinishedAt
	} else if !errors.Is(queryErr, sql.ErrNoRows) {
		return queryErr
	}
	if response.Validate(request, validationAt) != nil {
		return wiring.ErrOutcomeUnknown
	}
	var raw []byte
	var hash string
	e = tx.QueryRowContext(ctx, `SELECT request,content_hash FROM validation_wiring_readback_dispatches WHERE child_command_id=$1 AND command_id=$2`, request.CommandID, binding.CommandID).Scan(&raw, &hash)
	if e != nil {
		return e
	}
	var original fabricboot.CollectorRecollectRequest
	if json.Unmarshal(raw, &original) != nil || netbaseline.ContentHash(original) != hash || string(networkJSON(original)) != string(networkJSON(request)) {
		return wiring.ErrCaptureRejected
	}
	e = tx.QueryRowContext(ctx, `SELECT response,content_hash FROM validation_wiring_readback_results WHERE child_command_id=$1`, request.CommandID).Scan(&raw, &hash)
	if e == nil {
		var prior fabricboot.CollectorRecollectResponse
		if json.Unmarshal(raw, &prior) != nil || netbaseline.ContentHash(prior) != hash || string(networkJSON(prior)) != string(networkJSON(response)) {
			return wiring.ErrCaptureRejected
		}
		return tx.Commit()
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO validation_wiring_readback_results(child_command_id,response,content_hash,received_at) VALUES($1,$2,$3,statement_timestamp())`, request.CommandID, networkJSON(response), netbaseline.ContentHash(response))
	if e != nil {
		return e
	}
	return tx.Commit()
}

// Observe retains the authenticated terminal response of an original-command
// query. HTTP absence/running/unknown can never be stored as stopped evidence.
func (l *WiringReadbackLedger) Observe(ctx context.Context, binding wiring.ReadbackBinding, request fabricboot.CollectorRecollectRequest, observation fabricboot.CollectorRecollectObservation) error {
	if l == nil || l.Store == nil || !l.Store.WiringEnabled || !observation.Terminal() || observation.Validate(request, time.Now().UTC()) != nil {
		return wiring.ErrOutcomeUnknown
	}
	tx, e := l.Store.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = lockKey(ctx, tx, "wiring-command:"+binding.CommandID); e != nil {
		return e
	}
	if _, e = loadWiringCommandTx(ctx, tx, binding); e != nil {
		return e
	}
	var original, prior []byte
	var hash string
	e = tx.QueryRowContext(ctx, `SELECT request,content_hash FROM validation_wiring_readback_dispatches WHERE child_command_id=$1 AND command_id=$2`, request.CommandID, binding.CommandID).Scan(&original, &hash)
	if e != nil {
		return e
	}
	var frozen fabricboot.CollectorRecollectRequest
	if json.Unmarshal(original, &frozen) != nil || netbaseline.ContentHash(frozen) != hash || string(networkJSON(frozen)) != string(networkJSON(request)) {
		return wiring.ErrCaptureRejected
	}
	e = tx.QueryRowContext(ctx, `SELECT observation,content_hash FROM validation_wiring_readback_queries WHERE child_command_id=$1`, request.CommandID).Scan(&prior, &hash)
	if e == nil {
		var old fabricboot.CollectorRecollectObservation
		if json.Unmarshal(prior, &old) != nil || netbaseline.ContentHash(old) != hash || string(networkJSON(old)) != string(networkJSON(observation)) {
			return wiring.ErrOutcomeUnknown
		}
		return tx.Commit()
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO validation_wiring_readback_queries(child_command_id,observation,content_hash,observed_at) VALUES($1,$2,$3,statement_timestamp())`, request.CommandID, networkJSON(observation), netbaseline.ContentHash(observation))
	if e != nil {
		return e
	}
	return tx.Commit()
}
