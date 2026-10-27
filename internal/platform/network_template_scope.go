package platform

import (
	"context"
	"database/sql"

	"github.com/enzoOvOyu/keel/internal/netbaseline"
)

func validateNetworkTemplateScopeTx(ctx context.Context, tx *sql.Tx, project string, scope *netbaseline.TemplateScope, lock bool) error {
	// Old imported definitions remain identifiable as legacy unscoped versions;
	// their immutable content and existing approval hashes are not rewritten.
	if scope == nil {
		return nil
	}
	if e := scope.Validate(); e != nil {
		return apiError(400, "NETWORK_TEMPLATE_SCOPE_INVALID", e.Error())
	}
	query := `SELECT m.id,m.scope_key,m.kind,m.state,EXISTS(SELECT 1 FROM device_model_versions v WHERE v.model_id=m.id) FROM device_models m WHERE m.id=ANY($1)`
	if lock {
		query += " FOR SHARE OF m"
	}
	rows, e := tx.QueryContext(ctx, query, scope.DeviceModelIDs)
	if e != nil {
		return e
	}
	defer rows.Close()
	found := 0
	for rows.Next() {
		var id, source, kind, state string
		var published bool
		if e = rows.Scan(&id, &source, &kind, &state, &published); e != nil {
			return e
		}
		if (source != "global" && source != "project:"+project) || kind != "switch" || state != "active" || !published {
			return apiError(409, "NETWORK_TEMPLATE_MODEL_UNAVAILABLE", "Template scope requires active published switch models visible to the current project")
		}
		found++
	}
	if e = rows.Err(); e != nil {
		return e
	}
	if found != len(scope.DeviceModelIDs) {
		return apiError(409, "NETWORK_TEMPLATE_MODEL_UNAVAILABLE", "Template scope refers to unavailable switch models")
	}
	return nil
}
