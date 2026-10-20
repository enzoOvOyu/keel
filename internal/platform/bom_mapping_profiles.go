package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// BOMMappingProfile freezes source-header to canonical-field mappings per
// project and version. Preview accepts only approved versions; edits create a
// new version so prior BOM packages retain their original interpretation.
type BOMMappingProfile struct {
	ID            string                       `json:"id"`
	ProjectID     string                       `json:"project_id"`
	VersionRef    string                       `json:"version_ref"`
	State         string                       `json:"state"`
	ServerColumns map[string]map[string]string `json:"server_columns"`
	FabricColumns map[string]map[string]string `json:"fabric_columns"`
	CreatedAt     time.Time                    `json:"created_at"`
	ApprovedAt    *time.Time                   `json:"approved_at,omitempty"`
}

type CreateBOMMappingProfileInput struct {
	VersionRef    string                       `json:"version_ref"`
	ServerColumns map[string]map[string]string `json:"server_columns"`
	FabricColumns map[string]map[string]string `json:"fabric_columns"`
}

var bomProfileVersionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,119}$`)

func validateBOMMappingProfile(input CreateBOMMappingProfileInput) error {
	if !bomProfileVersionPattern.MatchString(input.VersionRef) {
		return apiError(400, "VALIDATION_ERROR", "Invalid BOM mapping profile version")
	}
	for _, domain := range []struct {
		name    string
		mapping map[string]map[string]string
	}{{"server", input.ServerColumns}, {"fabric", input.FabricColumns}} {
		if len(domain.mapping) == 0 {
			return apiError(400, "VALIDATION_ERROR", "Both BOM mapping domains are required")
		}
		for file, columns := range domain.mapping {
			want, ok := requiredBOMFiles[file]
			if !ok || want.domain != domain.name {
				return apiError(400, "VALIDATION_ERROR", "Unknown BOM mapping file")
			}
			canonical := map[string]bool{}
			for source, target := range columns {
				source = strings.TrimSpace(source)
				target = strings.TrimSpace(target)
				if source == "" || target == "" || len(source) > 120 || len(target) > 120 || canonical[target] || !isCanonicalBOMColumn(file, target) {
					return apiError(400, "VALIDATION_ERROR", "Invalid or duplicate BOM column mapping")
				}
				canonical[target] = true
			}
		}
	}
	for file, want := range requiredBOMFiles {
		columns := input.ServerColumns[file]
		if want.domain == "fabric" {
			columns = input.FabricColumns[file]
		}
		if columns == nil {
			return apiError(400, "VALIDATION_ERROR", "A mapping entry is required for all seven BOM files")
		}
	}
	return nil
}

func (s *Store) CreateBOMMappingProfile(ctx context.Context, a *AuthSession, project *ProjectSummary, input CreateBOMMappingProfileInput, trace string) (*BOMMappingProfile, error) {
	if e := validateBOMMappingProfile(input); e != nil {
		return nil, e
	}
	serverJSON, e := json.Marshal(input.ServerColumns)
	if e != nil {
		return nil, e
	}
	fabricJSON, e := json.Marshal(input.FabricColumns)
	if e != nil {
		return nil, e
	}
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireProjectAdmin(ctx, tx, a, project.ID); e != nil {
		return nil, e
	}
	out := &BOMMappingProfile{ID: RandomID(), ProjectID: project.ID, VersionRef: input.VersionRef, State: "draft", ServerColumns: input.ServerColumns, FabricColumns: input.FabricColumns, CreatedAt: time.Now().UTC().Truncate(time.Microsecond)}
	_, e = tx.ExecContext(ctx, `INSERT INTO bom_mapping_profiles(id,project_id,version_ref,state,server_columns,fabric_columns,created_by,created_at) VALUES($1,$2,$3,'draft',$4,$5,$6,$7)`, out.ID, project.ID, out.VersionRef, serverJSON, fabricJSON, a.User.ID, out.CreatedAt)
	if e != nil {
		var pg *pgconn.PgError
		if errors.As(e, &pg) && pg.Code == "23505" {
			return nil, apiError(409, "BOM_MAPPING_VERSION_EXISTS", "BOM mapping version already exists")
		}
		return nil, e
	}
	if e = appendAudit(ctx, tx, a.User.ID, project.ID, "bom.mapping.create", out.ID, trace, map[string]any{"version_ref": out.VersionRef}); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return out, nil
}

func (s *Store) ApproveBOMMappingProfile(ctx context.Context, a *AuthSession, project *ProjectSummary, versionRef, trace string) (*BOMMappingProfile, error) {
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireProjectAdmin(ctx, tx, a, project.ID); e != nil {
		return nil, e
	}
	out := &BOMMappingProfile{ServerColumns: map[string]map[string]string{}, FabricColumns: map[string]map[string]string{}}
	var serverJSON, fabricJSON []byte
	var approved sql.NullTime
	e = tx.QueryRowContext(ctx, `SELECT id,state,server_columns,fabric_columns,created_at,approved_at FROM bom_mapping_profiles WHERE project_id=$1 AND version_ref=$2 FOR UPDATE`, project.ID, versionRef).Scan(&out.ID, &out.State, &serverJSON, &fabricJSON, &out.CreatedAt, &approved)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if e != nil {
		return nil, e
	}
	if out.State != "draft" {
		return nil, apiError(409, "BOM_MAPPING_NOT_DRAFT", "Only draft BOM mappings can be approved")
	}
	if e = json.Unmarshal(serverJSON, &out.ServerColumns); e != nil {
		return nil, e
	}
	if e = json.Unmarshal(fabricJSON, &out.FabricColumns); e != nil {
		return nil, e
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if _, e = tx.ExecContext(ctx, `UPDATE bom_mapping_profiles SET state='approved',approved_by=$2,approved_at=$3 WHERE id=$1`, out.ID, a.User.ID, now); e != nil {
		return nil, e
	}
	if e = appendAudit(ctx, tx, a.User.ID, project.ID, "bom.mapping.approve", out.ID, trace, map[string]any{"version_ref": versionRef}); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	out.ProjectID = project.ID
	out.VersionRef = versionRef
	out.State = "approved"
	out.ApprovedAt = &now
	return out, nil
}

func (s *Store) GetApprovedBOMMappingProfile(ctx context.Context, a *AuthSession, project *ProjectSummary, versionRef string) (*BOMMappingProfile, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireProjectMember(ctx, tx, a, project.ID); e != nil {
		return nil, e
	}
	out := &BOMMappingProfile{ProjectID: project.ID, VersionRef: versionRef, State: "approved"}
	var serverJSON, fabricJSON []byte
	var approved time.Time
	e = tx.QueryRowContext(ctx, `SELECT id,server_columns,fabric_columns,created_at,approved_at FROM bom_mapping_profiles WHERE project_id=$1 AND version_ref=$2 AND state='approved'`, project.ID, versionRef).Scan(&out.ID, &serverJSON, &fabricJSON, &out.CreatedAt, &approved)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, apiError(409, "BOM_MAPPING_NOT_APPROVED", "Approved BOM mapping profile not found")
	}
	if e != nil {
		return nil, e
	}
	if e = json.Unmarshal(serverJSON, &out.ServerColumns); e != nil {
		return nil, e
	}
	if e = json.Unmarshal(fabricJSON, &out.FabricColumns); e != nil {
		return nil, e
	}
	out.ApprovedAt = &approved
	return out, nil
}
