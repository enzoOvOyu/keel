package execution

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/enzoOvOyu/keel/internal/platform"
	"strings"
	"time"
	"unicode/utf8"
)

func (s *Service) RegisterTargets(ctx context.Context, a *Actor, projectID string, in RegisterInput, key, trace string) ([]SimTarget, error) {
	if !identifier.MatchString(in.Prefix) || in.Count < 1 || in.Count > 2000 {
		return nil, invalid("prefix must be an identifier; count must be 1..2000")
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = authorize(ctx, tx, a, projectID, "Admin", "SRE"); e != nil {
		return nil, e
	}
	idem, prior, e := idempotency(ctx, tx, a, "sim-targets:"+projectID, key, in)
	if e != nil {
		return nil, e
	}
	if prior != nil {
		return decode[[]SimTarget](prior)
	}
	rows := make([]SimTarget, 0, in.Count)
	now := time.Now().UTC().Truncate(time.Microsecond)
	for i := 0; i < in.Count; i++ {
		t := SimTarget{ID: platform.RandomID(), ProjectID: projectID, Name: fmt.Sprintf("%s-%04d", in.Prefix, i+1), Seed: in.Seed + int64(i), Revision: 1, ModelVersion: "sim-model-v1", CreatedAt: now, Mode: "sim"}
		rows = append(rows, t)
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO exec_sim_targets(id,project_id,name,seed,revision,model_version,created_at)
 SELECT id,project_id,name,seed,revision,model_version,created_at FROM jsonb_to_recordset($1::jsonb)
 AS x(id varchar,project_id varchar,name varchar,seed bigint,revision bigint,model_version varchar,created_at timestamptz) ON CONFLICT(project_id,name) DO NOTHING`, jsonBytes(rows))
	if e != nil {
		return nil, e
	}
	// Returning existing registrations as newly created would corrupt reproducibility.
	var count int
	if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM exec_sim_targets WHERE project_id=$1 AND id IN (SELECT jsonb_array_elements_text($2::jsonb))", projectID, jsonBytes(targetIDs(rows))).Scan(&count); e != nil {
		return nil, e
	}
	if count != in.Count {
		return nil, conflict("simulation prefix already exists; use another prefix")
	}
	if e = audit(ctx, tx, a.User.ID, projectID, "sim.targets.register", projectID, trace, map[string]any{"mode": "sim", "count": in.Count, "seed": in.Seed}); e != nil {
		return nil, e
	}
	if e = remember(ctx, tx, idem, in, rows); e != nil {
		return nil, e
	}
	return rows, tx.Commit()
}
func targetIDs(ts []SimTarget) []string {
	ids := make([]string, 0, len(ts))
	for _, t := range ts {
		ids = append(ids, t.ID)
	}
	return ids
}
func (s *Service) ListTargets(ctx context.Context, projectID string, p platform.Pagination) (platform.Page[SimTarget], error) {
	out := platform.NewPage[SimTarget](p)
	if e := p.Validate(); e != nil {
		return out, e
	}
	if e := s.DB.QueryRowContext(ctx, "SELECT count(*) FROM exec_sim_targets WHERE project_id=$1", projectID).Scan(&out.Total); e != nil {
		return out, e
	}
	rows, e := s.DB.QueryContext(ctx, "SELECT id,project_id,name,seed,revision,model_version,created_at FROM exec_sim_targets WHERE project_id=$1 ORDER BY name,id LIMIT $2 OFFSET $3", projectID, p.PageSize, p.Offset())
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var t SimTarget
		t.Mode = "sim"
		if e = rows.Scan(&t.ID, &t.ProjectID, &t.Name, &t.Seed, &t.Revision, &t.ModelVersion, &t.CreatedAt); e != nil {
			return out, e
		}
		out.Items = append(out.Items, t)
	}
	return out, rows.Err()
}

func (s *Service) Publish(ctx context.Context, a *Actor, projectID, flowID string, in PublishInput, key, trace string) (*FlowVersion, error) {
	return s.publishVersion(ctx, a, projectID, flowID, in, key, trace, "", "")
}
func (s *Service) publishVersion(ctx context.Context, a *Actor, projectID, flowID string, in PublishInput, key, trace, rollbackSource, changeReason string) (*FlowVersion, error) {
	d, e := in.Parse()
	if e != nil {
		return nil, e
	}
	if utf8.RuneCountInString(strings.TrimSpace(in.Name)) < 1 || utf8.RuneCountInString(in.Name) > 120 {
		return nil, invalid("flow name requires 1..120 characters")
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = authorize(ctx, tx, a, projectID, "Admin", "SRE"); e != nil {
		return nil, e
	}
	var payload any = in
	action := "flow.version.publish"
	scope := "flow.publish:"
	if rollbackSource != "" {
		action = "flow.version.rollback"
		scope = "flow.rollback:"
		payload = map[string]any{"input": in, "source_version_id": rollbackSource, "change_reason": changeReason}
		if e = verifyRollbackSource(ctx, tx, projectID, flowID, rollbackSource, d); e != nil {
			return nil, e
		}
	}
	idem, prior, e := idempotency(ctx, tx, a, scope+projectID+":"+flowID, key, payload)
	if e != nil {
		return nil, e
	}
	if prior != nil {
		v, e := decode[FlowVersion](prior)
		return &v, e
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	version := int64(1)
	if flowID == "" {
		if in.ExpectedVersion != 0 {
			return nil, invalid("new flow expected_version must be zero")
		}
		flowID = platform.RandomID()
		_, e = tx.ExecContext(ctx, "INSERT INTO exec_flows VALUES($1,$2,$3,1,$4,$5,$5) ON CONFLICT(project_id,name) DO NOTHING", flowID, projectID, strings.TrimSpace(in.Name), a.User.ID, now)
		if e != nil {
			return nil, e
		}
		var exists bool
		e = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM exec_flows WHERE id=$1)", flowID).Scan(&exists)
		if e != nil {
			return nil, e
		}
		if !exists {
			return nil, conflict("flow name already exists")
		}
	} else {
		var name string
		e = tx.QueryRowContext(ctx, "SELECT version,name FROM exec_flows WHERE id=$1 AND project_id=$2 FOR UPDATE", flowID, projectID).Scan(&version, &name)
		if errors.Is(e, sql.ErrNoRows) {
			return nil, failure(404, "NOT_FOUND", "Flow not found")
		}
		if e != nil {
			return nil, e
		}
		if in.ExpectedVersion != version {
			return nil, conflict("flow version changed")
		}
		if strings.TrimSpace(in.Name) != name {
			return nil, invalid("version publication cannot rename a flow")
		}
		var frozen []byte
		if e = tx.QueryRowContext(ctx, "SELECT definition FROM exec_flow_versions WHERE flow_id=$1 AND project_id=$2 AND version=$3", flowID, projectID, version).Scan(&frozen); e != nil {
			return nil, e
		}
		current, e := decode[Definition](frozen)
		if e != nil {
			return nil, e
		}
		if current.Validate() != nil {
			return nil, invalid("owned or unsupported workflows cannot be modified through the general Flow catalog")
		}
		version++
		if _, e = tx.ExecContext(ctx, "UPDATE exec_flows SET version=$2,updated_at=$3 WHERE id=$1", flowID, version, now); e != nil {
			return nil, e
		}
	}
	v := &FlowVersion{ID: platform.RandomID(), FlowID: flowID, ProjectID: projectID, Name: strings.TrimSpace(in.Name), Version: version, Definition: d, YAMLSource: in.YAMLSource, ContentHash: hash(jsonBytes(d)), CreatedAt: now}
	_, e = tx.ExecContext(ctx, "INSERT INTO exec_flow_versions VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)", v.ID, flowID, projectID, version, jsonBytes(d), v.YAMLSource, v.ContentHash, a.User.ID, now)
	if e != nil {
		return nil, e
	}
	mode := "sim"
	if d.APIVersion == "keel.host/v1" {
		mode = "host"
	}
	if e = audit(ctx, tx, a.User.ID, projectID, action, v.ID, trace, map[string]any{"version": version, "content_hash": v.ContentHash, "mode": mode, "api_version": d.APIVersion, "source_version_id": rollbackSource, "change_reason": changeReason}); e != nil {
		return nil, e
	}
	if e = remember(ctx, tx, idem, payload, v); e != nil {
		return nil, e
	}
	return v, tx.Commit()
}

type scanner interface{ Scan(...any) error }

func scanFlow(r scanner) (FlowVersion, error) {
	var v FlowVersion
	var raw []byte
	e := r.Scan(&v.ID, &v.FlowID, &v.ProjectID, &v.Name, &v.Version, &raw, &v.YAMLSource, &v.ContentHash, &v.CreatedAt)
	if e == nil {
		v.Definition, e = decode[Definition](raw)
	}
	return v, e
}

const flowColumns = "v.id,v.flow_id,v.project_id,f.name,v.version,v.definition,v.yaml_source,v.content_hash,v.created_at"

func (s *Service) ListFlows(ctx context.Context, projectID, flowID string, p platform.Pagination) (platform.Page[FlowVersion], error) {
	out := platform.NewPage[FlowVersion](p)
	if e := p.Validate(); e != nil {
		return out, e
	}
	filter := " FROM exec_flow_versions v JOIN exec_flows f ON f.id=v.flow_id WHERE v.project_id=$1 AND ($2='' AND v.version=f.version OR v.flow_id=$2)"
	if e := s.DB.QueryRowContext(ctx, "SELECT count(*)"+filter, projectID, flowID).Scan(&out.Total); e != nil {
		return out, e
	}
	rows, e := s.DB.QueryContext(ctx, "SELECT "+flowColumns+filter+" ORDER BY v.created_at DESC,v.id DESC LIMIT $3 OFFSET $4", projectID, flowID, p.PageSize, p.Offset())
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		v, e := scanFlow(rows)
		if e != nil {
			return out, e
		}
		out.Items = append(out.Items, v)
	}
	return out, rows.Err()
}
