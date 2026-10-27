package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/enzoOvOyu/keel/internal/wiring"
)

type WiringBatchInput struct {
	HandshakeID string `json:"handshake_id"`
	FactsHash   string `json:"facts_hash"`
	Reason      string `json:"reason"`
}
type WiringBatch struct {
	ID            string              `json:"id"`
	ProjectID     string              `json:"project_id"`
	HandshakeID   string              `json:"handshake_id"`
	FactsHash     string              `json:"facts_hash"`
	CreatedBy     string              `json:"created_by"`
	CreatedAt     time.Time           `json:"created_at"`
	Reason        string              `json:"reason"`
	Targets       []WiringBatchTarget `json:"targets"`
	State         string              `json:"state"`
	NextStep      int                 `json:"next_step"`
	CanStartStep2 bool                `json:"can_start_step2"`
	Code          string              `json:"code"`
}
type WiringBatchTarget struct {
	AssetID     string                 `json:"asset_id"`
	MachineID   string                 `json:"machine_id"`
	TopologyID  string                 `json:"topology_id"`
	Expectation wiring.Expectation     `json:"expectation"`
	Relations   *TopologyRelationTable `json:"relations"`
	Reports     []WiringStepReport     `json:"reports"`
}
type WiringStepDetail struct {
	Version    string                 `json:"version"`
	Step       int                    `json:"step"`
	Verdict    string                 `json:"verdict"`
	TopologyID string                 `json:"topology_id"`
	Relations  *TopologyRelationTable `json:"relations"`
}
type WiringStepReport struct {
	ValidUntil         *time.Time       `json:"valid_until"`
	ID                 string           `json:"id"`
	BatchID            string           `json:"batch_id"`
	AssetID            string           `json:"asset_id"`
	Step               int              `json:"step"`
	Version            int64            `json:"version"`
	ParentID           string           `json:"parent_id"`
	SourceHash         string           `json:"source_hash"`
	Verdict            string           `json:"verdict"`
	Detail             WiringStepDetail `json:"detail"`
	CreatedBy          string           `json:"created_by"`
	CreatedAt          time.Time        `json:"created_at"`
	EffectiveVerdict   string           `json:"effective_verdict"`
	InvalidationReason string           `json:"invalidation_reason"`
}

// This explicit operation consumes a short-lived handshake exactly once and
// publishes only immutable Step 1. LLDP execution cannot be manufactured by a
// browser payload, ordinary Flow or a direct StepReport insertion.
func (s *Store) CreateWiringBatch(ctx context.Context, a *AuthSession, project string, in WiringBatchInput, key, trace string) (*WiringBatch, error) {
	in.Reason = strings.TrimSpace(in.Reason)
	if !validEntityID(in.HandshakeID) || len(in.FactsHash) != 64 || utf8.RuneCountInString(in.Reason) < 3 || utf8.RuneCountInString(in.Reason) > 1000 || strings.TrimSpace(key) == "" || len(key) > 128 {
		return nil, apiError(400, "VALIDATION_ERROR", "Exact handshake, facts hash, reason and Idempotency-Key required")
	}
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = wiringScopePermissionTx(ctx, tx, a, project, true); e != nil {
		return nil, e
	}
	identity := digest("wiring.batch:" + project + ":" + a.User.ID + ":" + key)
	requestHash := digest(string(networkJSON(in)))
	if e = lockKey(ctx, tx, identity); e != nil {
		return nil, e
	}
	prior, e := priorWiringHandshakeTx(ctx, tx, identity, requestHash)
	if e != nil {
		return nil, e
	}
	if prior != "" {
		out, e := s.wiringBatchTx(ctx, tx, project, prior)
		if e != nil {
			return nil, e
		}
		return out, tx.Commit()
	}
	handshake, e := s.wiringHandshakeTx(ctx, tx, project, in.HandshakeID, true)
	if e != nil {
		return nil, e
	}
	if !handshake.Ready || handshake.Facts.FactsHash != in.FactsHash {
		return nil, apiError(409, "WIRING_HANDSHAKE_NOT_CURRENT", "Refresh readiness; this handshake is expired, changed or already consumed")
	}
	// Recheck under the same serialized control transaction and lock the exact
	// assets. Initial thirty-second admission is not reused as a dispatch lease.
	current, e := s.wiringFactsTx(ctx, tx, project, wiringTargetsFromFacts(handshake.Facts), true)
	if e != nil {
		return nil, e
	}
	if !current.Ready || current.FactsHash != in.FactsHash || !handshake.ValidUntil.After(time.Now()) {
		return nil, apiError(409, "WIRING_SCOPE_CHANGED", "Current evidence no longer matches the handshake")
	}
	out := &WiringBatch{ID: RandomID(), ProjectID: project, HandshakeID: handshake.ID, FactsHash: current.FactsHash, CreatedBy: a.User.ID, CreatedAt: time.Now().UTC(), Reason: in.Reason, Targets: []WiringBatchTarget{}, State: "awaiting_lldp_adapter", NextStep: 2, Code: "WIRING_LLDP_ADAPTER_NOT_CONNECTED"}
	if _, e = tx.ExecContext(ctx, `INSERT INTO validation_wiring_batches(id,project_id,handshake_id,facts_hash,created_by,reason,created_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, out.ID, project, handshake.ID, current.FactsHash, a.User.ID, in.Reason, out.CreatedAt); e != nil {
		return nil, e
	}
	for _, host := range current.Targets {
		expected, e := WiringExpectation(current, host.AssetID)
		if e != nil {
			return nil, e
		}
		relations, e := s.topologyRelationsTx(ctx, tx, project, host.AssetID, host.TopologyID)
		if e != nil {
			return nil, e
		}
		relations.Step1Published = true
		if _, e = tx.ExecContext(ctx, `INSERT INTO validation_wiring_batch_targets(batch_id,project_id,asset_id,machine_id,topology_id,expectation,relations) VALUES($1,$2,$3,$4,$5,$6,$7)`, out.ID, project, host.AssetID, host.MachineID, host.TopologyID, networkJSON(expected), networkJSON(relations)); e != nil {
			return nil, e
		}
		detail := WiringStepDetail{Version: "keel.wiring-step/v1", Step: 1, Verdict: relations.EffectiveVerdict, TopologyID: host.TopologyID, Relations: relations}
		sourceHash := digest(string(networkJSON([]any{current.FactsHash, host.TopologyID, relations})))
		if _, e = tx.ExecContext(ctx, `INSERT INTO validation_wiring_step_reports(id,batch_id,asset_id,step,version,source_hash,verdict,detail,created_by,created_at) VALUES($1,$2,$3,1,1,$4,$5,$6,$7,$8)`, RandomID(), out.ID, host.AssetID, sourceHash, detail.Verdict, networkJSON(detail), a.User.ID, out.CreatedAt); e != nil {
			return nil, e
		}
	}
	if e = appendAudit(ctx, tx, a.User.ID, project, "wiring.step1.publish", out.ID, trace, map[string]any{"handshake_id": handshake.ID, "facts_hash": current.FactsHash, "targets": len(current.Targets), "reason": in.Reason}); e != nil {
		return nil, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO idempotencies(id,actor_id,request_hash,response,created_at) VALUES($1,$2,$3,$4,now())`, identity, a.User.ID, requestHash, networkJSON(map[string]string{"id": out.ID})); e != nil {
		return nil, e
	}
	out, e = s.wiringBatchTx(ctx, tx, project, out.ID)
	if e != nil {
		return nil, e
	}
	return out, tx.Commit()
}
func (s *Store) wiringBatchTx(ctx context.Context, tx *sql.Tx, project, id string) (*WiringBatch, error) {
	var out WiringBatch
	e := tx.QueryRowContext(ctx, `SELECT id,project_id,handshake_id,facts_hash,created_by,created_at,reason FROM validation_wiring_batches WHERE project_id=$1 AND id=$2`, project, id).Scan(&out.ID, &out.ProjectID, &out.HandshakeID, &out.FactsHash, &out.CreatedBy, &out.CreatedAt, &out.Reason)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if e != nil {
		return nil, e
	}
	out.Targets = []WiringBatchTarget{}
	out.State = "awaiting_lldp_adapter"
	out.NextStep = 2
	out.Code = "WIRING_LLDP_ADAPTER_NOT_CONNECTED"
	rows, e := tx.QueryContext(ctx, `SELECT asset_id,machine_id,topology_id,expectation,relations FROM validation_wiring_batch_targets WHERE project_id=$1 AND batch_id=$2 ORDER BY asset_id`, project, id)
	if e != nil {
		return nil, e
	}
	for rows.Next() {
		var target WiringBatchTarget
		var expected, relations []byte
		if e = rows.Scan(&target.AssetID, &target.MachineID, &target.TopologyID, &expected, &relations); e != nil {
			rows.Close()
			return nil, e
		}
		if e = json.Unmarshal(expected, &target.Expectation); e != nil {
			rows.Close()
			return nil, e
		}
		if e = json.Unmarshal(relations, &target.Relations); e != nil {
			rows.Close()
			return nil, e
		}
		target.Reports = []WiringStepReport{}
		out.Targets = append(out.Targets, target)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	for i := range out.Targets {
		target := &out.Targets[i]
		rows, e = tx.QueryContext(ctx, `SELECT id,step,version,coalesce(parent_id,''),source_hash,verdict,detail,created_by,created_at FROM validation_wiring_step_reports WHERE batch_id=$1 AND asset_id=$2 ORDER BY step,version`, id, target.AssetID)
		if e != nil {
			return nil, e
		}
		for rows.Next() {
			report := WiringStepReport{BatchID: id, AssetID: target.AssetID}
			var raw []byte
			if e = rows.Scan(&report.ID, &report.Step, &report.Version, &report.ParentID, &report.SourceHash, &report.Verdict, &raw, &report.CreatedBy, &report.CreatedAt); e != nil {
				rows.Close()
				return nil, e
			}
			if e = json.Unmarshal(raw, &report.Detail); e != nil {
				rows.Close()
				return nil, e
			}
			report.EffectiveVerdict = report.Verdict
			target.Reports = append(target.Reports, report)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return nil, e
		}
		topology, e := s.topologyDetailTx(ctx, tx, project, target.AssetID, target.TopologyID)
		if e != nil {
			return nil, e
		}
		for j := range target.Reports {
			report := &target.Reports[j]
			report.ValidUntil = &topology.ValidUntil
			// Step 1's validity follows topology, independently of the original network
			// handshake expiring. Fresh network proof is checked again at dispatch.
			if report.Step == 1 && (topology.EffectiveVerdict != "PASS" || target.Relations == nil || topology.ScopeHash != target.Relations.ScopeHash) {
				report.EffectiveVerdict = "BLOCKED"
				report.InvalidationReason = "WIRING_STEP1_TOPOLOGY_CHANGED"
			}
			if report.Step == 1 && report.EffectiveVerdict != "PASS" {
				out.State = "step1_blocked"
				out.NextStep = 1
				out.Code = "WIRING_STEP1_REQUIRED"
			}
		}
	}
	return &out, nil
}
func (s *Store) GetWiringBatch(ctx context.Context, a *AuthSession, project, id string) (*WiringBatch, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = wiringScopePermissionTx(ctx, tx, a, project, false); e != nil {
		return nil, e
	}
	out, e := s.wiringBatchTx(ctx, tx, project, id)
	if e != nil {
		return nil, e
	}
	return out, tx.Commit()
}
