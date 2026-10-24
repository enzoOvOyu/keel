package platform

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type LegacyBOMPreview struct {
	BatchID      string            `json:"bom_import_batch_id"`
	ContentHash  string            `json:"content_hash"`
	Machines     int               `json:"machines"`
	Ports        int               `json:"ports"`
	Issues       []BOMPackageIssue `json:"issues"`
	CanCommit    bool              `json:"can_commit"`
	Idempotent   bool              `json:"idempotent"`
	PreviewToken string            `json:"preview_token,omitempty"`
}

func legacyBOMFiles(batch string, data []byte) map[string][]byte {
	manifest, _ := json.Marshal(map[string]string{"schema_version": "legacy-wide/v1", "bom_import_batch_id": batch})
	return map[string][]byte{"manifest.json": manifest, "legacy/wide.csv": data}
}

func analyzeLegacyBOMTx(ctx context.Context, tx *sql.Tx, project *ProjectSummary, batch string, data []byte) (*BOMPackage, *LegacyBOMRows, *LegacyBOMPreview, error) {
	match := packageBatchID.FindStringSubmatch(batch)
	if match == nil || !strings.EqualFold(match[1], project.Slug) || len(batch) > 160 {
		return nil, nil, nil, apiError(400, "BOM_BATCH_INVALID", "Batch ID must name this project and follow BOM-BATCH-<project>-<YYYYMMDD>-<seq>")
	}
	if _, err := time.Parse("20060102", match[2]); err != nil {
		return nil, nil, nil, apiError(400, "BOM_BATCH_INVALID", "Invalid batch date")
	}
	folded, err := ParseLegacyBOMCSV(data)
	if err != nil {
		return nil, nil, nil, apiError(400, "BOM_LEGACY_INVALID", err.Error())
	}
	files := legacyBOMFiles(batch, data)
	hash := BOMContentHash(files)
	out := &LegacyBOMPreview{BatchID: batch, ContentHash: hash, Machines: len(folded.Input.Machines), Ports: folded.Rows, Issues: append([]BOMPackageIssue{}, folded.Issues...)}
	var existingHash, schema string
	err = tx.QueryRowContext(ctx, `SELECT content_hash,schema_version FROM bom_import_packages WHERE project_id=$1 AND bom_import_batch_id=$2`, project.ID, batch).Scan(&existingHash, &schema)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, nil, nil, err
	}
	if err == nil {
		if existingHash != hash || schema != "legacy-wide/v1" {
			out.Issues = append(out.Issues, BOMPackageIssue{Severity: "BLOCK", File: "legacy/wide.csv", Code: "batch_hash_conflict"})
		} else {
			out.Idempotent = true
		}
	}
	pkg := &BOMPackage{Manifest: BOMPackageManifest{SchemaVersion: "legacy-wide/v1", BatchID: batch, ProjectCode: project.Slug, ProjectID: project.ID, DeclaredBy: "server_team", ContentHash: hash, RowCount: folded.Rows}, Rows: map[string][]BOMPackageRow{}, FileHashes: map[string]string{}}
	digest := sha256.Sum256(data)
	pkg.FileHashes["legacy/wide.csv"] = hex.EncodeToString(digest[:])
	modelVersions := map[string]string{}
	for _, m := range folded.Input.Machines {
		if _, ok := modelVersions[m.ModelName]; ok {
			continue
		}
		var version string
		err = tx.QueryRowContext(ctx, `SELECT v.id FROM device_models m JOIN device_model_versions v ON v.model_id=m.id WHERE m.name=$1 AND m.kind='server' AND m.state='active' AND m.scope_key IN('global',$2) ORDER BY CASE WHEN m.scope_key=$2 THEN 0 ELSE 1 END,v.version DESC LIMIT 1`, m.ModelName, "project:"+project.ID).Scan(&version)
		if errors.Is(err, sql.ErrNoRows) {
			out.Issues = append(out.Issues, BOMPackageIssue{Severity: "BLOCK", File: "legacy/wide.csv", BOMLineNo: folded.ServerLines[m.SerialNumber], Field: "model_name", Code: "model_version_not_found"})
			continue
		}
		if err != nil {
			return nil, nil, nil, err
		}
		modelVersions[m.ModelName] = version
	}
	// Assign new globally unique revision line numbers. The raw CSV source line
	// stays in each snapshot so corrections can be traced to the supplied sheet.
	line := 0
	for _, m := range folded.Input.Machines {
		line++
		v := map[string]string{"sn": m.SerialNumber, "rack": m.Rack, "u": m.UnitPosition, "hostname": m.Hostname, "ip_hint": m.IpReference, "model_ref": modelVersions[m.ModelName], "sub_type": m.ModelName, "source_of_record": "design_bom"}
		pkg.Rows["server/servers.csv"] = append(pkg.Rows["server/servers.csv"], BOMPackageRow{File: "legacy/wide.csv", Line: line, Values: v})
	}
	for _, m := range folded.Input.Machines {
		for _, p := range m.Ports {
			line++
			synthetic := "LEGACY-MAC-" + strings.ReplaceAll(p.MacAddress, ":", "")
			v := map[string]string{"parent_sn": m.SerialNumber, "category": "nic", "serial": synthetic, "slot": p.PortName, "sub_type": "legacy-mac-identity", "mac": p.MacAddress, "legacy_source_line": fmt.Sprint(folded.PortLines[p.MacAddress])}
			pkg.Rows["server/server_components.csv"] = append(pkg.Rows["server/server_components.csv"], BOMPackageRow{File: "legacy/wide.csv", Line: line, Values: v})
		}
	}
	for _, m := range folded.Input.Machines {
		for _, p := range m.Ports {
			line++
			synthetic := "LEGACY-MAC-" + strings.ReplaceAll(p.MacAddress, ":", "")
			v := map[string]string{"parent_ref": synthetic, "port_index": p.PortName, "port_name": p.PortName, "mac": p.MacAddress, "rail": fmt.Sprint(p.Rail), "legacy_source_line": fmt.Sprint(folded.PortLines[p.MacAddress])}
			pkg.Rows["server/server_ports.csv"] = append(pkg.Rows["server/server_ports.csv"], BOMPackageRow{File: "legacy/wide.csv", Line: line, Values: v})
		}
	}
	hardwareIssues, hardwareErr := validateServerModelHardware(ctx, tx, project.ID, pkg)
	if hardwareErr != nil {
		return nil, nil, nil, hardwareErr
	}
	for i := range hardwareIssues {
		for _, row := range pkg.Rows["server/servers.csv"] {
			if row.Line == hardwareIssues[i].BOMLineNo {
				hardwareIssues[i].BOMLineNo = folded.ServerLines[row.Values["sn"]]
				break
			}
		}
	}
	out.Issues = append(out.Issues, hardwareIssues...)
	if !out.Idempotent && modelVersionsComplete(folded, modelVersions) {
		materials, buildErr := buildBOMMaterials(ctx, tx, project.ID, pkg)
		err = buildErr
		if err != nil {
			var build *BOMBuildError
			if errors.As(err, &build) {
				for _, rows := range pkg.Rows {
					for _, row := range rows {
						if row.Line == build.Issue.BOMLineNo {
							if original, parseErr := strconv.Atoi(row.Values["legacy_source_line"]); parseErr == nil {
								build.Issue.BOMLineNo = original
							}
						}
					}
				}
				out.Issues = append(out.Issues, build.Issue)
			} else {
				return nil, nil, nil, err
			}
		} else {
			macIssues, macErr := validateBOMMACPrefixes(ctx, tx, materials)
			if macErr != nil {
				return nil, nil, nil, macErr
			}
			out.Issues = append(out.Issues, macIssues...)
		}
	}
	hostnames := map[string]string{}
	for _, m := range folded.Input.Machines {
		if prior, duplicate := hostnames[m.Hostname]; duplicate && prior != m.SerialNumber {
			out.Issues = append(out.Issues, BOMPackageIssue{Severity: "BLOCK", File: "legacy/wide.csv", BOMLineNo: folded.ServerLines[m.SerialNumber], Field: "hostname", Code: "hostname_conflict"})
		}
		hostnames[m.Hostname] = m.SerialNumber
		var other string
		err = tx.QueryRowContext(ctx, `SELECT m.serial_number FROM project_assets a JOIN machines m ON m.id=a.machine_id WHERE a.project_id=$1 AND a.hostname=$2 AND m.serial_number<>$3`, project.ID, m.Hostname, m.SerialNumber).Scan(&other)
		if err == nil {
			out.Issues = append(out.Issues, BOMPackageIssue{Severity: "BLOCK", File: "legacy/wide.csv", BOMLineNo: folded.ServerLines[m.SerialNumber], Field: "hostname", Code: "hostname_conflict"})
		} else if !errors.Is(err, sql.ErrNoRows) {
			return nil, nil, nil, err
		}
	}
	out.CanCommit = true
	for _, issue := range out.Issues {
		if issue.Severity == "BLOCK" {
			out.CanCommit = false
			break
		}
	}
	return pkg, folded, out, nil
}
func modelVersionsComplete(f *LegacyBOMRows, versions map[string]string) bool {
	for _, m := range f.Input.Machines {
		if versions[m.ModelName] == "" {
			return false
		}
	}
	return true
}
func (s *Store) PreviewLegacyBOM(ctx context.Context, a *AuthSession, project *ProjectSummary, batch string, data []byte) (*LegacyBOMPreview, error) {
	tx, err := s.beginControlRead(ctx, a)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = requireProjectAdmin(ctx, tx, a, project.ID); err != nil {
		return nil, err
	}
	_, _, out, err := analyzeLegacyBOMTx(ctx, tx, project, batch, data)
	if err != nil {
		return nil, err
	}
	if out.CanCommit {
		out.PreviewToken = bomPreviewToken(a, project.ID, legacyBOMFiles(batch, data))
	}
	return out, tx.Commit()
}
func (s *Store) CommitLegacyBOM(ctx context.Context, a *AuthSession, project *ProjectSummary, batch string, data []byte, token, trace string) (*BOMPackageCommitResult, error) {
	if !validBOMPreviewToken(a, project.ID, legacyBOMFiles(batch, data), token) {
		return nil, apiError(409, "BOM_PREVIEW_REQUIRED", "Preview this exact legacy CSV before commit")
	}
	tx, err := s.beginControl(ctx, a)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = requireProjectAdmin(ctx, tx, a, project.ID); err != nil {
		return nil, err
	}
	if err = lockKey(ctx, tx, "bom-package:"+project.ID); err != nil {
		return nil, err
	}
	pkg, folded, preview, err := analyzeLegacyBOMTx(ctx, tx, project, batch, data)
	if err != nil {
		return nil, err
	}
	if !preview.CanCommit {
		return nil, apiError(409, "BOM_PRECHECK_BLOCKED", "Legacy BOM has blocking issues")
	}
	if preview.Idempotent {
		result := &BOMPackageCommitResult{BatchID: batch, ContentHash: preview.ContentHash, Idempotent: true}
		err = tx.QueryRowContext(ctx, `SELECT id,committed_at,(SELECT count(*) FROM bom_item_revisions WHERE package_id=p.id) FROM bom_import_packages p WHERE project_id=$1 AND bom_import_batch_id=$2`, project.ID, batch).Scan(&result.PackageID, &result.CommittedAt, &result.ItemCount)
		if err != nil {
			return nil, err
		}
		return result, tx.Commit()
	}
	materials, err := buildBOMMaterials(ctx, tx, project.ID, pkg)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	result := &BOMPackageCommitResult{PackageID: RandomID(), BatchID: batch, ContentHash: preview.ContentHash, ItemCount: len(materials), CommittedAt: now}
	manifestJSON, err := json.Marshal(pkg.Manifest)
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO bom_import_packages(id,project_id,bom_import_batch_id,schema_version,profile_version_ref,content_hash,declared_by,manifest,row_count,state,uploaded_by,committed_at) VALUES($1,$2,$3,'legacy-wide/v1',NULL,$4,'server_team',$5,$6,'committed',$7,$8)`, result.PackageID, project.ID, batch, preview.ContentHash, manifestJSON, folded.Rows, a.User.ID, now)
	if err != nil {
		return nil, err
	}
	columns := []string{"sn", "hostname", "model_name", "mac"}
	colsJSON, _ := json.Marshal(columns)
	_, err = tx.ExecContext(ctx, `INSERT INTO bom_import_manifest_files(package_id,file_path,domain,entity,columns,row_count,file_hash) VALUES($1,'legacy/wide.csv','server','legacy_wide',$2,$3,$4)`, result.PackageID, colsJSON, folded.Rows, pkg.FileHashes["legacy/wide.csv"])
	if err != nil {
		return nil, err
	}
	for _, issue := range preview.Issues {
		if issue.Severity == "BLOCK" {
			return nil, apiError(409, "BOM_PRECHECK_BLOCKED", "Legacy BOM has blocking issues")
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO bom_import_issues(id,package_id,severity,file_path,bom_line_no,field,code,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, RandomID(), result.PackageID, issue.Severity, issue.File, nullBOMInt(issue.BOMLineNo), nullBOMString(issue.Field), issue.Code, now)
		if err != nil {
			return nil, err
		}
	}
	for _, m := range materials {
		if err = writeBOMMaterial(ctx, tx, project.ID, result.PackageID, m, now); err != nil {
			return nil, err
		}
	}
	for _, m := range folded.Input.Machines {
		_, err = tx.ExecContext(ctx, `UPDATE machines SET bmc_address=$2 WHERE serial_number=$1`, m.SerialNumber, m.BmcAddress)
		if err != nil {
			return nil, err
		}
	}
	if err = appendAudit(ctx, tx, a.User.ID, project.ID, "bom.legacy.commit", result.PackageID, trace, map[string]any{"batch_id": batch, "content_hash": result.ContentHash, "machines": len(folded.Input.Machines), "ports": folded.Rows}); err != nil {
		return nil, err
	}
	return result, tx.Commit()
}
