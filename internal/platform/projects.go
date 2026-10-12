package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/enzoOvOyu/keel/ent"
	"github.com/enzoOvOyu/keel/ent/membership"
	"github.com/enzoOvOyu/keel/ent/project"
	"github.com/jackc/pgx/v5/pgconn"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

type ProjectSummary struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	Description string    `json:"description"`
	State       string    `json:"state"`
	Role        string    `json:"role"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Version     int       `json:"version"`
}
type CreateProjectInput struct {
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
}

var slugPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,62}[a-z0-9])?$`)

func projectSummary(p *ent.Project, role string) ProjectSummary {
	return ProjectSummary{ID: p.ID, Name: p.Name, Slug: p.Slug, Description: p.Description, State: string(p.State), Role: role, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt, Version: p.Version}
}
func (s *Store) ListProjects(ctx context.Context, a *AuthSession) ([]ProjectSummary, error) {
	roles := map[string]string{}
	q := s.Ent.Project.Query().Order(ent.Asc(project.FieldCreatedAt), ent.Asc(project.FieldID))
	if !a.User.IsAdmin {
		members, e := s.Ent.Membership.Query().Where(membership.UserID(a.User.ID)).All(ctx)
		if e != nil {
			return nil, e
		}
		ids := []string{}
		for _, m := range members {
			roles[m.ProjectID] = string(m.Role)
			ids = append(ids, m.ProjectID)
		}
		if len(ids) == 0 {
			return []ProjectSummary{}, nil
		}
		q = q.Where(project.IDIn(ids...))
	}
	rows, e := q.All(ctx)
	if e != nil {
		return nil, e
	}
	out := make([]ProjectSummary, 0, len(rows))
	for _, p := range rows {
		role := roles[p.ID]
		if a.User.IsAdmin {
			role = "Admin"
		}
		out = append(out, projectSummary(p, role))
	}
	return out, nil
}
func (s *Store) GetProject(ctx context.Context, a *AuthSession, id string) (*ProjectSummary, error) {
	role := "Admin"
	if !a.User.IsAdmin {
		m, e := s.Ent.Membership.Query().Where(membership.UserID(a.User.ID), membership.ProjectID(id)).Only(ctx)
		if ent.IsNotFound(e) {
			return nil, ErrNotFound
		}
		if e != nil {
			return nil, e
		}
		role = string(m.Role)
	}
	p, e := s.Ent.Project.Get(ctx, id)
	if ent.IsNotFound(e) {
		return nil, ErrNotFound
	}
	if e != nil {
		return nil, e
	}
	out := projectSummary(p, role)
	return &out, nil
}
func (s *Store) CreateProject(ctx context.Context, a *AuthSession, input CreateProjectInput, key, trace string) (*ProjectSummary, error) {
	if !a.User.IsAdmin {
		return nil, ErrForbidden
	}
	if len(key) < 1 || len(key) > 128 || strings.TrimSpace(key) != key {
		return nil, apiError(400, "VALIDATION_ERROR", "Idempotency-Key is required (1..128 characters)")
	}
	for _, r := range key {
		if r < 33 || r > 126 {
			return nil, apiError(400, "VALIDATION_ERROR", "Invalid Idempotency-Key")
		}
	}
	request, _ := json.Marshal(input)
	input.Name = strings.TrimSpace(input.Name)
	if utf8.RuneCountInString(input.Name) < 1 || utf8.RuneCountInString(input.Name) > 120 || !slugPattern.MatchString(input.Slug) || utf8.RuneCountInString(input.Description) > 2000 {
		return nil, apiError(400, "VALIDATION_ERROR", "Invalid project name, slug, or description")
	}
	requestHash := digest(string(request))
	idempotencyID := digest("project.create:" + a.User.ID + ":" + key)
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	// Recheck authority inside the same transaction as the write; CLI revocations lock this row.
	var enabled, admin bool
	var securityVersion int
	e = tx.QueryRowContext(ctx, "SELECT enabled,is_admin,security_version FROM users WHERE id=$1 FOR SHARE", a.User.ID).Scan(&enabled, &admin, &securityVersion)
	if e != nil {
		return nil, e
	}
	if !enabled || securityVersion != a.SecurityVersion {
		return nil, ErrUnauthenticated
	}
	if !admin {
		return nil, ErrForbidden
	}
	// Pin the authenticated session through commit. A completed logout must win
	// over a request that was authenticated earlier but has not performed its write.
	var sessionID string
	now := time.Now().UTC()
	e = tx.QueryRowContext(ctx, "SELECT id FROM sessions WHERE id=$1 AND user_id=$2 AND security_version=$3 AND revoked_at IS NULL AND expires_at>$4 AND last_seen_at>$5 FOR SHARE", a.ID, a.User.ID, a.SecurityVersion, now, now.Add(-IdleTimeout)).Scan(&sessionID)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, ErrUnauthenticated
	}
	if e != nil {
		return nil, e
	}
	if e = lockKey(ctx, tx, idempotencyID); e != nil {
		return nil, e
	}
	var priorHash string
	var prior []byte
	e = tx.QueryRowContext(ctx, "SELECT request_hash,response FROM idempotencies WHERE id=$1", idempotencyID).Scan(&priorHash, &prior)
	if e == nil {
		if priorHash != requestHash {
			return nil, apiError(409, "IDEMPOTENCY_CONFLICT", "Idempotency key was already used for a different request")
		}
		var result ProjectSummary
		if e = json.Unmarshal(prior, &result); e != nil {
			return nil, e
		}
		return &result, nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return nil, e
	}
	now = time.Now().UTC().Truncate(time.Microsecond)
	result := ProjectSummary{ID: RandomID(), Name: input.Name, Slug: input.Slug, Description: input.Description, State: "planning", Role: "Admin", CreatedAt: now, UpdatedAt: now, Version: 1}
	_, e = tx.ExecContext(ctx, "INSERT INTO projects(id,name,slug,description,state,created_by,version,created_at,updated_at) VALUES($1,$2,$3,$4,'planning',$5,1,$6,$6)", result.ID, result.Name, result.Slug, result.Description, a.User.ID, now)
	if e != nil {
		var pg *pgconn.PgError
		if errors.As(e, &pg) && pg.Code == "23505" {
			return nil, apiError(409, "PROJECT_SLUG_EXISTS", "Project slug already exists")
		}
		return nil, e
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO memberships(id,user_id,project_id,role,created_at) VALUES($1,$2,$3,'Admin',$4)", RandomID(), a.User.ID, result.ID, now); e != nil {
		return nil, e
	}
	if e = appendAudit(ctx, tx, a.User.ID, result.ID, "project.create", result.ID, trace, map[string]any{"before_version": 0, "after_version": 1, "state": "planning"}); e != nil {
		return nil, e
	}
	data, _ := json.Marshal(result)
	if _, e = tx.ExecContext(ctx, "INSERT INTO idempotencies(id,actor_id,request_hash,response,created_at) VALUES($1,$2,$3,$4,$5)", idempotencyID, a.User.ID, requestHash, data, now); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return &result, nil
}
