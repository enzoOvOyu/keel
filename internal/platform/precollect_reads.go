package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/enzoOvOyu/keel/internal/precollect"
)

func precollectReadScope(ctx context.Context, tx *sql.Tx, a *AuthSession, project, asset string) error {
	if e := requireProjectMember(ctx, tx, a, project); e != nil {
		return e
	}
	var exists bool
	if e := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM project_assets WHERE id=$1 AND project_id=$2)`, asset, project).Scan(&exists); e != nil {
		return e
	}
	if !exists {
		return ErrNotFound
	}
	return nil
}

const precollectReadFields = `q.id,q.project_id,q.asset_id,q.machine_id,q.execution_id,x.state,q.created_at,coalesce(r.origin,''),r.collected_at,coalesce(r.raw_sha256,''),coalesce(r.raw_size,0)`
const precollectReadFrom = ` FROM validation_precollect_requests q JOIN exec_executions x ON x.id=q.execution_id LEFT JOIN validation_precollect_records r ON r.id=q.id WHERE q.project_id=$1 AND q.asset_id=$2`

func scanPrecollect(row scanner) (*PrecollectRecord, error) {
	var out PrecollectRecord
	e := row.Scan(&out.ID, &out.ProjectID, &out.AssetID, &out.MachineID, &out.ExecutionID, &out.ExecutionState, &out.CreatedAt, &out.Origin, &out.CollectedAt, &out.RawSHA256, &out.RawSize)
	return &out, e
}
func (s *Store) ListPrecollect(ctx context.Context, a *AuthSession, project, asset string, page Pagination) (Page[PrecollectRecord], error) {
	out := NewPage[PrecollectRecord](page)
	if e := page.Validate(); e != nil {
		return out, e
	}
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	if e = precollectReadScope(ctx, tx, a, project, asset); e != nil {
		return out, e
	}
	if e = tx.QueryRowContext(ctx, `SELECT count(*)`+precollectReadFrom, project, asset).Scan(&out.Total); e != nil {
		return out, e
	}
	rows, e := tx.QueryContext(ctx, `SELECT `+precollectReadFields+precollectReadFrom+` ORDER BY q.created_at DESC,q.id DESC LIMIT $3 OFFSET $4`, project, asset, page.PageSize, page.Offset())
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		r, e := scanPrecollect(rows)
		if e != nil {
			return out, e
		}
		out.Items = append(out.Items, *r)
	}
	return out, rows.Err()
}
func (s *Store) precollectGateTx(ctx context.Context, tx *sql.Tx, project, asset, id string, scope *PrecollectScope) (*ValidationGateEvidence, error) {
	var g ValidationGateEvidence
	var evaluation []byte
	e := tx.QueryRowContext(ctx, `SELECT id,gate,precollect_id,scope_hash,revision_fingerprint,verdict,rule_version,observed_at,valid_until,evaluation FROM validation_gate_evidence WHERE project_id=$1 AND asset_id=$2 AND precollect_id=$3`, project, asset, id).Scan(&g.ID, &g.Gate, &g.PrecollectID, &g.ScopeHash, &g.RevisionFingerprint, &g.Verdict, &g.RuleVersion, &g.ObservedAt, &g.ValidUntil, &evaluation)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	if e = json.Unmarshal(evaluation, &g.Evaluation); e != nil {
		return nil, e
	}
	g.Validity = "fresh"
	g.EffectiveVerdict = g.Verdict
	if !g.ValidUntil.After(time.Now()) {
		g.Validity = "stale"
		g.EffectiveVerdict = "BLOCKED"
		g.InvalidationReason = "G1_EVIDENCE_EXPIRED"
		return &g, nil
	}
	current, e := s.PreparePrecollectScopeTx(ctx, tx, project, asset, scope, false)
	if e != nil {
		var domain *Error
		if !errors.As(e, &domain) {
			return nil, e
		}
		g.Validity = "invalidated"
		g.EffectiveVerdict = "BLOCKED"
		g.InvalidationReason = domain.Code
		return &g, nil
	}
	if !current.Ready || current.ScopeHash != g.ScopeHash {
		g.Validity = "invalidated"
		g.EffectiveVerdict = "BLOCKED"
		g.InvalidationReason = "PRECOLLECT_SCOPE_CHANGED"
		if !current.Ready {
			g.InvalidationReason = current.Code
		}
	}
	if g.Validity == "fresh" && g.Verdict == "BLOCKED" {
		var waiver sql.NullString
		if e = tx.QueryRowContext(ctx, `SELECT keel_current_g1_waiver($1)`, g.ID).Scan(&waiver); e != nil {
			return nil, e
		}
		if waiver.Valid {
			var until time.Time
			if e = tx.QueryRowContext(ctx, `SELECT valid_until FROM gate_waivers WHERE id=$1`, waiver.String).Scan(&until); e != nil {
				return nil, e
			}
			g.WaiverID = waiver.String
			g.EffectiveVerdict = "WAIVED"
			g.EffectiveValidUntil = &until
		}
	}
	return &g, nil
}
func (s *Store) GetPrecollect(ctx context.Context, a *AuthSession, project, asset, id string) (*PrecollectRecord, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = precollectReadScope(ctx, tx, a, project, asset); e != nil {
		return nil, e
	}
	out, e := scanPrecollect(tx.QueryRowContext(ctx, `SELECT `+precollectReadFields+precollectReadFrom+` AND q.id=$3`, project, asset, id))
	if errors.Is(e, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if e != nil {
		return nil, e
	}
	var scopeJSON, reportJSON []byte
	if e = tx.QueryRowContext(ctx, `SELECT q.scope,r.report FROM validation_precollect_requests q LEFT JOIN validation_precollect_records r ON r.id=q.id WHERE q.id=$1`, id).Scan(&scopeJSON, &reportJSON); e != nil {
		return nil, e
	}
	out.Scope = &PrecollectScope{}
	if e = json.Unmarshal(scopeJSON, out.Scope); e != nil {
		return nil, e
	}
	if len(reportJSON) > 0 {
		out.Report = &precollect.Report{}
		if e = json.Unmarshal(reportJSON, out.Report); e != nil {
			return nil, e
		}
	}
	out.Gate, e = s.precollectGateTx(ctx, tx, project, asset, id, out.Scope)
	return out, e
}

type CurrentG1 struct {
	Gate             string                  `json:"gate"`
	Evidence         *ValidationGateEvidence `json:"evidence"`
	EffectiveVerdict string                  `json:"effective_verdict"`
	Reason           string                  `json:"reason"`
}

func (s *Store) CurrentG1(ctx context.Context, a *AuthSession, project, asset string) (*CurrentG1, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = precollectReadScope(ctx, tx, a, project, asset); e != nil {
		return nil, e
	}
	return s.currentG1Tx(ctx, tx, project, asset)
}
func (s *Store) currentG1Tx(ctx context.Context, tx *sql.Tx, project, asset string) (*CurrentG1, error) {
	out := &CurrentG1{Gate: "G1", EffectiveVerdict: "BLOCKED", Reason: "G1_EVIDENCE_REQUIRED"}
	var e error
	var id string
	var scopeJSON []byte
	e = tx.QueryRowContext(ctx, `SELECT q.id,q.scope FROM validation_gate_evidence g JOIN validation_precollect_requests q ON q.id=g.precollect_id WHERE g.project_id=$1 AND g.asset_id=$2 AND g.gate='G1' ORDER BY g.created_at DESC,g.id DESC LIMIT 1`, project, asset).Scan(&id, &scopeJSON)
	if errors.Is(e, sql.ErrNoRows) {
		return out, nil
	}
	if e != nil {
		return nil, e
	}
	var scope PrecollectScope
	if e = json.Unmarshal(scopeJSON, &scope); e != nil {
		return nil, e
	}
	out.Evidence, e = s.precollectGateTx(ctx, tx, project, asset, id, &scope)
	if e != nil {
		return nil, e
	}
	if out.Evidence != nil {
		out.Reason = out.Evidence.InvalidationReason
		if out.Evidence.Validity == "fresh" {
			out.EffectiveVerdict = out.Evidence.EffectiveVerdict
			out.Reason = ""
		}
	}
	return out, nil
}

// Permission is checked before any versioned object is opened. The complete
// retained bytes are bounded and digest-checked before serving a download.
func (s *Store) PrecollectRaw(ctx context.Context, a *AuthSession, project, asset, id string) ([]byte, string, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, "", e
	}
	defer tx.Rollback()
	if e = precollectReadScope(ctx, tx, a, project, asset); e != nil {
		return nil, "", e
	}
	var key, version, sha string
	var size int64
	e = tx.QueryRowContext(ctx, `SELECT r.raw_key,r.raw_version,r.raw_sha256,r.raw_size FROM validation_precollect_records r JOIN validation_precollect_requests q ON q.id=r.id WHERE q.project_id=$1 AND q.asset_id=$2 AND q.id=$3`, project, asset, id).Scan(&key, &version, &sha, &size)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, "", ErrNotFound
	}
	if e != nil {
		return nil, "", e
	}
	tx.Rollback()
	if s.MediaObjects == nil {
		return nil, "", apiError(409, "PRECOLLECT_RAW_UNAVAILABLE", "Versioned raw storage unavailable")
	}
	reader, e := s.MediaObjects.Open(ctx, key, version)
	if e != nil {
		return nil, "", apiError(409, "PRECOLLECT_RAW_UNAVAILABLE", "Original version unavailable")
	}
	defer reader.Close()
	raw, e := io.ReadAll(io.LimitReader(reader, precollect.MaxBundleBytes+1))
	if e != nil || int64(len(raw)) != size || precollect.Hash(raw) != sha {
		return nil, "", apiError(409, "PRECOLLECT_RAW_CORRUPT", "Original raw version failed digest verification")
	}
	return raw, sha, nil
}
