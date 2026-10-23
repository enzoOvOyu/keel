package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/enzoOvOyu/keel/internal/fabricboot"
)

// Live control configuration never falls back to the separate mock offer store.
// We re-read the actual controller hash for admission; stale, unknown and
// configuration-only jobs cannot serve device data.
func (s *Store) liveBootRouteTx(ctx context.Context, tx *sql.Tx, project, batch, stage string) (*KeaDeployment, error) {
	control, key, e := s.FabricProbes.DeploymentControl()
	if e != nil {
		return nil, ErrNotFound
	}
	var quarantined bool
	e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM ztp_kea_deployments WHERE spec->>'controller_key'=$1 AND state IN('applying','unknown'))`, key).Scan(&quarantined)
	if e != nil {
		return nil, e
	}
	if quarantined {
		return nil, apiError(409, "ZTP_KEA_QUARANTINED", "Controller write outcome must be reconciled before serving bootstrap data")
	}
	live, e := control.ReadFullDHCP4(ctx)
	if e != nil {
		return nil, apiError(503, "ZTP_KEA_UNAVAILABLE", "Cannot verify the live bootstrap configuration")
	}
	job, e := scanKeaDeployment(tx.QueryRowContext(ctx, "SELECT "+keaDeploymentFields+` FROM ztp_kea_deployments WHERE project_id=$1 AND spec->'options'->>'batch_ref'=$2 AND spec->'options'->>'stage_ref'=$3 AND state IN('config_persisted','boot_ready') ORDER BY created_at DESC,id DESC LIMIT 1 FOR SHARE`, project, batch, stage))
	if errors.Is(e, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if e != nil {
		return nil, e
	}
	if job.State != "boot_ready" || job.ActiveHash != live.Hash || job.DORA == nil || job.Spec.ProbeHash != s.FabricProbes.Fingerprint || job.Spec.ControllerKey != key || job.SpecHash != fabricboot.ManagementPlanDigest(job.Spec) || job.DORA.Option239URL != job.Spec.Options.Option239URL {
		return nil, ErrNotFound
	}
	plan, e := currentManagementPlanTx(ctx, tx, project, batch)
	if e != nil {
		return nil, e
	}
	if plan.ID != job.Spec.PlanID {
		return nil, ErrNotFound
	}
	return job, nil
}
func (s *Store) active239OptionsTx(ctx context.Context, tx *sql.Tx, project, batch, stage string) ([]byte, string, *KeaDeployment, error) {
	if s.FabricProbes.LiveBootConfigured() {
		job, e := s.liveBootRouteTx(ctx, tx, project, batch, stage)
		if e != nil {
			return nil, "", nil, e
		}
		raw, e := json.Marshal(job.Spec.Options)
		return raw, job.ActiveHash, job, e
	}
	var raw []byte
	var hash string
	e := tx.QueryRowContext(ctx, `SELECT active_config,coalesce(active_hash,'') FROM ztp_mock_kea_plans WHERE project_id=$1 AND batch_ref=$2 AND stage_ref=$3 FOR SHARE`, project, batch, stage).Scan(&raw, &hash)
	if errors.Is(e, sql.ErrNoRows) {
		e = ErrNotFound
	}
	return raw, hash, nil, e
}
