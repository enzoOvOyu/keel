package platform

import (
	"context"
	"database/sql"
	"errors"
)

type BOMPackageIssuesResult struct {
	PackageID string            `json:"package_id"`
	Issues    []BOMPackageIssue `json:"issues"`
}

// ListBOMPackageIssues returns the diagnostic facts preserved with a committed
// package. Blocking preview issues are never persisted because commit is atomic.
func (s *Store) ListBOMPackageIssues(ctx context.Context, actor *AuthSession, projectID, packageID, severity string) (*BOMPackageIssuesResult, error) {
	if severity != "" && severity != "BLOCK" && severity != "WARN" && severity != "INFO" {
		return nil, apiError(400, "VALIDATION_ERROR", "Invalid issue severity")
	}
	tx, err := s.beginControlRead(ctx, actor)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := requireProjectMember(ctx, tx, actor, projectID); err != nil {
		return nil, err
	}
	var id string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM bom_import_packages WHERE id=$1 AND project_id=$2`, packageID, projectID).Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apiError(404, "BOM_PACKAGE_NOT_FOUND", "BOM package not found")
		}
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT severity,file_path,bom_line_no,field,code FROM bom_import_issues WHERE package_id=$1 AND ($2='' OR severity=$2) ORDER BY file_path,bom_line_no NULLS FIRST,severity,code,id`, packageID, severity)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := &BOMPackageIssuesResult{PackageID: packageID, Issues: []BOMPackageIssue{}}
	for rows.Next() {
		var issue BOMPackageIssue
		var line sql.NullInt64
		var field sql.NullString
		if err := rows.Scan(&issue.Severity, &issue.File, &line, &field, &issue.Code); err != nil {
			return nil, err
		}
		if line.Valid {
			issue.BOMLineNo = int(line.Int64)
		}
		if field.Valid {
			issue.Field = field.String
		}
		result.Issues = append(result.Issues, issue)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, tx.Commit()
}
