package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/enzoOvOyu/keel/internal/fabricboot"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var bootRouteRef = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,159}$`)
var bootProjectID = regexp.MustCompile(`^[a-f0-9]{32}$`)
var bootMediaID = regexp.MustCompile(`^[a-f0-9]{32}$`)

type ZTP239Claim struct {
	Serial      string
	BaseMAC     string
	MgmtMAC     string
	ProductName string
	Version     string
	Build       string
}

type ZTP239Script struct {
	Body            string
	SHA256          string
	IdentityVerdict string
}

func (s *Store) ServeZTP239(ctx context.Context, projectID, batchRef, stageRef, mediaID, sourceIP, requestScheme, requestHost, requestPath, rawQuery, trace string, claim ZTP239Claim) (*ZTP239Script, error) {
	if !bootProjectID.MatchString(projectID) || !bootRouteRef.MatchString(batchRef) || stageRef != "N-S4" || !bootMediaID.MatchString(mediaID) || requestPath != "/api/v1/boot/ztp" {
		return nil, ErrNotFound
	}
	source, err := netip.ParseAddr(sourceIP)
	if err != nil || !source.Is4() {
		return nil, ErrNotFound
	}
	for _, value := range []string{claim.Serial, claim.BaseMAC, claim.MgmtMAC, claim.ProductName, claim.Version, claim.Build} {
		if strings.TrimSpace(value) == "" || len(value) > 128 || strings.ContainsAny(value, "\r\n\x00") {
			return nil, apiError(400, "ZTP_239_HEADERS_INVALID", "Required Cumulus device headers are missing or invalid")
		}
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	raw, activeHash, liveJob, err := s.active239OptionsTx(ctx, tx, projectID, batchRef, stageRef)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, ErrNotFound
	}
	var cfg fabricboot.DHCPOptions
	if err = json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	if cfg.ProjectID != projectID || cfg.BatchRef != batchRef || cfg.StageRef != stageRef {
		return nil, ErrNotFound
	}
	mock := fabricboot.NewMockKea()
	computed, validateErr := mock.Stage(cfg)
	if validateErr != nil || liveJob == nil && computed != activeHash {
		return nil, apiError(409, "ZTP_KEA_ACTIVE_INVALID", "Active Kea configuration failed validation")
	}
	prefix, err := netip.ParsePrefix(cfg.ManagementCIDR)
	if err != nil || !prefix.Contains(source) {
		return nil, ErrNotFound
	}
	configuredURL, err := url.Parse(cfg.Option239URL)
	if err != nil || configuredURL.Path != requestPath || configuredURL.Scheme != requestScheme || !strings.EqualFold(configuredURL.Host, requestHost) {
		return nil, ErrNotFound
	}
	actualQuery, err := url.ParseQuery(rawQuery)
	if err != nil || configuredURL.Query().Encode() != actualQuery.Encode() || actualQuery.Get("project") != projectID || actualQuery.Get("batch") != batchRef || actualQuery.Get("stage") != stageRef || actualQuery.Get("policy") != mediaID {
		return nil, ErrNotFound
	}
	preview, err := approvedZTPManagementPolicyTx(ctx, tx, projectID, batchRef, mediaID, true)
	if err != nil {
		return nil, err
	}
	verdict, err := resolveZTPIdentityTx(ctx, tx, projectID, ZTPIdentityInput{Serial: claim.Serial, BaseMAC: claim.BaseMAC, MgmtMAC: claim.MgmtMAC})
	if err != nil {
		return nil, err
	}
	if verdict.Verdict == "conflict" {
		if err = upsertZTPIdentityIssueTx(ctx, tx, projectID, batchRef, "239", claim.Serial, claim.Serial, sourceIP, trace, verdict.Reasons); err != nil {
			return nil, err
		}
		if err = appendAudit(ctx, tx, "", projectID, "ztp.boot239.conflict", batchRef, trace, map[string]any{"serial": claim.Serial, "source_ip": sourceIP, "reasons": verdict.Reasons}); err != nil {
			return nil, err
		}
		if err = tx.Commit(); err != nil {
			return nil, err
		}
		return nil, apiError(409, "ZTP_IDENTITY_CONFLICT", "Device identity conflicts with SoT")
	}
	if liveJob != nil && verdict.SwitchID != "" {
		bound := false
		for _, binding := range liveJob.Spec.Bindings {
			if binding.SwitchID == verdict.SwitchID && binding.IP == sourceIP && strings.EqualFold(binding.MAC, claim.MgmtMAC) {
				bound = true
				break
			}
		}
		if !bound {
			return nil, apiError(409, "ZTP_BOOT_BINDING_MISMATCH", "Device identity and direct management source do not match this verified batch")
		}
	}
	body, err := ztpApprovedPersonalizedScript(preview, verdict.SwitchID)
	if err != nil {
		return nil, err
	}
	scriptHash := sha256.Sum256([]byte(body))
	if _, err = tx.ExecContext(ctx, `INSERT INTO ztp_device_observations(id,project_id,batch_ref,switch_id,serial_number,source_ip,boot_channel,event_type,offer_hash,evidence_sha256,trace_id,occurred_at) VALUES($1,$2,$3,NULLIF($4,''),$5,$6,'239','data-served',$7,$8,$9,$10)`, RandomID(), projectID, batchRef, verdict.SwitchID, claim.Serial, sourceIP, activeHash, hex.EncodeToString(scriptHash[:]), trace, time.Now().UTC()); err != nil {
		return nil, err
	}
	if err = appendAudit(ctx, tx, "", projectID, "ztp.boot239.serve", batchRef, trace, map[string]any{"serial": claim.Serial, "source_ip": sourceIP, "verdict": verdict.Verdict, "switch_id": verdict.SwitchID, "media_id": mediaID, "script_sha256": hex.EncodeToString(scriptHash[:])}); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return &ZTP239Script{Body: body, SHA256: hex.EncodeToString(scriptHash[:]), IdentityVerdict: verdict.Verdict}, nil
}
