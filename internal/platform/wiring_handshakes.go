package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/enzoOvOyu/keel/internal/fabricboot"
)

// A handshake is immutable readiness evidence, not a G2 verdict or permission
// to execute arbitrary commands. Its lease starts before the collector probe.
type WiringHandshake struct {
	BatchID            string                 `json:"batch_id"`
	ID                 string                 `json:"id"`
	ProjectID          string                 `json:"project_id"`
	Facts              *WiringFacts           `json:"facts"`
	Collector          fabricboot.ProbeResult `json:"collector"`
	ReadinessAt        time.Time              `json:"readiness_at"`
	ValidUntil         time.Time              `json:"valid_until"`
	CreatedBy          string                 `json:"created_by"`
	CreatedAt          time.Time              `json:"created_at"`
	Reason             string                 `json:"reason"`
	Validity           string                 `json:"validity"`
	Ready              bool                   `json:"ready"`
	InvalidationReason string                 `json:"invalidation_reason"`
}

func wiringScopePermissionTx(ctx context.Context, tx *sql.Tx, a *AuthSession, project string, write bool) error {
	if e := requireProjectMember(ctx, tx, a, project); e != nil {
		return e
	}
	if !write {
		return nil
	}
	role, e := issueRoleTx(ctx, tx, a, project)
	if e != nil {
		return e
	}
	if !slices.Contains([]string{"Admin", "DE", "DL", "NE", "SRE"}, role) {
		return ErrForbidden
	}
	var state string
	if e = tx.QueryRowContext(ctx, `SELECT state FROM projects WHERE id=$1`, project).Scan(&state); e != nil {
		return e
	}
	if state == "archived" || state == "delivered" {
		return apiError(409, "PROJECT_READ_ONLY", "Project is read-only")
	}
	return nil
}

// POST /scope is a bounded read with an explicit selection. It performs no
// network probe, writes no evidence and never returns a synthetic online proof.
func (s *Store) PreviewWiring(ctx context.Context, a *AuthSession, project string, targets []WiringTargetInput) (*WiringFacts, error) {
	normalized, e := normalizeWiringTargets(targets)
	if e != nil {
		return nil, e
	}
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = wiringScopePermissionTx(ctx, tx, a, project, false); e != nil {
		return nil, e
	}
	out, e := s.wiringFactsTx(ctx, tx, project, normalized, false)
	if e != nil {
		return nil, e
	}
	return out, tx.Commit()
}

func wiringHandshakeIdentity(project, actor, key string) string {
	return digest("wiring.handshake:" + project + ":" + actor + ":" + key)
}
func priorWiringHandshakeTx(ctx context.Context, tx *sql.Tx, identity, requestHash string) (string, error) {
	var hash string
	var raw []byte
	e := tx.QueryRowContext(ctx, `SELECT request_hash,response FROM idempotencies WHERE id=$1`, identity).Scan(&hash, &raw)
	if errors.Is(e, sql.ErrNoRows) {
		return "", nil
	}
	if e != nil {
		return "", e
	}
	if hash != requestHash {
		return "", apiError(409, "IDEMPOTENCY_CONFLICT", "This key was used for a different handshake")
	}
	var ref struct {
		ID string `json:"id"`
	}
	if e = json.Unmarshal(raw, &ref); e != nil {
		return "", e
	}
	return ref.ID, nil
}

func (s *Store) CreateWiringHandshake(ctx context.Context, a *AuthSession, project string, in WiringHandshakeInput, key, trace string) (*WiringHandshake, error) {
	in.Reason = strings.TrimSpace(in.Reason)
	if utf8.RuneCountInString(in.Reason) < 3 || utf8.RuneCountInString(in.Reason) > 1000 || strings.TrimSpace(key) == "" || len(key) > 128 {
		return nil, apiError(400, "VALIDATION_ERROR", "Reason and Idempotency-Key required")
	}
	targets, e := normalizeWiringTargets(in.Targets)
	if e != nil {
		return nil, e
	}
	in.Targets = targets
	identity := wiringHandshakeIdentity(project, a.User.ID, key)
	requestHash := digest(string(networkJSON(in)))
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	// Close the pre-probe transaction on every path, before any hardware I/O.
	var facts *WiringFacts
	var prior string
	func() {
		defer tx.Rollback()
		if e = wiringScopePermissionTx(ctx, tx, a, project, true); e != nil {
			return
		}
		prior, e = priorWiringHandshakeTx(ctx, tx, identity, requestHash)
		if e != nil || prior != "" {
			return
		}
		facts, e = s.wiringFactsTx(ctx, tx, project, targets, false)
	}()
	if e != nil {
		return nil, e
	}
	if prior != "" {
		return s.GetWiringHandshake(ctx, a, project, prior)
	}
	if !facts.Ready {
		return nil, apiError(409, "WIRING_PREREQUISITES_REQUIRED", "Refresh scope and resolve G1/topology/NP-1/NP-2/NP-4 blockers")
	}
	// No DB locks are retained across this fixed authenticated GET. The caller
	// cannot select the endpoint, URL, credentials or probe result.
	readinessAt := time.Now().UTC()
	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	collector := s.FabricProbes.CheckCollector(probeCtx)
	cancel()
	if collector.State != "ready" {
		return nil, apiError(409, "WIRING_COLLECTOR_OFFLINE", "The authenticated collector did not prove ready")
	}
	tx, e = s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = wiringScopePermissionTx(ctx, tx, a, project, true); e != nil {
		return nil, e
	}
	if e = lockKey(ctx, tx, identity); e != nil {
		return nil, e
	}
	prior, e = priorWiringHandshakeTx(ctx, tx, identity, requestHash)
	if e != nil {
		return nil, e
	}
	if prior != "" {
		out, e := s.wiringHandshakeTx(ctx, tx, project, prior, true)
		if e != nil {
			return nil, e
		}
		return out, tx.Commit()
	}
	current, e := s.wiringFactsTx(ctx, tx, project, targets, true)
	if e != nil {
		return nil, e
	}
	if !current.Ready || current.FactsHash != facts.FactsHash {
		return nil, apiError(409, "WIRING_SCOPE_CHANGED", "Evidence changed while checking collector readiness; refresh the selection")
	}
	validUntil := readinessAt.Add(time.Duration(current.TTLSeconds) * time.Second)
	if current.EvidenceValidUntil.Before(validUntil) {
		validUntil = current.EvidenceValidUntil
	}
	if !validUntil.After(time.Now()) {
		return nil, apiError(409, "WIRING_HANDSHAKE_EXPIRED", "Readiness lease expired before persistence")
	}
	out := &WiringHandshake{ID: RandomID(), ProjectID: project, Facts: current, Collector: collector, ReadinessAt: readinessAt, ValidUntil: validUntil, CreatedBy: a.User.ID, CreatedAt: time.Now().UTC(), Reason: in.Reason, Validity: "fresh", Ready: true}
	_, e = tx.ExecContext(ctx, `INSERT INTO validation_wiring_handshakes(id,project_id,facts_hash,facts,collector,readiness_at,valid_until,created_by,reason,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, out.ID, project, current.FactsHash, networkJSON(current), networkJSON(collector), readinessAt, validUntil, a.User.ID, in.Reason, out.CreatedAt)
	if e != nil {
		return nil, e
	}
	if e = appendAudit(ctx, tx, a.User.ID, project, "wiring.handshake", out.ID, trace, map[string]any{"facts_hash": current.FactsHash, "targets": len(targets), "valid_until": validUntil, "reason": in.Reason}); e != nil {
		return nil, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO idempotencies(id,actor_id,request_hash,response,created_at) VALUES($1,$2,$3,$4,now())`, identity, a.User.ID, requestHash, networkJSON(map[string]string{"id": out.ID})); e != nil {
		return nil, e
	}
	return out, tx.Commit()
}

func wiringTargetsFromFacts(facts *WiringFacts) []WiringTargetInput {
	out := make([]WiringTargetInput, 0, len(facts.Targets))
	for _, host := range facts.Targets {
		target := WiringTargetInput{AssetID: host.AssetID, PhysicalPortIDs: []string{}}
		for _, port := range host.Ports {
			target.PhysicalPortIDs = append(target.PhysicalPortIDs, port.PhysicalPortID)
		}
		out = append(out, target)
	}
	return out
}
func (s *Store) wiringHandshakeTx(ctx context.Context, tx *sql.Tx, project, id string, revalidate bool) (*WiringHandshake, error) {
	var out WiringHandshake
	var facts, collector []byte
	e := tx.QueryRowContext(ctx, `SELECT id,project_id,facts,collector,readiness_at,valid_until,created_by,created_at,reason FROM validation_wiring_handshakes WHERE project_id=$1 AND id=$2`, project, id).Scan(&out.ID, &out.ProjectID, &facts, &collector, &out.ReadinessAt, &out.ValidUntil, &out.CreatedBy, &out.CreatedAt, &out.Reason)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if e != nil {
		return nil, e
	}
	if e = tx.QueryRowContext(ctx, `SELECT coalesce((SELECT id FROM validation_wiring_batches WHERE handshake_id=$1 AND project_id=$2),'')`, id, project).Scan(&out.BatchID); e != nil {
		return nil, e
	}
	if e = json.Unmarshal(facts, &out.Facts); e != nil {
		return nil, e
	}
	if e = json.Unmarshal(collector, &out.Collector); e != nil {
		return nil, e
	}
	var dispatched bool
	if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM validation_wiring_commands WHERE handshake_id=$1) OR EXISTS(SELECT 1 FROM validation_wiring_runs WHERE handshake_id=$1)`, out.ID).Scan(&dispatched); e != nil {
		return nil, e
	}
	if out.BatchID != "" || dispatched {
		out.Validity = "invalidated"
		out.InvalidationReason = "WIRING_HANDSHAKE_CONSUMED"
		return &out, nil
	}
	out.Validity = "stale"
	out.InvalidationReason = "WIRING_HANDSHAKE_EXPIRED"
	if out.ValidUntil.After(time.Now()) && revalidate {
		current, e := s.wiringFactsTx(ctx, tx, project, wiringTargetsFromFacts(out.Facts), false)
		if e != nil {
			var domain *Error
			if !errors.As(e, &domain) || domain.Status != 409 && domain.Status != 404 {
				return nil, e
			}
			out.Validity = "invalidated"
			out.InvalidationReason = domain.Code
			return &out, nil
		}
		if current.Ready && current.FactsHash == out.Facts.FactsHash {
			out.Validity = "fresh"
			out.Ready = true
			out.InvalidationReason = ""
		} else {
			out.Validity = "invalidated"
			out.InvalidationReason = "WIRING_SCOPE_CHANGED"
		}
	} else if out.ValidUntil.After(time.Now()) {
		out.Validity = "unchecked"
		out.InvalidationReason = "WIRING_CURRENT_SCOPE_NOT_CHECKED"
	}
	return &out, nil
}
func (s *Store) GetWiringHandshake(ctx context.Context, a *AuthSession, project, id string) (*WiringHandshake, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = wiringScopePermissionTx(ctx, tx, a, project, false); e != nil {
		return nil, e
	}
	out, e := s.wiringHandshakeTx(ctx, tx, project, id, true)
	if e != nil {
		return nil, e
	}
	return out, tx.Commit()
}
func (s *Store) ListWiringHandshakes(ctx context.Context, a *AuthSession, project string, page Pagination) (Page[WiringHandshake], error) {
	out := NewPage[WiringHandshake](page)
	if e := page.Validate(); e != nil {
		return out, e
	}
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	if e = wiringScopePermissionTx(ctx, tx, a, project, false); e != nil {
		return out, e
	}
	if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM validation_wiring_handshakes WHERE project_id=$1`, project).Scan(&out.Total); e != nil {
		return out, e
	}
	rows, e := tx.QueryContext(ctx, `SELECT id FROM validation_wiring_handshakes WHERE project_id=$1 ORDER BY created_at DESC,id DESC LIMIT $2 OFFSET $3`, project, page.PageSize, page.Offset())
	if e != nil {
		return out, e
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return out, e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	// History does not fan out a fresh scope check for every old lease. Only the
	// explicit detail can return ready=true; history never represents admission.
	for _, id := range ids {
		record, e := s.wiringHandshakeTx(ctx, tx, project, id, false)
		if e != nil {
			return out, e
		}
		out.Items = append(out.Items, *record)
	}
	return out, tx.Commit()
}
