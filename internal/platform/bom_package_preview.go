package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

type BOMPackagePreview struct {
	Manifest     BOMPackageManifest `json:"manifest"`
	Issues       []BOMPackageIssue  `json:"issues"`
	FileHashes   map[string]string  `json:"file_hashes"`
	Derivations  map[string]string  `json:"derivations"`
	CanCommit    bool               `json:"can_commit"`
	Idempotent   bool               `json:"idempotent"`
	PreviewToken string             `json:"preview_token,omitempty"`
}

// PreviewBOMPackage never stores an upload or mutates the database. Commit
// calls the same analyzer inside its write transaction before changing facts.
func (s *Store) PreviewBOMPackage(ctx context.Context, a *AuthSession, project *ProjectSummary, files map[string][]byte) (*BOMPackagePreview, error) {
	tx, err := s.beginControlRead(ctx, a)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = requireProjectMember(ctx, tx, a, project.ID); err != nil {
		return nil, err
	}
	_, out, err := analyzeBOMPackageTx(ctx, tx, project, files)
	if err != nil {
		return nil, err
	}
	if out.CanCommit {
		out.PreviewToken = bomPreviewToken(a, project.ID, files)
	}
	return out, tx.Commit()
}

func analyzeBOMPackageTx(ctx context.Context, tx *sql.Tx, project *ProjectSummary, files map[string][]byte) (*BOMPackage, *BOMPackagePreview, error) {
	pkg, err := ValidateBOMPackage(files)
	if err != nil {
		return nil, nil, apiError(400, "BOM_INVALID_MANIFEST", err.Error())
	}
	out := &BOMPackagePreview{Manifest: pkg.Manifest, FileHashes: pkg.FileHashes, Derivations: map[string]string{"port.sub_type": "published_parent_model"}}
	add := func(file string, line int, field, code string) {
		pkg.Issues = append(pkg.Issues, BOMPackageIssue{Severity: "BLOCK", File: file, BOMLineNo: line, Field: field, Code: code})
	}
	if pkg.Manifest.ProjectID != project.ID || !strings.EqualFold(pkg.Manifest.ProjectCode, project.Slug) {
		add("manifest.json", 0, "project_id", "project_mismatch")
	}
	// Until project-template material minimums are persisted, a standard package
	// must at least anchor both delivery domains and one planned connection.
	for _, file := range []string{"server/servers.csv", "server/server_ports.csv", "fabric/switches.csv", "fabric/switch_ports.csv", "fabric/cables.csv"} {
		if len(pkg.Rows[file]) == 0 {
			add(file, 0, "", "required_material_missing")
		}
	}
	var serverJSON, fabricJSON []byte
	err = tx.QueryRowContext(ctx, `SELECT server_columns,fabric_columns FROM bom_mapping_profiles WHERE project_id=$1 AND version_ref=$2 AND state='approved'`, project.ID, pkg.Manifest.ProfileVersionRef).Scan(&serverJSON, &fabricJSON)
	if errors.Is(err, sql.ErrNoRows) {
		add("manifest.json", 0, "profile_version_ref", "mapping_not_approved")
	} else if err != nil {
		return nil, nil, err
	} else {
		profile := &BOMMappingProfile{}
		if err = json.Unmarshal(serverJSON, &profile.ServerColumns); err != nil {
			return nil, nil, err
		}
		if err = json.Unmarshal(fabricJSON, &profile.FabricColumns); err != nil {
			return nil, nil, err
		}
		ApplyBOMMappingProfile(pkg, profile)
	}
	var existingHash string
	err = tx.QueryRowContext(ctx, `SELECT content_hash FROM bom_import_packages WHERE project_id=$1 AND bom_import_batch_id=$2`, project.ID, pkg.Manifest.BatchID).Scan(&existingHash)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, nil, err
	}
	if err == nil {
		if existingHash != pkg.Manifest.ContentHash {
			add("manifest.json", 0, "bom_import_batch_id", "batch_hash_conflict")
		} else {
			out.Idempotent = true
		}
	}
	type modelFact struct {
		kind, scope, state string
		found              bool
	}
	modelCache := map[string]modelFact{}
	for file, rows := range pkg.Rows {
		if _, required := requiredBOMFiles[file]; !required {
			continue
		}
		for _, row := range rows {
			modelRef := strings.TrimSpace(row.Values["model_ref"])
			if modelRef == "" {
				continue
			}
			model, cached := modelCache[modelRef]
			if !cached {
				err = tx.QueryRowContext(ctx, `SELECT m.kind,m.scope_key,m.state FROM device_model_versions v JOIN device_models m ON m.id=v.model_id WHERE v.id=$1`, modelRef).Scan(&model.kind, &model.scope, &model.state)
				if err != nil && !errors.Is(err, sql.ErrNoRows) {
					return nil, nil, err
				}
				model.found = err == nil
				modelCache[modelRef] = model
			}
			if !model.found {
				add(file, row.Line, "model_ref", "model_version_not_found")
				continue
			}
			if model.scope != "global" && model.scope != "project:"+project.ID {
				add(file, row.Line, "model_ref", "model_version_not_in_project")
			}
			if model.state != "active" {
				add(file, row.Line, "model_ref", "model_retired")
			}
			wantKind := ""
			switch file {
			case "server/servers.csv":
				wantKind = "server"
			case "fabric/switches.csv":
				wantKind = "switch"
			case "server/server_components.csv":
				wantKind = row.Values["category"]
			case "fabric/optic_modules.csv":
				wantKind = "optic_module"
			}
			matchesKind := model.kind == wantKind || (wantKind == "server" && model.kind == "compute-tray")
			if wantKind != "" && !matchesKind {
				add(file, row.Line, "model_ref", "model_kind_mismatch")
			}
		}
	}
	engineeringIssues, engineeringErr := validateSwitchPortCapabilities(ctx, tx, project.ID, pkg)
	if engineeringErr != nil {
		return nil, nil, engineeringErr
	}
	pkg.Issues = append(pkg.Issues, engineeringIssues...)
	opticIssues, opticErr := validateOpticCapabilities(ctx, tx, project.ID, pkg)
	if opticErr != nil {
		return nil, nil, opticErr
	}
	pkg.Issues = append(pkg.Issues, opticIssues...)
	hardwareIssues, hardwareErr := validateServerModelHardware(ctx, tx, project.ID, pkg)
	if hardwareErr != nil {
		return nil, nil, hardwareErr
	}
	pkg.Issues = append(pkg.Issues, hardwareIssues...)
	railIssues, railErr := validateServerRailScale(ctx, tx, project.ID, pkg)
	if railErr != nil {
		return nil, nil, railErr
	}
	pkg.Issues = append(pkg.Issues, railIssues...)
	networkIssues, networkErr := validateNetworkEngineering(ctx, tx, project.ID, pkg)
	if networkErr != nil {
		return nil, nil, networkErr
	}
	pkg.Issues = append(pkg.Issues, networkIssues...)
	pkg.Issues = append(pkg.Issues, validatePackageCableRails(pkg)...)
	portSubtypeIssues, portSubtypeErr := deriveBOMPortSubtypes(ctx, tx, project.ID, pkg)
	if portSubtypeErr != nil {
		return nil, nil, portSubtypeErr
	}
	pkg.Issues = append(pkg.Issues, portSubtypeIssues...)
	identityIssues, identityErr := validateSwitchIdentityMACs(ctx, tx, pkg)
	if identityErr != nil {
		return nil, nil, identityErr
	}
	pkg.Issues = append(pkg.Issues, identityIssues...)
	hasBlock := false
	for _, issue := range pkg.Issues {
		if issue.Severity == "BLOCK" {
			hasBlock = true
			break
		}
	}
	if !hasBlock && !out.Idempotent {
		materials, planErr := buildBOMMaterials(ctx, tx, project.ID, pkg)
		if planErr != nil {
			var buildErr *BOMBuildError
			if errors.As(planErr, &buildErr) {
				pkg.Issues = append(pkg.Issues, buildErr.Issue)
			} else {
				return nil, nil, planErr
			}
		} else {
			macIssues, err := validateBOMMACPrefixes(ctx, tx, materials)
			if err != nil {
				return nil, nil, err
			}
			pkg.Issues = append(pkg.Issues, macIssues...)
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
	return pkg, out, nil
}
