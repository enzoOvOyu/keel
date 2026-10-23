package platform

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/enzoOvOyu/keel/internal/fabricboot"
)

type SZTPOnboarding struct {
	CMS             []byte
	IdentityVerdict string
	ProjectID       string
}

type activeSZTPPlan struct {
	projectID, batchRef, stageRef, hash string
	cfg                                 fabricboot.DHCPOptions
	live                                *KeaDeployment
}

// activeSZTPPlanTx selects one validated active route from direct source and
// TLS Host. Both data delivery and progress reports use the same route gate.
func (s *Store) activeSZTPPlanTx(ctx context.Context, tx *sql.Tx, source netip.Addr, requestHost string) (*activeSZTPPlan, error) {
	if s.FabricProbes.LiveBootConfigured() {
		return s.liveSZTPPlanTx(ctx, tx, source, requestHost)
	}
	rows, err := tx.QueryContext(ctx, `SELECT project_id,batch_ref,stage_ref,active_config,coalesce(active_hash,'') FROM ztp_mock_kea_plans WHERE stage_ref='N-S4' AND active_config IS NOT NULL FOR SHARE`)
	if err != nil {
		return nil, err
	}
	matches := []activeSZTPPlan{}
	for rows.Next() {
		var plan activeSZTPPlan
		var raw []byte
		if err = rows.Scan(&plan.projectID, &plan.batchRef, &plan.stageRef, &raw, &plan.hash); err != nil {
			break
		}
		if err = json.Unmarshal(raw, &plan.cfg); err != nil {
			break
		}
		prefix, parseErr := netip.ParsePrefix(plan.cfg.ManagementCIDR)
		if parseErr != nil || !prefix.Contains(source) {
			continue
		}
		uri := "https://" + requestHost
		found := false
		for _, candidate := range plan.cfg.Option143URIs {
			if strings.EqualFold(candidate, uri) {
				found = true
				break
			}
		}
		if found {
			matches = append(matches, plan)
		}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, ErrNotFound
	}
	if len(matches) != 1 {
		return nil, apiError(409, "SZTP_ROUTE_AMBIGUOUS", "Bootstrap offer matches more than one project or batch")
	}
	plan := matches[0]
	if plan.cfg.ProjectID != plan.projectID || plan.cfg.BatchRef != plan.batchRef || plan.cfg.StageRef != plan.stageRef {
		return nil, apiError(409, "SZTP_ACTIVE_INVALID", "Active bootstrap scope does not match")
	}
	mock := fabricboot.NewMockKea()
	recomputed, validationErr := mock.Stage(plan.cfg)
	if validationErr != nil || recomputed != plan.hash {
		return nil, apiError(409, "SZTP_ACTIVE_INVALID", "Active bootstrap offer failed validation")
	}
	return &plan, nil
}

// ServeZTP143 uses the authenticated certificate serial, actual TCP peer and
// exactly one active bootstrap offer to select a project. No URL parameter or
// user supplied HTTP identity participates in the selection.
func (s *Store) ServeZTP143(ctx context.Context, certSerial, sourceIP, requestHost, trace string, signedDataPreferred bool) (*SZTPOnboarding, error) {
	if !snPattern.MatchString(certSerial) || strings.TrimSpace(certSerial) != certSerial || requestHost == "" {
		return nil, ErrNotFound
	}
	source, err := netip.ParseAddr(sourceIP)
	if err != nil || !source.Is4() {
		return nil, ErrNotFound
	}
	if signedDataPreferred {
		return nil, apiError(409, "SZTP_SIGNED_DATA_UNAVAILABLE", "Signed onboarding data is not configured")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	plan, err := s.activeSZTPPlanTx(ctx, tx, source, requestHost)
	if err != nil {
		return nil, err
	}
	boot239, err := url.Parse(plan.cfg.Option239URL)
	if err != nil {
		return nil, err
	}
	mediaID := boot239.Query().Get("policy")
	if !bootMediaID.MatchString(mediaID) {
		return nil, apiError(409, "SZTP_SCRIPT_NOT_APPROVED", "Approved isolation script is required")
	}
	preview, err := approvedZTPManagementPolicyTx(ctx, tx, plan.projectID, plan.batchRef, mediaID, true)
	if err != nil {
		return nil, err
	}
	verdict, err := resolveZTPIdentityTx(ctx, tx, plan.projectID, ZTPIdentityInput{Serial: certSerial})
	if err != nil {
		return nil, err
	}
	if verdict.Verdict == "conflict" {
		if err = upsertZTPIdentityIssueTx(ctx, tx, plan.projectID, plan.batchRef, "143", certSerial, certSerial, sourceIP, trace, verdict.Reasons); err != nil {
			return nil, err
		}
		if err = appendAudit(ctx, tx, "", plan.projectID, "ztp.boot143.conflict", plan.batchRef, trace, map[string]any{"serial": certSerial, "source_ip": sourceIP, "reasons": verdict.Reasons}); err != nil {
			return nil, err
		}
		if err = tx.Commit(); err != nil {
			return nil, err
		}
		return nil, apiError(409, "ZTP_IDENTITY_CONFLICT", "Device identity conflicts with SoT")
	}
	if plan.live != nil && !liveBindingMatches(plan.live, verdict.SwitchID, sourceIP) {
		return nil, apiError(409, "SZTP_BOOT_BINDING_MISMATCH", "Certificate identity does not match the verified management source")
	}
	body, err := ztpApprovedPersonalizedScript(preview, verdict.SwitchID)
	if err != nil {
		return nil, err
	}
	cms, err := fabricboot.UnsignedScriptOnboarding(body, false)
	if err != nil {
		return nil, err
	}
	if err = appendAudit(ctx, tx, "", plan.projectID, "ztp.boot143.serve", plan.batchRef, trace, map[string]any{"serial": certSerial, "source_ip": sourceIP, "verdict": verdict.Verdict, "switch_id": verdict.SwitchID, "media_id": mediaID, "script_sha256": digest(body)}); err != nil {
		return nil, err
	}
	cmsHash := sha256.Sum256(cms)
	if _, err = tx.ExecContext(ctx, `INSERT INTO ztp_device_observations(id,project_id,batch_ref,switch_id,serial_number,source_ip,boot_channel,event_type,offer_hash,evidence_sha256,trace_id,occurred_at) VALUES($1,$2,$3,NULLIF($4,''),$5,$6,'143','data-served',$7,$8,$9,$10)`, RandomID(), plan.projectID, plan.batchRef, verdict.SwitchID, certSerial, sourceIP, plan.hash, hex.EncodeToString(cmsHash[:]), trace, time.Now().UTC()); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return &SZTPOnboarding{CMS: cms, IdentityVerdict: verdict.Verdict, ProjectID: plan.projectID}, nil
}
