package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/enzoOvOyu/keel/internal/fabricboot"
)

type ZTPIdentityHistoryInput struct {
	ReplacementID       string `json:"replacement_id,omitempty"`
	Kind                string `json:"kind"`
	FromSessionID       string `json:"from_session_id"`
	ToSessionID         string `json:"to_session_id"`
	FromExpectedVersion int64  `json:"from_expected_version"`
	ToExpectedVersion   int64  `json:"to_expected_version"`
	Reason              string `json:"reason"`
	Confirmed           bool   `json:"confirmed"`
}
type ZTPIdentityHistorySnapshot struct {
	SessionID    string    `json:"session_id"`
	StateVersion int64     `json:"state_version"`
	SwitchID     string    `json:"switch_id"`
	Serial       string    `json:"serial"`
	BatchRef     string    `json:"batch_ref"`
	BootChannel  string    `json:"boot_channel"`
	StartedAt    time.Time `json:"started_at"`
	ManagedAt    time.Time `json:"managed_at"`
}
type ZTPIdentityHistory struct {
	ReplacementID     string                     `json:"replacement_id,omitempty"`
	ID                string                     `json:"id"`
	ProjectID         string                     `json:"project_id"`
	Kind              string                     `json:"kind"`
	Mode              string                     `json:"mode"`
	Source            ZTPIdentityHistorySnapshot `json:"source"`
	Target            ZTPIdentityHistorySnapshot `json:"target"`
	Reason            string                     `json:"reason"`
	RecordedBy        string                     `json:"recorded_by"`
	RecordedAt        time.Time                  `json:"recorded_at"`
	DeclarationSource string                     `json:"declaration_source"`
}

const ztpHistoryColumns = "id,project_id,kind,mode,source_snapshot,target_snapshot,reason,recorded_by,recorded_at,coalesce(replacement_id,'')"

func scanZTPIdentityHistory(row scanner) (*ZTPIdentityHistory, error) {
	var out ZTPIdentityHistory
	var source, target []byte
	err := row.Scan(&out.ID, &out.ProjectID, &out.Kind, &out.Mode, &source, &target, &out.Reason, &out.RecordedBy, &out.RecordedAt, &out.ReplacementID)
	if err == nil {
		err = json.Unmarshal(source, &out.Source)
	}
	if err == nil {
		err = json.Unmarshal(target, &out.Target)
	}
	out.DeclarationSource = "operator_declared"
	return &out, err
}
func historySnapshotTx(ctx context.Context, tx *sql.Tx, session *ZTPSessionDetail) (ZTPIdentityHistorySnapshot, error) {
	var snapshot ZTPIdentityHistorySnapshot
	if session.State != "managed" || session.ManagedAt == nil || session.SwitchID == "" || session.IdentityResolution.Verdict != "matched" {
		return snapshot, apiError(409, "ZTP_HISTORY_SETTLED_REQUIRED", "Both sessions must have matched identity and completed management")
	}
	var raw []byte
	var serial string
	err := tx.QueryRowContext(ctx, `SELECT s.serial_number,z.identity_claim FROM switches s JOIN ztp_sessions z ON z.switch_id=s.id WHERE z.id=$1 AND z.project_id=$2 FOR SHARE OF s`, session.ID, session.ProjectID).Scan(&serial, &raw)
	if err != nil {
		return snapshot, err
	}
	var claim ZTPIdentityInput
	if json.Unmarshal(raw, &claim) != nil {
		return snapshot, apiError(409, "ZTP_HISTORY_IDENTITY_CHANGED", "Original identity claim is unavailable")
	}
	if claim.Serial != "" && claim.Serial != serial {
		return snapshot, apiError(409, "ZTP_HISTORY_IDENTITY_CHANGED", "Original serial no longer matches its physical identity")
	}
	snapshot = ZTPIdentityHistorySnapshot{SessionID: session.ID, StateVersion: session.StateVersion, SwitchID: session.SwitchID, Serial: serial, BatchRef: session.BatchRef, BootChannel: session.BootChannel, StartedAt: session.StartedAt, ManagedAt: *session.ManagedAt}
	return snapshot, nil
}

// Link only observed completed sessions. This declaration records the human's
// association, not hardware replacement, old-key removal or new device writes.
func (s *Store) RecordZTPIdentityHistory(ctx context.Context, a *AuthSession, project *ProjectSummary, in ZTPIdentityHistoryInput, trace string) (*ZTPIdentityHistory, error) {
	in.Reason = strings.TrimSpace(in.Reason)
	if !in.Confirmed || (in.Kind != "reinstall" && in.Kind != "rma") || !bootMediaID.MatchString(in.FromSessionID) || !bootMediaID.MatchString(in.ToSessionID) || in.FromSessionID == in.ToSessionID || in.FromExpectedVersion < 1 || in.ToExpectedVersion < 1 || utf8.RuneCountInString(in.Reason) < 3 || utf8.RuneCountInString(in.Reason) > 1000 || strings.ContainsAny(in.Reason, "\x00\r\n") {
		return nil, apiError(400, "ZTP_HISTORY_INVALID", "Confirm kind, distinct sessions, exact versions and a bounded single-line reason")
	}
	tx, err := s.beginControl(ctx, a)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = requireFabricPermission(ctx, tx, a, project.ID, "fabric:plan"); err != nil {
		return nil, err
	}
	// Project-scoped advisory lock and stable row order serialize duplicate links.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "ztp.identity.history:"+project.ID); err != nil {
		return nil, err
	}
	existing, err := scanZTPIdentityHistory(tx.QueryRowContext(ctx, "SELECT "+ztpHistoryColumns+" FROM ztp_identity_history WHERE project_id=$1 AND to_session_id=$2", project.ID, in.ToSessionID))
	requestHash := fabricboot.ManagementPlanDigest(in)
	if err == nil {
		var savedHash string
		if err = tx.QueryRowContext(ctx, `SELECT request_hash FROM ztp_identity_history WHERE id=$1`, existing.ID).Scan(&savedHash); err != nil {
			return nil, err
		}
		if savedHash != requestHash || existing.RecordedBy != a.User.ID {
			return nil, apiError(409, "ZTP_HISTORY_ALREADY_RECORDED", "This session already has an immutable predecessor")
		}
		return existing, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	var replacement *ZTPReplacement
	if in.Kind == "rma" {
		if !bootMediaID.MatchString(in.ReplacementID) {
			return nil, apiError(409, "ZTP_RMA_APPROVAL_REQUIRED", "RMA lineage requires an independently approved replacement")
		}
		replacement, err = scanZTPReplacement(tx.QueryRowContext(ctx, "SELECT "+ztpReplacementFields+" FROM ztp_replacements WHERE project_id=$1 AND id=$2 FOR SHARE", project.ID, in.ReplacementID))
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		if replacement.State != "approved" || replacement.ApprovedAt == nil || replacement.Snapshot.FromSessionID != in.FromSessionID {
			return nil, apiError(409, "ZTP_RMA_APPROVAL_REQUIRED", "Replacement approval does not cover the original session")
		}
	} else if in.ReplacementID != "" {
		return nil, apiError(400, "ZTP_HISTORY_INVALID", "Reinstall does not consume an RMA approval")
	}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM ztp_sessions WHERE project_id=$1 AND id IN($2,$3) ORDER BY id FOR UPDATE`, project.ID, in.FromSessionID, in.ToSessionID)
	if err != nil {
		return nil, err
	}
	count := 0
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			break
		}
		count++
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, err
	}
	if count != 2 {
		return nil, ErrNotFound
	}
	from, err := getZTPSessionTx(ctx, tx, project.ID, in.FromSessionID, false)
	if err != nil {
		return nil, err
	}
	to, err := getZTPSessionTx(ctx, tx, project.ID, in.ToSessionID, false)
	if err != nil {
		return nil, err
	}
	if from.StateVersion != in.FromExpectedVersion || to.StateVersion != in.ToExpectedVersion {
		return nil, apiError(409, "ZTP_STATE_VERSION_CONFLICT", "Session version changed")
	}
	if from.Mode != to.Mode {
		return nil, apiError(409, "ZTP_HISTORY_MODE_MISMATCH", "Simulation and device histories cannot be combined")
	}
	source, err := historySnapshotTx(ctx, tx, from)
	if err != nil {
		return nil, err
	}
	target, err := historySnapshotTx(ctx, tx, to)
	if err != nil {
		return nil, err
	}
	if !target.StartedAt.After(source.StartedAt) || source.ManagedAt.After(target.StartedAt) {
		return nil, apiError(409, "ZTP_HISTORY_ORDER_INVALID", "The replacement session must start after its predecessor completed")
	}
	if in.Kind == "reinstall" && source.SwitchID != target.SwitchID || in.Kind == "rma" && (source.SwitchID == target.SwitchID || source.Serial == target.Serial) {
		return nil, apiError(409, "ZTP_HISTORY_KIND_MISMATCH", "Reinstall keeps physical identity; RMA requires a different BOM device and serial")
	}
	if replacement != nil && (replacement.Snapshot.Old.SwitchID != source.SwitchID || replacement.Snapshot.New.SwitchID != target.SwitchID || replacement.Snapshot.Mode != to.Mode || target.StartedAt.Before(*replacement.ApprovedAt) || replacement.Snapshot.Old.Serial != source.Serial || replacement.Snapshot.New.Serial != target.Serial) {
		return nil, apiError(409, "ZTP_RMA_APPROVAL_SCOPE_MISMATCH", "Replacement approval must precede and cover the exact new identity and evidence mode")
	}
	if replacement != nil {
		stale, e := activeReplacementStaleTx(ctx, tx, project.ID, target.SwitchID, to.Mode)
		if e != nil {
			return nil, e
		}
		if stale {
			return nil, apiError(409, "ZTP_RMA_SNAPSHOT_CHANGED", "New binding facts changed after replacement approval")
		}
	}
	now := time.Now().UTC()
	out := &ZTPIdentityHistory{ID: RandomID(), ProjectID: project.ID, Kind: in.Kind, Mode: to.Mode, Source: source, Target: target, Reason: in.Reason, RecordedBy: a.User.ID, RecordedAt: now, DeclarationSource: "operator_declared"}
	sourceRaw, _ := json.Marshal(source)
	targetRaw, _ := json.Marshal(target)
	out.ReplacementID = in.ReplacementID
	_, err = tx.ExecContext(ctx, `INSERT INTO ztp_identity_history(id,project_id,kind,from_session_id,to_session_id,from_switch_id,to_switch_id,mode,source_snapshot,target_snapshot,reason,request_hash,recorded_by,recorded_at,replacement_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,NULLIF($15,''))`, out.ID, project.ID, in.Kind, from.ID, to.ID, source.SwitchID, target.SwitchID, to.Mode, sourceRaw, targetRaw, in.Reason, requestHash, a.User.ID, now, in.ReplacementID)
	if err != nil {
		return nil, err
	}
	event := ZTPSessionEvent{Seq: to.StateVersion + 1, FromState: to.State, ToState: to.State, EventCode: "history." + in.Kind, EvidenceRef: "identity-history:" + out.ID, Attempt: to.Attempt, OccurredAt: now}
	if err = insertZTPEvent(ctx, tx, to.ID, event); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE ztp_sessions SET state_version=$2,updated_at=$3 WHERE id=$1`, to.ID, event.Seq, now); err != nil {
		return nil, err
	}
	if err = appendAudit(ctx, tx, a.User.ID, project.ID, "ztp.identity_history.record", out.ID, trace, map[string]any{"kind": in.Kind, "from_session_id": from.ID, "to_session_id": to.ID, "mode": to.Mode, "source": "operator_declared"}); err != nil {
		return nil, err
	}
	return out, tx.Commit()
}
func (s *Store) ListZTPIdentityHistory(ctx context.Context, a *AuthSession, project *ProjectSummary) ([]ZTPIdentityHistory, error) {
	tx, err := s.beginControlRead(ctx, a)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = requireProjectMember(ctx, tx, a, project.ID); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT "+ztpHistoryColumns+" FROM ztp_identity_history WHERE project_id=$1 ORDER BY recorded_at DESC,id DESC LIMIT 100", project.ID)
	if err != nil {
		return nil, err
	}
	out := []ZTPIdentityHistory{}
	for rows.Next() {
		item, e := scanZTPIdentityHistory(rows)
		if e != nil {
			rows.Close()
			return nil, e
		}
		out = append(out, *item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	return out, tx.Commit()
}
