package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/enzoOvOyu/keel/internal/fabricboot"
)

type ZTPManagementManifest struct {
	IsolationSHA256 string                        `json:"isolation_sha256"`
	IsolationSize   int                           `json:"isolation_size"`
	Version         string                        `json:"version"`
	ProjectID       string                        `json:"project_id"`
	BatchRef        string                        `json:"batch_ref"`
	PlanID          string                        `json:"plan_id"`
	PlanHash        string                        `json:"plan_hash"`
	Renderer        string                        `json:"renderer"`
	Scripts         []fabricboot.ManagementScript `json:"scripts"`
}
type ZTPManagementArtifactPreview struct {
	Manifest ZTPManagementManifest         `json:"manifest"`
	SHA256   string                        `json:"sha256"`
	Size     int                           `json:"size"`
	Content  string                        `json:"content"`
	Scripts  []fabricboot.ManagementScript `json:"scripts"`
}

func ztpManagementArtifactsTx(ctx context.Context, tx *sql.Tx, project, batch string) (*ZTPManagementArtifactPreview, error) {
	plan, e := currentManagementPlanTx(ctx, tx, project, batch)
	if e != nil {
		return nil, e
	}
	out := &ZTPManagementArtifactPreview{Manifest: ZTPManagementManifest{Version: "ztp-management-manifest/v1", IsolationSHA256: fabricboot.IsolationScriptDigest(), IsolationSize: len(fabricboot.IsolationScriptV1), ProjectID: project, BatchRef: batch, PlanID: plan.ID, PlanHash: plan.ContentHash, Renderer: fabricboot.ManagementRendererV1, Scripts: []fabricboot.ManagementScript{}}, Scripts: []fabricboot.ManagementScript{}}
	for _, a := range plan.Assignments {
		if a.Batch != batch {
			continue
		}
		ref, e := simSwitchCredentialRef(ctx, tx, project, a.SwitchID)
		if e != nil {
			return nil, e
		}
		registered, e := simCredentialRegistered(ctx, tx, project, ref)
		if e != nil {
			return nil, e
		}
		if !registered {
			return nil, apiError(409, "ZTP_CREDENTIAL_BATCH_REQUIRED", "Register each target switch credential batch before approving management artifacts")
		}
		rendered, e := fabricboot.RenderManagementScript(plan.Rules, a, ref)
		if e != nil {
			return nil, e
		}
		out.Scripts = append(out.Scripts, *rendered)
		summary := *rendered
		summary.Body = ""
		out.Manifest.Scripts = append(out.Manifest.Scripts, summary)
	}
	raw, e := json.Marshal(out.Manifest)
	if e != nil {
		return nil, e
	}
	out.Content = string(raw)
	out.SHA256 = digest(out.Content)
	out.Size = len(raw)
	return out, nil
}
func (s *Store) PreviewZTPManagementArtifacts(ctx context.Context, a *AuthSession, p *ProjectSummary, batch string) (*ZTPManagementArtifactPreview, error) {
	if !bootRouteRef.MatchString(batch) {
		return nil, apiError(400, "ZTP_BATCH_INVALID", "Select a planned batch")
	}
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireFabricPermission(ctx, tx, a, p.ID, "fabric:read"); e != nil {
		return nil, e
	}
	out, e := ztpManagementArtifactsTx(ctx, tx, p.ID, batch)
	if e != nil {
		return nil, e
	}
	return out, tx.Commit()
}
func approvedZTPManagementPolicyTx(ctx context.Context, tx *sql.Tx, project, batch, media string, lock bool) (*ZTPManagementArtifactPreview, error) {
	query := `SELECT sha256,size FROM media_items WHERE id=$1 AND scope_key IN($2,'global') AND type='ztp_script' AND state='approved' AND NOT EXISTS(SELECT 1 FROM media_artifact_integrity mi WHERE mi.media_id=media_items.id AND mi.quarantined_at IS NOT NULL)`
	if lock {
		query += " FOR SHARE"
	}
	var hash string
	var size int64
	e := tx.QueryRowContext(ctx, query, media, "project:"+project).Scan(&hash, &size)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, apiError(409, "ZTP_SCRIPT_NOT_APPROVED", "An independently approved isolation script or management manifest is required")
	}
	if e != nil {
		return nil, e
	}
	// Generated management manifests may predate stored artifacts. If a bound
	// artifact has integrity state, its quarantine is still an immediate gate.
	if lock {
		var quarantined bool
		e = tx.QueryRowContext(ctx, "SELECT quarantined_at IS NOT NULL FROM media_artifact_integrity WHERE media_id=$1 FOR SHARE", media).Scan(&quarantined)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return nil, e
		}
		if quarantined {
			return nil, apiError(409, "ZTP_SCRIPT_QUARANTINED", "Approved bootstrap media is quarantined")
		}
	}
	if hash == fabricboot.IsolationScriptDigest() && size == int64(len(fabricboot.IsolationScriptV1)) {
		return nil, nil
	}
	out, e := ztpManagementArtifactsTx(ctx, tx, project, batch)
	if e != nil {
		return nil, e
	}
	if hash != out.SHA256 || size != int64(out.Size) {
		return nil, apiError(409, "ZTP_SCRIPT_DIGEST_MISMATCH", "Approved manifest no longer matches the current plan, credential batches or renderer")
	}
	return out, nil
}
func ztpApprovedPersonalizedScript(preview *ZTPManagementArtifactPreview, switchID string) (string, error) {
	if preview == nil {
		return fabricboot.IsolationScriptV1, nil
	}
	for _, script := range preview.Scripts {
		if script.SwitchID == switchID {
			return script.Body, nil
		}
	}
	return "", apiError(409, "ZTP_MANAGEMENT_SCRIPT_NOT_FOUND", "Device is outside the approved batch manifest")
}
