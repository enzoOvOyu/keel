package platform

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

type BOMPackagePreview struct {
	Manifest   BOMPackageManifest `json:"manifest"`
	Issues     []BOMPackageIssue  `json:"issues"`
	FileHashes map[string]string  `json:"file_hashes"`
	CanCommit  bool               `json:"can_commit"`
	Idempotent bool               `json:"idempotent"`
}

// PreviewBOMPackage reads the complete archive and the current project facts.
// It never stores an upload or mutates the database. Commit must repeat these
// checks inside its own transaction to close the preview/commit race.
func (s *Store) PreviewBOMPackage(ctx context.Context, a *AuthSession, project *ProjectSummary, files map[string][]byte) (*BOMPackagePreview, error) {
	pkg, err := ValidateBOMPackage(files)
	if err != nil {
		return nil, apiError(400, "BOM_INVALID_MANIFEST", err.Error())
	}
	out := &BOMPackagePreview{Manifest: pkg.Manifest, Issues: pkg.Issues, FileHashes: pkg.FileHashes}
	add := func(file string, line int, field, code string) {
		pkg.Issues = append(pkg.Issues, BOMPackageIssue{Severity: "BLOCK", File: file, BOMLineNo: line, Field: field, Code: code})
	}
	if pkg.Manifest.ProjectID != project.ID {
		add("manifest.json", 0, "project_id", "project_mismatch")
	}
	profile, err := s.GetApprovedBOMMappingProfile(ctx, a, project, pkg.Manifest.ProfileVersionRef)
	if err != nil {
		var apiErr *Error
		if !errors.As(err, &apiErr) || apiErr.Code != "BOM_MAPPING_NOT_APPROVED" {
			return nil, err
		}
		add("manifest.json", 0, "profile_version_ref", "mapping_not_approved")
	} else {
		ApplyBOMMappingProfile(pkg, profile)
	}
	tx, err := s.beginControlRead(ctx, a)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = requireProjectMember(ctx, tx, a, project.ID); err != nil {
		return nil, err
	}
	var existingHash string
	err = tx.QueryRowContext(ctx, `SELECT content_hash FROM bom_import_packages WHERE project_id=$1 AND bom_import_batch_id=$2`, project.ID, pkg.Manifest.BatchID).Scan(&existingHash)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if err == nil {
		if existingHash != pkg.Manifest.ContentHash {
			add("manifest.json", 0, "bom_import_batch_id", "batch_hash_conflict")
		} else {
			out.Idempotent = true
		}
	}
	for file, rows := range pkg.Rows {
		if _, required := requiredBOMFiles[file]; !required {
			continue
		}
		for _, row := range rows {
			modelRef := strings.TrimSpace(row.Values["model_ref"])
			if modelRef == "" {
				continue
			}
			var kind, scope, state string
			err = tx.QueryRowContext(ctx, `SELECT m.kind,m.scope_key,m.state FROM device_model_versions v JOIN device_models m ON m.id=v.model_id WHERE v.id=$1`, modelRef).Scan(&kind, &scope, &state)
			if errors.Is(err, sql.ErrNoRows) {
				add(file, row.Line, "model_ref", "model_version_not_found")
				continue
			}
			if err != nil {
				return nil, err
			}
			if scope != "global" && scope != "project:"+project.ID {
				add(file, row.Line, "model_ref", "model_version_not_in_project")
			}
			if state != "active" {
				add(file, row.Line, "model_ref", "model_retired")
			}
			if file == "server/servers.csv" && kind != "server" || file == "fabric/switches.csv" && kind != "switch" {
				add(file, row.Line, "model_ref", "model_kind_mismatch")
			}
		}
	}
	out.Issues = pkg.Issues
	out.CanCommit = !out.Idempotent
	for _, issue := range out.Issues {
		if issue.Severity == "BLOCK" {
			out.CanCommit = false
			break
		}
	}
	return out, tx.Commit()
}
