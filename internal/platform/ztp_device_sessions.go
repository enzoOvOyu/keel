package platform

import (
	"context"
)

func (s *Store) ListZTPDeviceSessions(ctx context.Context, a *AuthSession, p *ProjectSummary) ([]ZTPSessionSummary, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireFabricPermission(ctx, tx, a, p.ID, "fabric:read"); e != nil {
		return nil, e
	}
	rows, e := tx.QueryContext(ctx, `SELECT id,batch_ref,boot_channel,state,state_version,attempt,identity_resolution->>'verdict',updated_at FROM ztp_sessions WHERE project_id=$1 AND mode='device' ORDER BY updated_at DESC,id DESC LIMIT 100`, p.ID)
	if e != nil {
		return nil, e
	}
	out := []ZTPSessionSummary{}
	for rows.Next() {
		var r ZTPSessionSummary
		if e = rows.Scan(&r.ID, &r.BatchRef, &r.BootChannel, &r.State, &r.StateVersion, &r.Attempt, &r.IdentityVerdict, &r.UpdatedAt); e != nil {
			rows.Close()
			return nil, e
		}
		out = append(out, r)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	return out, tx.Commit()
}
func (s *Store) GetZTPDeviceSession(ctx context.Context, a *AuthSession, p *ProjectSummary, id string) (*ZTPSessionDetail, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireFabricPermission(ctx, tx, a, p.ID, "fabric:read"); e != nil {
		return nil, e
	}
	out, e := getZTPSessionTx(ctx, tx, p.ID, id, false)
	if e != nil {
		return nil, e
	}
	if out.Mode != "device" {
		return nil, ErrNotFound
	}
	if e = loadZTPEvents(ctx, tx, out); e != nil {
		return nil, e
	}
	return out, tx.Commit()
}
