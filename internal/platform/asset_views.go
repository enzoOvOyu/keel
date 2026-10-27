package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/enzoOvOyu/keel/internal/assetview"
)

type AssetViewInput struct {
	Name            string               `json:"name"`
	Visibility      string               `json:"visibility"`
	ExpectedVersion int64                `json:"expected_version"`
	Definition      assetview.Definition `json:"definition"`
	Reason          string               `json:"reason"`
}
type AssetView struct {
	ID          string               `json:"id"`
	ProjectID   string               `json:"project_id"`
	OwnerID     string               `json:"owner_id"`
	Visibility  string               `json:"visibility"`
	Version     int64                `json:"version"`
	Name        string               `json:"name"`
	Definition  assetview.Definition `json:"definition"`
	ContentHash string               `json:"content_hash"`
	UpdatedAt   time.Time            `json:"updated_at"`
}

const assetViewFields = `v.id,v.project_id,v.owner_id,v.visibility,r.version,r.name,r.definition,r.content_hash,r.created_at`
const assetViewFrom = ` FROM asset_views v JOIN asset_view_revisions r ON r.view_id=v.id AND r.version=v.version WHERE v.project_id=$1 AND (v.visibility='team' OR v.owner_id=$2)`

func scanAssetView(row scanner) (*AssetView, error) {
	var v AssetView
	var raw []byte
	e := row.Scan(&v.ID, &v.ProjectID, &v.OwnerID, &v.Visibility, &v.Version, &v.Name, &raw, &v.ContentHash, &v.UpdatedAt)
	if e == nil {
		e = json.Unmarshal(raw, &v.Definition)
	}
	return &v, e
}
func (s *Store) ListAssetViews(ctx context.Context, a *AuthSession, project string, page Pagination) (Page[AssetView], error) {
	out := NewPage[AssetView](page)
	if e := page.Validate(); e != nil {
		return out, e
	}
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	if e = requireProjectMember(ctx, tx, a, project); e != nil {
		return out, e
	}
	if e = tx.QueryRowContext(ctx, `SELECT count(*)`+assetViewFrom, project, a.User.ID).Scan(&out.Total); e != nil {
		return out, e
	}
	rows, e := tx.QueryContext(ctx, `SELECT `+assetViewFields+assetViewFrom+` ORDER BY v.updated_at DESC,v.id DESC LIMIT $3 OFFSET $4`, project, a.User.ID, page.PageSize, page.Offset())
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		v, e := scanAssetView(rows)
		if e != nil {
			return out, e
		}
		out.Items = append(out.Items, *v)
	}
	return out, rows.Err()
}
func (s *Store) GetAssetView(ctx context.Context, a *AuthSession, project, id string) (*AssetView, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireProjectMember(ctx, tx, a, project); e != nil {
		return nil, e
	}
	v, e := scanAssetView(tx.QueryRowContext(ctx, `SELECT `+assetViewFields+assetViewFrom+` AND v.id=$3`, project, a.User.ID, id))
	if errors.Is(e, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return v, e
}
func (s *Store) SaveAssetView(ctx context.Context, a *AuthSession, project, id string, in AssetViewInput, trace string) (*AssetView, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Reason = strings.TrimSpace(in.Reason)
	if !validAssetViewID(id) || utf8.RuneCountInString(in.Name) < 1 || utf8.RuneCountInString(in.Name) > 100 || utf8.RuneCountInString(in.Reason) < 3 || utf8.RuneCountInString(in.Reason) > 1000 || in.ExpectedVersion < 0 || (in.Visibility != "personal" && in.Visibility != "team") || in.Definition.Validate() != nil {
		return nil, apiError(400, "ASSET_VIEW_INVALID", "A named bounded JSONPath view, visibility, version and reason are required")
	}
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = lockKey(ctx, tx, "asset-view:"+id); e != nil {
		return nil, e
	}
	if e = requireProjectMember(ctx, tx, a, project); e != nil {
		return nil, e
	}
	var state string
	if e = tx.QueryRowContext(ctx, `SELECT state FROM projects WHERE id=$1 FOR SHARE`, project).Scan(&state); e != nil {
		return nil, e
	}
	if state == "archived" || state == "delivered" {
		return nil, apiError(409, "PROJECT_READ_ONLY", "Project is read-only")
	}
	var teamWriter bool
	if e = tx.QueryRowContext(ctx, `SELECT $3 OR EXISTS(SELECT 1 FROM memberships WHERE project_id=$1 AND user_id=$2 AND role IN('Admin','DE','DL','SRE'))`, project, a.User.ID, a.User.IsAdmin).Scan(&teamWriter); e != nil {
		return nil, e
	}
	current, e := scanAssetView(tx.QueryRowContext(ctx, `SELECT `+assetViewFields+assetViewFrom+` AND v.id=$3 FOR UPDATE OF v`, project, a.User.ID, id))
	exists := e == nil
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return nil, e
	}
	payloadHash := digest(string(networkJSON(map[string]any{"name": in.Name, "visibility": in.Visibility, "definition": in.Definition, "reason": in.Reason})))
	if exists {
		if current.Visibility != in.Visibility {
			return nil, apiError(409, "ASSET_VIEW_SCOPE_IMMUTABLE", "Copy the view to change its visibility")
		}
		if current.OwnerID != a.User.ID {
			if current.Visibility != "team" || !a.User.IsAdmin {
				var elevated bool
				if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM memberships WHERE project_id=$1 AND user_id=$2 AND role IN('Admin','DL'))`, project, a.User.ID).Scan(&elevated); e != nil {
					return nil, e
				}
				if !elevated {
					return nil, ErrForbidden
				}
			}
		}
		if current.Visibility == "team" && !teamWriter {
			return nil, ErrForbidden
		}
		if current.Version == in.ExpectedVersion+1 && current.ContentHash == payloadHash {
			return current, nil
		}
		if current.Version != in.ExpectedVersion {
			return nil, apiError(409, "ASSET_VIEW_VERSION_CONFLICT", "Refresh the saved view before editing")
		}
	} else {
		var taken bool
		if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM asset_views WHERE id=$1)`, id).Scan(&taken); e != nil {
			return nil, e
		}
		if taken {
			return nil, ErrNotFound
		}
		if in.ExpectedVersion != 0 {
			return nil, ErrNotFound
		}
		if in.Visibility == "team" && !teamWriter {
			return nil, ErrForbidden
		}
	}
	now := time.Now().UTC()
	version := in.ExpectedVersion + 1
	owner := a.User.ID
	if exists {
		owner = current.OwnerID
		_, e = tx.ExecContext(ctx, `UPDATE asset_views SET version=$2,updated_at=$3 WHERE id=$1`, id, version, now)
	} else {
		_, e = tx.ExecContext(ctx, `INSERT INTO asset_views(id,project_id,owner_id,visibility,version,created_at,updated_at)VALUES($1,$2,$3,$4,1,$5,$5)`, id, project, owner, in.Visibility, now)
	}
	if e != nil {
		return nil, e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO asset_view_revisions(view_id,version,name,definition,content_hash,reason,created_by,created_at)VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, id, version, in.Name, networkJSON(in.Definition), payloadHash, in.Reason, a.User.ID, now)
	if e != nil {
		return nil, e
	}
	if e = appendAudit(ctx, tx, a.User.ID, project, "asset_view.save", id, trace, map[string]any{"version": version, "visibility": in.Visibility, "content_hash": payloadHash, "reason": in.Reason}); e != nil {
		return nil, e
	}
	out := &AssetView{ID: id, ProjectID: project, OwnerID: owner, Visibility: in.Visibility, Version: version, Name: in.Name, Definition: in.Definition, ContentHash: payloadHash, UpdatedAt: now}
	return out, tx.Commit()
}
func validAssetViewID(id string) bool {
	if len(id) != 32 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'f' || c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}
