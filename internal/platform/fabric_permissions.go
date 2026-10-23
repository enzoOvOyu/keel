package platform

import (
	"context"
	"database/sql"
	"errors"
)

// This catalog covers implemented network operations only. Approval signatures
// and production push permissions must be added with their enforcing use cases.
func fabricPermissions(role string) []string {
	switch role {
	case "Admin", "NDE":
		return []string{"fabric:read", "fabric:plan", "fabric:collect", "fabric:template:edit"}
	case "NE":
		return []string{"fabric:read", "fabric:collect", "fabric:template:edit"}
	case "DE", "DL", "NH", "SRE", "Viewer":
		return []string{"fabric:read"}
	default:
		return []string{}
	}
}

// Never authorize a write from SessionResponse's cross-project capability union
// or a caller-supplied ProjectSummary. Resolve the current membership in the same
// transaction as the operation; control writes already serialize role changes.
func requireFabricPermission(ctx context.Context, tx *sql.Tx, a *AuthSession, projectID, permission string) error {
	var exists bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM projects WHERE id=$1)", projectID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	role := "Admin"
	if !a.User.IsAdmin {
		err := tx.QueryRowContext(ctx, "SELECT role FROM memberships WHERE project_id=$1 AND user_id=$2", projectID, a.User.ID).Scan(&role)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
	}
	for _, allowed := range fabricPermissions(role) {
		if allowed == permission {
			return nil
		}
	}
	return ErrForbidden
}
