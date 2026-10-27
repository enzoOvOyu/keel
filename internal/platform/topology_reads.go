package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// Historical verdict is immutable; effective verdict is recomputed from the
// current G1/model/BOM scope. GET never performs a capture or inserts evidence.
func (s *Store) topologyValidityTx(ctx context.Context, tx *sql.Tx, out *PciTopologySnapshot) error {
	out.Validity = "fresh"
	out.EffectiveVerdict = out.Verdict
	out.InvalidationReason = ""
	if !out.ValidUntil.After(time.Now()) {
		out.Validity = "stale"
		out.EffectiveVerdict = "BLOCKED"
		out.InvalidationReason = "TOPOLOGY_EVIDENCE_EXPIRED"
		return nil
	}
	current, _, e := s.topologyScopeTx(ctx, tx, out.ProjectID, out.AssetID, false)
	if e != nil {
		if code, ok := topologyReadInvalidation(e); ok {
			out.Validity = "invalidated"
			out.EffectiveVerdict = "BLOCKED"
			out.InvalidationReason = code
			return nil
		}
		return e
	}
	if !current.Ready || current.ScopeHash != out.ScopeHash {
		out.Validity = "invalidated"
		out.EffectiveVerdict = "BLOCKED"
		out.InvalidationReason = "TOPOLOGY_SCOPE_CHANGED"
		if !current.Ready {
			out.InvalidationReason = current.Code
		}
	}
	return nil
}
func (s *Store) topologyDetailTx(ctx context.Context, tx *sql.Tx, project, asset, id string) (*PciTopologySnapshot, error) {
	out, e := scanTopology(tx.QueryRowContext(ctx, `SELECT `+topologyFields+` FROM pci_topology_snapshots WHERE project_id=$1 AND asset_id=$2 AND id=$3`, project, asset, id))
	if errors.Is(e, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if e != nil {
		return nil, e
	}
	var raw, scope []byte
	if e = tx.QueryRowContext(ctx, `SELECT snapshot,scope FROM pci_topology_snapshots WHERE id=$1`, id).Scan(&raw, &scope); e != nil {
		return nil, e
	}
	if e = json.Unmarshal(raw, &out.Snapshot); e != nil {
		return nil, e
	}
	if e = json.Unmarshal(scope, &out.Scope); e != nil {
		return nil, e
	}
	rows, e := tx.QueryContext(ctx, `SELECT id,detail,coalesce(issue_id,'') FROM topology_diffs WHERE snapshot_id=$1 ORDER BY ordinal`, id)
	if e != nil {
		return nil, e
	}
	for rows.Next() {
		var d TopologyDiff
		var raw []byte
		if e = rows.Scan(&d.ID, &raw, &d.IssueID); e != nil {
			rows.Close()
			return nil, e
		}
		if e = json.Unmarshal(raw, &d.Detail); e != nil {
			rows.Close()
			return nil, e
		}
		out.Diffs = append(out.Diffs, d)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	if e = s.topologyValidityTx(ctx, tx, out); e != nil {
		return nil, e
	}
	return out, nil
}
func (s *Store) GetTopology(ctx context.Context, a *AuthSession, project, asset, id string) (*PciTopologySnapshot, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = precollectReadScope(ctx, tx, a, project, asset); e != nil {
		return nil, e
	}
	return s.topologyDetailTx(ctx, tx, project, asset, id)
}
func (s *Store) ListTopology(ctx context.Context, a *AuthSession, project, asset string, page Pagination) (Page[PciTopologySnapshot], error) {
	out := NewPage[PciTopologySnapshot](page)
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
	if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM pci_topology_snapshots WHERE project_id=$1 AND asset_id=$2`, project, asset).Scan(&out.Total); e != nil {
		return out, e
	}
	rows, e := tx.QueryContext(ctx, `SELECT `+topologyFields+` FROM pci_topology_snapshots WHERE project_id=$1 AND asset_id=$2 ORDER BY version DESC LIMIT $3 OFFSET $4`, project, asset, page.PageSize, page.Offset())
	if e != nil {
		return out, e
	}
	for rows.Next() {
		v, e := scanTopology(rows)
		if e != nil {
			rows.Close()
			return out, e
		}
		out.Items = append(out.Items, *v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	// Calculate current scope once for a whole history page, not per row.
	current, _, e := s.topologyScopeTx(ctx, tx, project, asset, false)
	if e != nil {
		if code, ok := topologyReadInvalidation(e); ok {
			current = &TopologyScope{Ready: false, Code: code}
		} else {
			return out, e
		}
	}
	for i := range out.Items {
		v := &out.Items[i]
		v.Validity = "fresh"
		v.EffectiveVerdict = v.Verdict
		if !v.ValidUntil.After(time.Now()) {
			v.Validity = "stale"
			v.InvalidationReason = "TOPOLOGY_EVIDENCE_EXPIRED"
		} else if !current.Ready || current.ScopeHash != v.ScopeHash {
			v.Validity = "invalidated"
			v.InvalidationReason = "TOPOLOGY_SCOPE_CHANGED"
			if !current.Ready {
				v.InvalidationReason = current.Code
			}
		}
		if v.Validity != "fresh" {
			v.EffectiveVerdict = "BLOCKED"
		}
	}
	return out, nil
}
