package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/enzoOvOyu/keel/internal/fabricboot"
	"net/netip"
	"strconv"
	"sync"
	"time"
)

type ZTPManageabilityCheck struct {
	ID                 string                         `json:"id"`
	ObservationID      string                         `json:"observation_id"`
	SwitchID           string                         `json:"switch_id"`
	SessionID          string                         `json:"session_id,omitempty"`
	CredentialBatchRef string                         `json:"credential_batch_ref"`
	EvidenceSource     string                         `json:"evidence_source"`
	Code               string                         `json:"code"`
	Attempt            int                            `json:"attempt"`
	NextRetryAt        *time.Time                     `json:"next_retry_at"`
	CheckedAt          time.Time                      `json:"checked_at"`
	Proof              *fabricboot.ManageabilityProof `json:"proof"`
}
type ztpManageabilityFacts struct {
	Request     fabricboot.ManageabilityRequest
	Hash        string
	Observation ZTPDeviceObservation
	Actor       AuthSession
	RetryMax    int
}

func (s *Store) ztpManageabilityFactsTx(ctx context.Context, tx *sql.Tx, id string) (*ztpManageabilityFacts, error) {
	f := &ztpManageabilityFacts{RetryMax: 3}
	o := &f.Observation
	e := tx.QueryRowContext(ctx, `SELECT id,project_id,batch_ref,switch_id,serial_number,source_ip,boot_channel,offer_hash,evidence_sha256,occurred_at FROM ztp_device_observations WHERE id=$1 AND event_type='data-served' AND switch_id IS NOT NULL AND occurred_at>now()-interval '24 hours'`, id).Scan(&o.ID, &f.Request.ProjectID, &o.BatchRef, &o.SwitchID, &o.SerialNumber, &o.SourceIP, &o.BootChannel, &o.OfferHash, &o.EvidenceSHA256, &o.OccurredAt)
	if e != nil {
		return nil, e
	}
	live, e := s.liveBootRouteTx(ctx, tx, f.Request.ProjectID, o.BatchRef, "N-S4")
	if e != nil {
		return nil, e
	}
	if live.ActiveHash != o.OfferHash || !liveBindingMatches(live, o.SwitchID, o.SourceIP) {
		return nil, apiError(409, "ZTP_BOOT_EVIDENCE_STALE", "Boot observation is outside the current verified route")
	}
	var enabled bool
	e = tx.QueryRowContext(ctx, `SELECT u.id,u.enabled,u.is_admin FROM ztp_kea_deployments k JOIN users u ON u.id=k.created_by WHERE k.id=$1 FOR SHARE OF u,k`, live.ID).Scan(&f.Actor.User.ID, &enabled, &f.Actor.User.IsAdmin)
	if e != nil {
		return nil, e
	}
	if !enabled {
		return nil, ErrForbidden
	}
	if e = requireFabricPermission(ctx, tx, &f.Actor, f.Request.ProjectID, "fabric:plan"); e != nil {
		return nil, e
	}
	preview, e := approvedZTPManagementPolicyTx(ctx, tx, f.Request.ProjectID, o.BatchRef, policyMediaID(live.Spec.Options.Option239URL), true)
	if e != nil {
		return nil, e
	}
	if preview == nil {
		return nil, apiError(409, "ZTP_MANAGEMENT_MANIFEST_REQUIRED", "Isolation-only delivery cannot prove personalized management")
	}
	plan, e := currentManagementPlanTx(ctx, tx, f.Request.ProjectID, o.BatchRef)
	if e != nil {
		return nil, e
	}
	assignment := fabricboot.ManagementAssignment{}
	for _, a := range plan.Assignments {
		if a.SwitchID == o.SwitchID {
			assignment = a
			break
		}
	}
	if assignment.SwitchID == "" || assignment.Address != o.SourceIP {
		return nil, apiError(409, "ZTP_MANAGEMENT_BINDING_STALE", "Current identity and management source must match")
	}
	resolution, e := resolveZTPIdentityTx(ctx, tx, f.Request.ProjectID, ZTPIdentityInput{Serial: o.SerialNumber})
	if e != nil {
		return nil, e
	}
	if resolution.Verdict != "matched" || resolution.SwitchID != o.SwitchID {
		return nil, apiError(409, "ZTP_IDENTITY_CONFLICT", "Identity no longer matches the approved target")
	}
	prefix, _ := netip.ParsePrefix(plan.Rules.CIDR)
	request := fabricboot.ManageabilityRequest{Version: "ztp-manageability-request/v1", ProjectID: f.Request.ProjectID, BatchRef: o.BatchRef, SwitchID: o.SwitchID, Serial: o.SerialNumber, PlanHash: plan.ContentHash, OfferHash: o.OfferHash, BootEvidenceSHA256: o.EvidenceSHA256, Hostname: assignment.Hostname, Address: assignment.Address, PrefixBits: prefix.Bits(), Gateway: assignment.Gateway}
	for _, b := range live.Spec.Bindings {
		if b.SwitchID == o.SwitchID {
			request.ManagementMAC = b.MAC
		}
	}
	for _, script := range preview.Scripts {
		if script.SwitchID == o.SwitchID {
			request.ScriptSHA256 = script.SHA256
			request.CredentialBatchRef = script.CredentialBatchRef
		}
	}
	values, e := catalogDefaultsTx(ctx, tx, request.ProjectID, "fabric.ztp_retry_max")
	if e != nil {
		return nil, e
	}
	if configured, ok := values["fabric.ztp_retry_max"]; ok {
		n, e := strconv.Atoi(configured)
		if e != nil || n < 0 || n > 10 {
			return nil, apiError(409, "ZTP_RETRY_CONFIG_INVALID", "ZTP retry maximum must be 0 through 10")
		}
		f.RetryMax = n
	}
	f.Request = request
	f.Hash = fabricboot.ManagementPlanDigest(struct {
		Request fabricboot.ManageabilityRequest
		Probe   string
	}{request, s.FabricProbes.Fingerprint})
	return f, nil
}

// ReconcileZTPDeviceSessions only reads external devices. It consumes existing
// immutable boot observations; it creates no second device-write runner/queue.
func (s *Store) ReconcileZTPDeviceSessions(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > 32 {
		return 0, apiError(400, "ZTP_RECONCILE_LIMIT", "Use a batch limit from 1 through 32")
	}
	if !s.FabricProbes.ManageabilityConfigured() || !s.FabricProbes.LiveBootConfigured() {
		return 0, nil
	}

	coordinator, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return 0, e
	}
	defer coordinator.Rollback()
	var held bool
	if e = coordinator.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock(230923,144)`).Scan(&held); e != nil {
		return 0, e
	}
	if !held {
		return 0, nil
	}
	rows, e := s.DB.QueryContext(ctx, ztpDeviceCandidateQuery, limit, s.FabricProbes.Fingerprint, fabricboot.IsolationScriptDigest())
	if e != nil {
		return 0, e
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return 0, e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return 0, e
	}
	count := 0
	var firstError error
	var mu sync.Mutex
	var wg sync.WaitGroup
	slots := make(chan struct{}, 4)
	for _, id := range ids {
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			wg.Wait()
			return count, ctx.Err()
		}
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			defer func() { <-slots }()
			done, checkErr := s.reconcileZTPDevice(ctx, id)
			if checkErr != nil {
				var business *Error
				if !errors.As(checkErr, &business) {
					mu.Lock()
					if firstError == nil {
						firstError = checkErr
					}
					mu.Unlock()
				}
			}
			if done {
				mu.Lock()
				count++
				mu.Unlock()
			}
		}(id)
	}
	wg.Wait()
	return count, firstError
}
func (s *Store) reconcileZTPDevice(ctx context.Context, id string) (bool, error) {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return false, e
	}
	facts, e := s.ztpManageabilityFactsTx(ctx, tx, id)
	if e != nil {
		tx.Rollback()
		return false, e
	}
	attempt := 1
	var last int
	var code string
	var next sql.NullTime
	e = tx.QueryRowContext(ctx, `SELECT attempt,code,next_retry_at FROM ztp_manageability_checks WHERE observation_id=$1 AND facts_hash=$2 ORDER BY attempt DESC LIMIT 1`, id, facts.Hash).Scan(&last, &code, &next)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		tx.Rollback()
		return false, e
	}
	if e == nil {
		if code == "ready" || last > facts.RetryMax || next.Valid && time.Now().Before(next.Time) {
			tx.Rollback()
			return false, nil
		}
		attempt = last + 1
	}
	tx.Rollback()
	resourceGuard, e := s.guardZTPManagementRead(ctx, facts.Request.SwitchID)
	if e != nil {
		return false, e
	}
	defer resourceGuard.Rollback()
	request := facts.Request
	request.ID = RandomID()
	proof, probeErr := s.FabricProbes.VerifyManageability(ctx, request)
	checked := time.Now().UTC().Truncate(time.Microsecond)
	result := &ZTPManageabilityCheck{ID: RandomID(), ObservationID: id, SwitchID: request.SwitchID, CredentialBatchRef: request.CredentialBatchRef, EvidenceSource: "unknown", Code: "unreachable", Attempt: attempt, CheckedAt: checked, Proof: proof}
	if probeErr == nil {
		result.Code = proof.Code
		result.EvidenceSource = proof.Source
	}
	if result.Code != "ready" && attempt <= facts.RetryMax {
		retry := checked.Add(ztpRetryDelay(attempt))
		result.NextRetryAt = &retry
	}
	tx, e = s.DB.BeginTx(ctx, nil)
	if e != nil {
		return false, e
	}
	defer tx.Rollback()
	if e = lockKey(ctx, tx, "keel-admin-initialization"); e != nil {
		return false, e
	}
	current, e := s.ztpManageabilityFactsTx(ctx, tx, id)
	if e != nil {
		return false, e
	}
	if current.Hash != facts.Hash {
		return false, apiError(409, "ZTP_MANAGEABILITY_FACTS_CHANGED", "Current plan, credentials, authority or boot route changed")
	}
	if proof != nil {
		if e = fabricboot.ValidateManageabilityProof(request, proof, time.Now().UTC()); e != nil {
			return false, e
		}
	}
	current.Request = request
	if result.EvidenceSource == "device" {
		session, e := s.projectZTPManageabilityTx(ctx, tx, current, result)
		if e != nil {
			return false, e
		}
		result.SessionID = session
	}
	requestJSON, _ := json.Marshal(request)
	proofJSON, _ := json.Marshal(proof)
	_, e = tx.ExecContext(ctx, `INSERT INTO ztp_manageability_checks(id,observation_id,project_id,switch_id,session_id,facts_hash,probe_hash,credential_batch_ref,evidence_source,code,request,proof,attempt,next_retry_at,checked_at) VALUES($1,$2,$3,$4,NULLIF($5,''),$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`, result.ID, id, request.ProjectID, request.SwitchID, result.SessionID, facts.Hash, s.FabricProbes.Fingerprint, request.CredentialBatchRef, result.EvidenceSource, result.Code, requestJSON, proofJSON, result.Attempt, result.NextRetryAt, checked)
	if e != nil {
		return false, e
	}
	if e = appendAudit(ctx, tx, current.Actor.User.ID, request.ProjectID, "ztp.manageability.verify", request.SwitchID, "", map[string]any{"check_id": result.ID, "code": result.Code, "evidence_source": result.EvidenceSource, "credential_batch_ref": result.CredentialBatchRef, "attempt": attempt}); e != nil {
		return false, e
	}
	return true, tx.Commit()
}
func (s *Store) projectZTPManageabilityTx(ctx context.Context, tx *sql.Tx, facts *ztpManageabilityFacts, result *ZTPManageabilityCheck) (string, error) {
	if result.EvidenceSource != "device" || result.Proof == nil || result.Proof.Source != "device" || result.Proof.Code != result.Code {
		return "", apiError(409, "ZTP_DEVICE_EVIDENCE_REQUIRED", "Only trusted device proof can project a real ZTP session")
	}
	if e := fabricboot.ValidateManageabilityProof(facts.Request, result.Proof, time.Now().UTC()); e != nil {
		return "", e
	}
	request := facts.Request
	now := result.CheckedAt
	var sessionID string
	var version int64
	var state string
	e := tx.QueryRowContext(ctx, `SELECT id,state_version,state FROM ztp_sessions WHERE project_id=$1 AND switch_id=$2 AND batch_ref=$3 AND mode='device' AND started_at=$4 ORDER BY id LIMIT 1 FOR UPDATE`, request.ProjectID, request.SwitchID, request.BatchRef, facts.Observation.OccurredAt).Scan(&sessionID, &version, &state)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return "", e
	}
	if errors.Is(e, sql.ErrNoRows) {
		sessionID = RandomID()
		claim, _ := json.Marshal(ZTPIdentityInput{Serial: request.Serial})
		resolution, _ := json.Marshal(ZTPIdentityResolution{Verdict: "matched", SwitchID: request.SwitchID})
		_, e = tx.ExecContext(ctx, `INSERT INTO ztp_sessions(id,project_id,switch_id,batch_ref,mode,boot_channel,state,state_version,attempt,retry_max,identity_claim,identity_resolution,started_at,updated_at) VALUES($1,$2,$3,$4,'device',$5,'powered',1,$6,$7,$8,$9,$10,$11)`, sessionID, request.ProjectID, request.SwitchID, request.BatchRef, facts.Observation.BootChannel, result.Attempt, facts.RetryMax, claim, resolution, facts.Observation.OccurredAt, now)
		if e != nil {
			return "", e
		}
		version = 1
		state = "powered"
		if e = insertZTPEvent(ctx, tx, sessionID, ZTPSessionEvent{Seq: 1, ToState: "powered", EventCode: "device.observed_powered", EvidenceRef: facts.Observation.ID, Attempt: result.Attempt, OccurredAt: now}); e != nil {
			return "", e
		}
	}
	if state == "managed" {
		return sessionID, nil
	}
	if result.Code != "ready" {
		event := ZTPSessionEvent{Seq: version + 1, FromState: state, ToState: "failed", EventCode: "device." + result.Code, EvidenceRef: result.ID, Attempt: result.Attempt, OccurredAt: now}
		if e = insertZTPEvent(ctx, tx, sessionID, event); e != nil {
			return "", e
		}
		_, e = tx.ExecContext(ctx, `UPDATE ztp_sessions SET state='failed',state_version=$2,attempt=$3,failure_phenomenon=$4,failure_cause=$5,failure_next_action=$6,next_retry_at=$7,updated_at=$8 WHERE id=$1`, sessionID, version+1, result.Attempt, "Authenticated minimum management verification did not pass", result.Code, "Check credential lifecycle, trusted SSH host key, minimum configuration and isolation; automatic bounded retry applies", result.NextRetryAt, now)
		if e != nil {
			return "", e
		}
		if result.Code == "credential_expired" {
			_, e = tx.ExecContext(ctx, `INSERT INTO ztp_credential_issues(id,project_id,session_id,switch_id,credential_batch_ref,verdict,attempt,occurred_at) VALUES($1,$2,$3,$4,$5,'expired',$6,$7) ON CONFLICT(session_id,attempt) DO NOTHING`, RandomID(), request.ProjectID, sessionID, request.SwitchID, request.CredentialBatchRef, result.Attempt, now)
		}
		return sessionID, e
	}
	if state == "failed" {
		version++
		if e = insertZTPEvent(ctx, tx, sessionID, ZTPSessionEvent{Seq: version, FromState: state, ToState: "powered", EventCode: "device.retry_verified", EvidenceRef: result.ID, Attempt: result.Attempt, OccurredAt: now}); e != nil {
			return "", e
		}
		state = "powered"
	}
	for _, stage := range ztpStages[1:] {
		version++
		if e = insertZTPEvent(ctx, tx, sessionID, ZTPSessionEvent{Seq: version, FromState: state, ToState: stage, EventCode: "device.verified_" + stage, EvidenceRef: result.ID, Attempt: result.Attempt, OccurredAt: now}); e != nil {
			return "", e
		}
		state = stage
	}
	_, e = tx.ExecContext(ctx, `UPDATE ztp_sessions SET state='managed',state_version=$2,attempt=$3,failure_phenomenon=NULL,failure_cause=NULL,failure_next_action=NULL,next_retry_at=NULL,updated_at=$4,managed_at=$4 WHERE id=$1`, sessionID, version, result.Attempt, now)
	return sessionID, e
}
func (s *Store) ListZTPManageabilityChecks(ctx context.Context, a *AuthSession, p *ProjectSummary) ([]ZTPManageabilityCheck, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireFabricPermission(ctx, tx, a, p.ID, "fabric:read"); e != nil {
		return nil, e
	}
	rows, e := tx.QueryContext(ctx, `SELECT id,observation_id,switch_id,coalesce(session_id,''),credential_batch_ref,evidence_source,code,attempt,next_retry_at,checked_at,proof FROM ztp_manageability_checks WHERE project_id=$1 ORDER BY checked_at DESC,id DESC LIMIT 100`, p.ID)
	if e != nil {
		return nil, e
	}
	out := []ZTPManageabilityCheck{}
	for rows.Next() {
		var r ZTPManageabilityCheck
		var raw []byte
		var retry sql.NullTime
		if e = rows.Scan(&r.ID, &r.ObservationID, &r.SwitchID, &r.SessionID, &r.CredentialBatchRef, &r.EvidenceSource, &r.Code, &r.Attempt, &retry, &r.CheckedAt, &raw); e != nil {
			rows.Close()
			return nil, e
		}
		if retry.Valid {
			r.NextRetryAt = &retry.Time
		}
		if len(raw) > 0 && string(raw) != "null" {
			if e = json.Unmarshal(raw, &r.Proof); e != nil {
				rows.Close()
				return nil, e
			}
		}
		out = append(out, r)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	return out, tx.Commit()
}

// The existing execution kernel locks exec_resources FOR UPDATE before claiming
// any SSH lease. A shared row lock covers this bounded read probe and its final
// receipt, while every active/reconciling lease blocks admission.
func (s *Store) guardZTPManagementRead(ctx context.Context, switchID string) (*sql.Tx, error) {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	fail := func(e error) (*sql.Tx, error) { tx.Rollback(); return nil, e }
	resource := "ssh:switch:" + switchID
	if _, e = tx.ExecContext(ctx, `INSERT INTO exec_resources(id) VALUES($1) ON CONFLICT DO NOTHING`, resource); e != nil {
		return fail(e)
	}
	var locked string
	if e = tx.QueryRowContext(ctx, `SELECT id FROM exec_resources WHERE id=$1 FOR SHARE`, resource).Scan(&locked); e != nil {
		return fail(e)
	}
	var busy bool
	if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM exec_resource_leases WHERE resource_id=$1 AND state IN('held','reconciling'))`, resource).Scan(&busy); e != nil {
		return fail(e)
	}
	if busy {
		return fail(apiError(409, "ZTP_SSH_RESOURCE_BUSY", "An existing switch operation or unknown-write quarantine blocks management reads"))
	}
	return tx, nil
}

const ztpDeviceCandidateQuery = `SELECT o.id FROM ztp_device_observations o WHERE o.event_type='data-served' AND o.switch_id IS NOT NULL AND o.occurred_at>now()-interval '24 hours' AND NOT EXISTS(SELECT 1 FROM ztp_device_observations newer WHERE newer.project_id=o.project_id AND newer.switch_id=o.switch_id AND newer.event_type='data-served' AND (newer.occurred_at,newer.id)>(o.occurred_at,o.id)) AND NOT EXISTS(SELECT 1 FROM ztp_manageability_checks c WHERE c.id=(SELECT c2.id FROM ztp_manageability_checks c2 WHERE c2.observation_id=o.id AND c2.probe_hash=$2 ORDER BY c2.checked_at DESC,c2.id DESC LIMIT 1) AND (c.code='ready' OR c.next_retry_at IS NULL OR c.next_retry_at>now())) AND EXISTS(SELECT 1 FROM ztp_kea_deployments k JOIN media_items m ON m.id=split_part(split_part(k.spec->'options'->>'option_239_url','policy=',2),'&',1) WHERE k.project_id=o.project_id AND k.spec->'options'->>'batch_ref'=o.batch_ref AND k.active_hash=o.offer_hash AND k.state='boot_ready' AND m.type='ztp_script' AND m.state='approved' AND m.sha256<>$3 AND m.scope_key IN('global','project:'||o.project_id)) ORDER BY o.occurred_at DESC,o.id DESC LIMIT $1`
