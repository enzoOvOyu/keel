package platform

import (
	"context"
	"database/sql"
	"strings"
)

// AppendBMCInventoryEventTx records a domain fact in the producer's transaction.
// It is an admission hint, never hardware evidence or a second execution queue.
func AppendBMCInventoryEventTx(ctx context.Context, tx *sql.Tx, project, kind, source, machine, batch string) error {
	if len(source) == 0 || len(source) > 300 || strings.ContainsAny(source, "\r\n\x00") {
		return apiError(400, "BMC_EVENT_INVALID", "Invalid inventory event reference")
	}
	switch kind {
	case "bom.committed":
		if machine != "" || batch != "" {
			return apiError(400, "BMC_EVENT_INVALID", "BOM event is project scoped")
		}
	case "credential.batch.rotated":
		if batch == "" || len(batch) > 120 || machine != "" {
			return apiError(400, "BMC_EVENT_INVALID", "Credential event requires a batch reference")
		}
	case "asset.provisioned":
		if machine == "" || batch != "" {
			return apiError(400, "BMC_EVENT_INVALID", "Provision event requires a machine")
		}
	default:
		return apiError(400, "BMC_EVENT_INVALID", "Unsupported inventory event")
	}
	// Serialize sequence allocation per project through commit. A consumer must
	// never advance past a lower sequence allocated by an uncommitted producer.
	if e := lockKey(ctx, tx, "bmc-event:"+project); e != nil {
		return e
	}
	_, e := tx.ExecContext(ctx, `INSERT INTO bmc_inventory_events(project_id,event_type,source_ref,machine_id,credential_batch_ref,occurred_at) VALUES($1,$2,$3,nullif($4,''),nullif($5,''),statement_timestamp()) ON CONFLICT(project_id,event_type,source_ref) DO NOTHING`, project, kind, source, machine, batch)
	return e
}
