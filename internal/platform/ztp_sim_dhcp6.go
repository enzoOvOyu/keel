package platform

import (
	"context"
	"database/sql"

	"github.com/enzoOvOyu/keel/internal/fabricboot"
)

// Explicit PoC bindings supplement, rather than allocate or rewrite, the
// approved management plan. They belong only to the mock transport contract.
func validateZTPSimDHCP6ScopeTx(ctx context.Context, tx *sql.Tx, project string, cfg *fabricboot.DHCPOptions) error {
	if cfg.DHCP6 == nil {
		return nil
	}
	for _, binding := range cfg.DHCP6.Bindings {
		var exists bool
		err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM switches s WHERE s.id=$1 AND EXISTS(SELECT 1 FROM bom_item_revisions r WHERE r.item_uid=s.id AND r.project_id=$2))`, binding.SwitchID, project).Scan(&exists)
		if err != nil {
			return err
		}
		if !exists {
			return apiError(409, "ZTP_DHCP6_TARGET_SCOPE", "Every DHCPv6 binding must refer to this project's BOM switch")
		}
	}
	return nil
}
