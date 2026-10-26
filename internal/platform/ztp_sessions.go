package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/enzoOvOyu/keel/internal/fabricboot"
	"strconv"
	"strings"
	"time"
)

type ZTPSimSessionInput struct {
	BatchRef    string           `json:"batch_ref"`
	BootChannel string           `json:"boot_channel"`
	Identity    ZTPIdentityInput `json:"identity"`
}

type ZTPSessionAction struct {
	DORA              *fabricboot.DORARequest `json:"dora,omitempty"`
	Action            string                  `json:"action"`
	ExpectedVersion   int64                   `json:"expected_version"`
	EvidenceRef       string                  `json:"evidence_ref"`
	CredentialVerdict string                  `json:"credential_verdict,omitempty"`
	Phenomenon        string                  `json:"phenomenon"`
	Cause             string                  `json:"cause"`
	NextAction        string                  `json:"next_action"`
}

type ZTPSessionEvent struct {
	Seq         int64     `json:"seq"`
	FromState   string    `json:"from_state,omitempty"`
	ToState     string    `json:"to_state"`
	EventCode   string    `json:"event_code"`
	EvidenceRef string    `json:"evidence_ref,omitempty"`
	Attempt     int       `json:"attempt"`
	OccurredAt  time.Time `json:"occurred_at"`
}

type ZTPSessionDetail struct {
	ID                 string                `json:"id"`
	ProjectID          string                `json:"project_id"`
	SwitchID           string                `json:"switch_id,omitempty"`
	BatchRef           string                `json:"batch_ref"`
	Mode               string                `json:"mode"`
	BootChannel        string                `json:"boot_channel"`
	State              string                `json:"state"`
	StateVersion       int64                 `json:"state_version"`
	Attempt            int                   `json:"attempt"`
	RetryMax           int                   `json:"retry_max"`
	IdentityResolution ZTPIdentityResolution `json:"identity_resolution"`
	FailurePhenomenon  string                `json:"failure_phenomenon,omitempty"`
	FailureCause       string                `json:"failure_cause,omitempty"`
	FailureNextAction  string                `json:"failure_next_action,omitempty"`
	NextRetryAt        *time.Time            `json:"next_retry_at,omitempty"`
	StartedAt          time.Time             `json:"started_at"`
	UpdatedAt          time.Time             `json:"updated_at"`
	ManagedAt          *time.Time            `json:"managed_at,omitempty"`
	Events             []ZTPSessionEvent     `json:"events"`
}

func (s *Store) CreateZTPSimSession(ctx context.Context, a *AuthSession, project *ProjectSummary, input ZTPSimSessionInput, trace string) (*ZTPSessionDetail, error) {
	if !validZTPChannel(input.BootChannel) || !validZTPEvidence(input.BatchRef) || len(input.BatchRef) > 160 {
		return nil, apiError(400, "ZTP_SESSION_INVALID", "Invalid batch or boot channel")
	}
	resolution, err := s.ResolveZTPIdentity(ctx, a, project, input.Identity)
	if err != nil {
		return nil, err
	}
	claimJSON, err := json.Marshal(input.Identity)
	if err != nil {
		return nil, err
	}
	resolutionJSON, err := json.Marshal(resolution)
	if err != nil {
		return nil, err
	}
	tx, err := s.beginControl(ctx, a)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = requireFabricPermission(ctx, tx, a, project.ID, "fabric:plan"); err != nil {
		return nil, err
	}
	retryMax := 3
	values, cfgErr := catalogDefaultsTx(ctx, tx, project.ID, "fabric.ztp_retry_max")
	if cfgErr != nil {
		return nil, cfgErr
	}
	if configured, ok := values["fabric.ztp_retry_max"]; ok {
		parsed, parseErr := strconv.Atoi(configured)
		if parseErr != nil || parsed < 0 || parsed > 10 {
			return nil, apiError(409, "ZTP_RETRY_CONFIG_INVALID", "Invalid configured ZTP retry maximum")
		}
		retryMax = parsed
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	out := &ZTPSessionDetail{ID: RandomID(), ProjectID: project.ID, SwitchID: resolution.SwitchID, BatchRef: input.BatchRef, Mode: "sim", BootChannel: input.BootChannel, State: "powered", StateVersion: 1, Attempt: 1, RetryMax: retryMax, IdentityResolution: *resolution, StartedAt: now, UpdatedAt: now, Events: []ZTPSessionEvent{}}
	_, err = tx.ExecContext(ctx, `INSERT INTO ztp_sessions(id,project_id,switch_id,batch_ref,mode,boot_channel,state,state_version,attempt,retry_max,identity_claim,identity_resolution,created_by,started_at,updated_at) VALUES($1,$2,$3,$4,'sim',$5,'powered',1,1,$6,$7,$8,$9,$10,$10)`, out.ID, project.ID, nullBOMString(out.SwitchID), out.BatchRef, out.BootChannel, retryMax, claimJSON, resolutionJSON, a.User.ID, now)
	if err != nil {
		return nil, err
	}
	event := ZTPSessionEvent{Seq: 1, ToState: "powered", EventCode: "sim.powered", Attempt: 1, OccurredAt: now}
	if err = insertZTPEvent(ctx, tx, out.ID, event); err != nil {
		return nil, err
	}
	out.Events = append(out.Events, event)
	if err = appendAudit(ctx, tx, a.User.ID, project.ID, "ztp.sim.create", out.ID, trace, map[string]any{"batch_ref": out.BatchRef, "channel": out.BootChannel, "identity_verdict": resolution.Verdict}); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

func insertZTPEvent(ctx context.Context, tx *sql.Tx, sessionID string, event ZTPSessionEvent) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO ztp_session_events(id,session_id,seq,from_state,to_state,event_code,evidence_ref,attempt,occurred_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, RandomID(), sessionID, event.Seq, nullBOMString(event.FromState), event.ToState, event.EventCode, nullBOMString(event.EvidenceRef), event.Attempt, event.OccurredAt)
	return err
}

func getZTPSessionTx(ctx context.Context, tx *sql.Tx, projectID, sessionID string, lock bool) (*ZTPSessionDetail, error) {
	query := `SELECT project_id,coalesce(switch_id,''),batch_ref,mode,boot_channel,state,state_version,attempt,retry_max,identity_resolution,coalesce(failure_phenomenon,''),coalesce(failure_cause,''),coalesce(failure_next_action,''),next_retry_at,started_at,updated_at,managed_at FROM ztp_sessions WHERE id=$1 AND project_id=$2`
	if lock {
		query += " FOR UPDATE"
	}
	out := &ZTPSessionDetail{ID: sessionID, Events: []ZTPSessionEvent{}}
	var raw []byte
	var retry, managed sql.NullTime
	err := tx.QueryRowContext(ctx, query, sessionID, projectID).Scan(&out.ProjectID, &out.SwitchID, &out.BatchRef, &out.Mode, &out.BootChannel, &out.State, &out.StateVersion, &out.Attempt, &out.RetryMax, &raw, &out.FailurePhenomenon, &out.FailureCause, &out.FailureNextAction, &retry, &out.StartedAt, &out.UpdatedAt, &managed)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(raw, &out.IdentityResolution); err != nil {
		return nil, err
	}
	if retry.Valid {
		out.NextRetryAt = &retry.Time
	}
	if managed.Valid {
		out.ManagedAt = &managed.Time
	}
	return out, nil
}

func loadZTPEvents(ctx context.Context, tx *sql.Tx, out *ZTPSessionDetail) error {
	rows, err := tx.QueryContext(ctx, `SELECT seq,coalesce(from_state,''),to_state,event_code,coalesce(evidence_ref,''),attempt,occurred_at FROM ztp_session_events WHERE session_id=$1 ORDER BY seq`, out.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var event ZTPSessionEvent
		if err = rows.Scan(&event.Seq, &event.FromState, &event.ToState, &event.EventCode, &event.EvidenceRef, &event.Attempt, &event.OccurredAt); err != nil {
			return err
		}
		out.Events = append(out.Events, event)
	}
	return rows.Err()
}

type ZTPSessionSummary struct {
	ID              string    `json:"id"`
	BatchRef        string    `json:"batch_ref"`
	BootChannel     string    `json:"boot_channel"`
	State           string    `json:"state"`
	StateVersion    int64     `json:"state_version"`
	Attempt         int       `json:"attempt"`
	IdentityVerdict string    `json:"identity_verdict"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func (s *Store) ListZTPSimSessions(ctx context.Context, a *AuthSession, project *ProjectSummary) ([]ZTPSessionSummary, error) {
	tx, err := s.beginControlRead(ctx, a)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = requireProjectMember(ctx, tx, a, project.ID); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,batch_ref,boot_channel,state,state_version,attempt,identity_resolution->>'verdict',updated_at FROM ztp_sessions WHERE project_id=$1 AND mode='sim' ORDER BY updated_at DESC,id DESC LIMIT 100`, project.ID)
	if err != nil {
		return nil, err
	}
	out := []ZTPSessionSummary{}
	for rows.Next() {
		var entry ZTPSessionSummary
		if err = rows.Scan(&entry.ID, &entry.BatchRef, &entry.BootChannel, &entry.State, &entry.StateVersion, &entry.Attempt, &entry.IdentityVerdict, &entry.UpdatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, entry)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	return out, tx.Commit()
}

func (s *Store) GetZTPSession(ctx context.Context, a *AuthSession, project *ProjectSummary, sessionID string) (*ZTPSessionDetail, error) {
	tx, err := s.beginControlRead(ctx, a)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = requireProjectMember(ctx, tx, a, project.ID); err != nil {
		return nil, err
	}
	out, err := getZTPSessionTx(ctx, tx, project.ID, sessionID, false)
	if err != nil {
		return nil, err
	}
	if out.Mode != "sim" {
		return nil, ErrNotFound
	}
	if err = loadZTPEvents(ctx, tx, out); err != nil {
		return nil, err
	}
	return out, tx.Commit()
}

func (s *Store) AdvanceZTPSimSession(ctx context.Context, a *AuthSession, project *ProjectSummary, sessionID string, input ZTPSessionAction, trace string) (*ZTPSessionDetail, error) {
	if input.ExpectedVersion < 1 {
		return nil, apiError(400, "ZTP_VERSION_REQUIRED", "Expected state version is required")
	}
	if input.Action != "advance" && input.Action != "fail" && input.Action != "retry" {
		return nil, apiError(400, "ZTP_ACTION_INVALID", "Invalid simulation action")
	}
	if input.EvidenceRef != "" && !validZTPEvidence(input.EvidenceRef) {
		return nil, apiError(400, "ZTP_EVIDENCE_INVALID", "Invalid stage evidence reference")
	}
	if input.Action == "fail" {
		for _, value := range []string{input.Phenomenon, input.Cause, input.NextAction} {
			if strings.TrimSpace(value) == "" || len(value) > 1000 {
				return nil, apiError(400, "ZTP_FAILURE_DETAIL_REQUIRED", "Phenomenon, cause and next action are required")
			}
		}
	}
	tx, err := s.beginControl(ctx, a)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = requireFabricPermission(ctx, tx, a, project.ID, "fabric:plan"); err != nil {
		return nil, err
	}
	out, err := s.advanceZTPSimSessionTx(ctx, tx, project.ID, sessionID, input, a.User.ID, trace)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) advanceZTPSimSessionTx(ctx context.Context, tx *sql.Tx, projectID, sessionID string, input ZTPSessionAction, actorID, trace string) (*ZTPSessionDetail, error) {
	out, err := getZTPSessionTx(ctx, tx, projectID, sessionID, true)
	if err != nil {
		return nil, err
	}
	if out.Mode != "sim" {
		return nil, apiError(409, "ZTP_DEVICE_SESSION_READ_ONLY", "Simulation actions cannot change a device session")
	}
	if out.StateVersion != input.ExpectedVersion {
		return nil, apiError(409, "ZTP_STATE_VERSION_CONFLICT", "ZTP session state changed")
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	original := out.State
	events := []ZTPSessionEvent{}
	add := func(next, code, evidence string) {
		events = append(events, ZTPSessionEvent{Seq: out.StateVersion + int64(len(events)) + 1, FromState: out.State, ToState: next, EventCode: code, EvidenceRef: evidence, Attempt: out.Attempt, OccurredAt: now})
		out.State = next
	}
	switch input.Action {
	case "advance":
		next := nextZTPStage(out.State)
		if next == "" {
			return nil, apiError(409, "ZTP_TRANSITION_INVALID", "No next ZTP stage")
		}
		evidence := input.EvidenceRef
		if next == "dhcp" {
			if input.DORA == nil {
				return nil, apiError(400, "ZTP_DORA_REQUIRED", "DORA request is required for DHCP stage")
			}
			request := *input.DORA
			request.ProjectID = projectID
			request.BatchRef = out.BatchRef
			request.StageRef = "N-S4"
			offer, offerErr := simulateZTPDORATx(ctx, tx, projectID, request)
			if offerErr != nil {
				return nil, offerErr
			}

			if out.BootChannel == "136" {
				if offer.Protocol != "dhcp6" || offer.SwitchID != out.SwitchID || len(offer.Option136URIs) == 0 {
					return nil, apiError(409, "ZTP_DHCP6_BINDING_MISMATCH", "IPv6 session requires an exact DUID and resolved switch binding")
				}
			} else if offer.Protocol == "dhcp6" {
				return nil, apiError(409, "ZTP_DHCP_CHANNEL_MISMATCH", "IPv6 offers cannot satisfy an IPv4 session")
			}
			if offer.Outcome != "ack" {
				return nil, apiError(409, "ZTP_DHCP_NO_OFFER", "No committed mock Kea offer for management interface")
			}
			evidence = "mock-kea:" + offer.ConfigHash
		} else if next == "booturl" || next == "fetch" {
			cfg, activeHash, loadErr := activeZTPSimOfferTx(ctx, tx, projectID, out.BatchRef)
			if loadErr != nil {
				return nil, loadErr
			}
			if loadErr = requireSimDHCPHashTx(ctx, tx, out.ID, out.Attempt, activeHash); loadErr != nil {
				return nil, loadErr
			}
			if next == "booturl" {
				if (out.BootChannel == "143" || out.BootChannel == "136") && len(cfg.Option143URIs) == 0 {
					return nil, apiError(409, "ZTP_BOOT_URI_MISSING", "No active SZTP URI")
				}
				if out.BootChannel != "143" && out.BootChannel != "136" && cfg.Option239URL == "" {
					return nil, apiError(409, "ZTP_BOOT_URI_MISSING", "No active Cumulus boot URL")
				}
				evidence = "mock-kea-url:" + activeHash
			} else {
				if out.IdentityResolution.Verdict != "matched" {
					return nil, apiError(409, "ZTP_IDENTITY_NOT_MATCHED", "Unmatched identity cannot fetch configuration")
				}
				evidence, loadErr = approvedZTPSimArtifactTx(ctx, tx, projectID, cfg, out.BootChannel)
				if loadErr != nil {
					return nil, loadErr
				}
			}
		} else if next == "lookup" {
			var rawClaim []byte
			if err = tx.QueryRowContext(ctx, `SELECT identity_claim FROM ztp_sessions WHERE id=$1`, out.ID).Scan(&rawClaim); err != nil {
				return nil, err
			}
			var claim ZTPIdentityInput
			if err = json.Unmarshal(rawClaim, &claim); err != nil {
				return nil, err
			}
			resolution, lookupErr := resolveZTPIdentityTx(ctx, tx, projectID, claim, "sim")
			if lookupErr != nil {
				return nil, lookupErr
			}
			out.IdentityResolution = *resolution
			out.SwitchID = resolution.SwitchID
			evidence = "sot-lookup:" + resolution.Verdict
			if resolution.SwitchID != "" {
				evidence += ":" + resolution.SwitchID
			}
			if resolution.Verdict == "conflict" {
				identityKey := ztpIssueIdentityKey(claim)
				if err = upsertZTPIdentityIssueTx(ctx, tx, projectID, out.BatchRef, "sim", identityKey, claim.Serial, "", trace, resolution.Reasons); err != nil {
					return nil, err
				}
			}
		} else if !validZTPEvidence(evidence) {
			return nil, apiError(400, "ZTP_EVIDENCE_REQUIRED", "Stage evidence reference is required")
		}
		if next == "config" && out.IdentityResolution.Verdict != "matched" {
			return nil, apiError(409, "ZTP_IDENTITY_NOT_MATCHED", "Unmatched identity cannot import configuration")
		}
		if next == "managed" && out.SwitchID != "" {
			ref, refErr := simSwitchCredentialRef(ctx, tx, projectID, out.SwitchID)
			if refErr != nil {
				return nil, refErr
			}
			if ref != "" {
				registered, checkErr := simCredentialRegistered(ctx, tx, projectID, ref)
				if checkErr != nil {
					return nil, checkErr
				}
				verdict := input.CredentialVerdict
				if !registered {
					verdict = "unregistered"
				} else if verdict != "reachable" && verdict != "expired" && verdict != "unreachable" {
					return nil, apiError(400, "ZTP_SIM_CREDENTIAL_VERDICT_REQUIRED", "Provide a simulated credential verdict")
				}
				if verdict != "reachable" {
					if err = insertZTPCredentialIssueTx(ctx, tx, projectID, out, ref, verdict, now); err != nil {
						return nil, err
					}
					add("failed", "sim.credential_"+verdict, "mock-credential:"+ref)
					out.FailurePhenomenon = "Simulated switch credential check: " + verdict
					out.FailureCause = "Credential batch " + ref + " is " + verdict + " in this simulation"
					out.FailureNextAction = "Review switch credential batch reference and Vault rotation, then retry ZTP"
					if out.Attempt <= out.RetryMax {
						retry := now.Add(ztpRetryDelay(out.Attempt))
						out.NextRetryAt = &retry
					}
				} else {
					evidence = "mock-credential:" + ref + ":reachable"
				}
			}
		}
		if out.State != "failed" {
			add(next, "sim."+next, evidence)
		}

		if next == "lookup" && out.IdentityResolution.Verdict != "matched" {
			if out.IdentityResolution.Verdict == "conflict" {
				add("conflict", "sim.identity_conflict", "")
			} else {
				cfg, activeHash, loadErr := activeZTPSimOfferTx(ctx, tx, projectID, out.BatchRef)
				if loadErr != nil {
					return nil, loadErr
				}
				if loadErr = requireSimDHCPHashTx(ctx, tx, out.ID, out.Attempt, activeHash); loadErr != nil {
					return nil, loadErr
				}
				artifact, artifactErr := approvedZTPSimArtifactTx(ctx, tx, projectID, cfg, out.BootChannel)
				if artifactErr != nil {
					return nil, artifactErr
				}
				add("fetch", "sim.isolation_fetch", artifact)
				add("isolated", "sim.identity_isolated", "")
			}
		}
		if out.State == "managed" {
			out.ManagedAt = &now
		}
	case "fail":
		if nextZTPStage(out.State) == "" {
			return nil, apiError(409, "ZTP_TRANSITION_INVALID", "Terminal session cannot fail again")
		}
		add("failed", "sim.failed", input.EvidenceRef)
		out.FailurePhenomenon = input.Phenomenon
		out.FailureCause = input.Cause
		out.FailureNextAction = input.NextAction
		if out.Attempt <= out.RetryMax {
			retry := now.Add(ztpRetryDelay(out.Attempt))
			out.NextRetryAt = &retry
		}
	case "retry":
		if out.State != "failed" || out.Attempt > out.RetryMax || out.NextRetryAt == nil {
			return nil, apiError(409, "ZTP_RETRY_NOT_ALLOWED", "No retry is available")
		}
		if now.Before(*out.NextRetryAt) {
			return nil, apiError(409, "ZTP_RETRY_TOO_EARLY", "Retry backoff has not elapsed")
		}
		out.Attempt++
		out.FailurePhenomenon = ""
		out.FailureCause = ""
		out.FailureNextAction = ""
		out.NextRetryAt = nil
		add("powered", "sim.retry", input.EvidenceRef)
	}
	for _, event := range events {
		if err = insertZTPEvent(ctx, tx, out.ID, event); err != nil {
			return nil, err
		}
	}
	out.StateVersion += int64(len(events))
	out.UpdatedAt = now
	resolutionJSON, marshalErr := json.Marshal(out.IdentityResolution)
	if marshalErr != nil {
		return nil, marshalErr
	}
	_, err = tx.ExecContext(ctx, `UPDATE ztp_sessions SET state=$2,state_version=$3,attempt=$4,failure_phenomenon=$5,failure_cause=$6,failure_next_action=$7,next_retry_at=$8,updated_at=$9,managed_at=$10,identity_resolution=$11,switch_id=NULLIF($12,'') WHERE id=$1`, out.ID, out.State, out.StateVersion, out.Attempt, nullBOMString(out.FailurePhenomenon), nullBOMString(out.FailureCause), nullBOMString(out.FailureNextAction), out.NextRetryAt, now, out.ManagedAt, resolutionJSON, out.SwitchID)
	if err != nil {
		return nil, err
	}
	auditDetail := map[string]any{"from": original, "to": out.State, "attempt": out.Attempt}
	if out.State == "failed" {
		auditDetail["phenomenon"] = out.FailurePhenomenon
		auditDetail["cause"] = out.FailureCause
		auditDetail["next_action"] = out.FailureNextAction
	}
	if input.Action == "fail" {
		auditDetail["phenomenon"] = input.Phenomenon
		auditDetail["cause"] = input.Cause
		auditDetail["next_action"] = input.NextAction
	}
	if err = appendAudit(ctx, tx, actorID, projectID, "ztp.sim."+input.Action, out.ID, trace, auditDetail); err != nil {
		return nil, err
	}
	if err = loadZTPEvents(ctx, tx, out); err != nil {
		return nil, err
	}
	return out, nil
}
