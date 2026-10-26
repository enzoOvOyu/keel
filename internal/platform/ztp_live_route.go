package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/enzoOvOyu/keel/internal/fabricboot"
	"net/netip"
	"strings"
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
	if job.Spec.VendorProfile != nil && !job.Spec.VendorProfile.MatchesProof(job.DORA) {
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

func liveBindingMatches(job *KeaDeployment, id, source string) bool {
	for _, b := range job.Spec.Bindings {
		if b.SwitchID == id && b.IP == source {
			return true
		}
	}
	return false
}
func (s *Store) liveSZTPPlanTx(ctx context.Context, tx *sql.Tx, source netip.Addr, host string) (*activeSZTPPlan, error) {
	origin := "https://" + host
	configured, enabled := s.FabricProbes.SZTPOrigin()
	if !enabled || !strings.EqualFold(origin, configured) {
		return nil, ErrNotFound
	}
	rows, e := tx.QueryContext(ctx, `SELECT DISTINCT project_id,spec->'options'->>'batch_ref' FROM ztp_kea_deployments WHERE state='boot_ready' AND dora_proof->'option_143_uris' @> to_jsonb(ARRAY[$1]::text[]) AND EXISTS(SELECT 1 FROM jsonb_array_elements(spec->'bindings') b WHERE b->>'management_ip'=$2)`, configured, source.String())
	if e != nil {
		return nil, e
	}
	type candidate struct{ project, batch string }
	candidates := []candidate{}
	for rows.Next() {
		var c candidate
		if e = rows.Scan(&c.project, &c.batch); e != nil {
			break
		}
		candidates = append(candidates, c)
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return nil, e
	}
	if len(candidates) == 0 {
		return nil, ErrNotFound
	}
	if len(candidates) != 1 {
		return nil, apiError(409, "SZTP_ROUTE_AMBIGUOUS", "Management peer matches more than one verified bootstrap scope")
	}
	c := candidates[0]
	job, e := s.liveBootRouteTx(ctx, tx, c.project, c.batch, "N-S4")
	if e != nil {
		return nil, e
	}
	if job.Spec.SZTPEnvironmentID == "" || job.DORA == nil || len(job.DORA.Option143URIs) != 1 || job.DORA.Option143URIs[0] != configured {
		return nil, ErrNotFound
	}
	return &activeSZTPPlan{projectID: c.project, batchRef: c.batch, stageRef: "N-S4", hash: job.ActiveHash, cfg: job.Spec.Options, live: job}, nil
}
