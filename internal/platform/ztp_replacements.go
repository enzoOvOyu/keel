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

type ZTPReplacementDevice struct {
	SwitchID string            `json:"switch_id"`
	Serial   string            `json:"serial"`
	MgmtMAC  string            `json:"mgmt_mac"`
	Position map[string]string `json:"position"`
}
type ZTPReplacementSnapshot struct {
	FromSessionID      string               `json:"from_session_id"`
	FromSessionVersion int64                `json:"from_session_version"`
	Mode               string               `json:"mode"`
	Old                ZTPReplacementDevice `json:"old"`
	New                ZTPReplacementDevice `json:"new"`
}
type ZTPReplacement struct {
	ID             string                 `json:"id"`
	ProjectID      string                 `json:"project_id"`
	State          string                 `json:"state"`
	StateVersion   int64                  `json:"state_version"`
	Snapshot       ZTPReplacementSnapshot `json:"snapshot"`
	SnapshotSHA256 string                 `json:"snapshot_sha256"`
	Reason         string                 `json:"reason"`
	CreatedBy      string                 `json:"created_by"`
	CreatedAt      time.Time              `json:"created_at"`
	ApprovedBy     string                 `json:"approved_by,omitempty"`
	ApprovedAt     *time.Time             `json:"approved_at,omitempty"`
}
type ZTPReplacementInput struct {
	FromSessionID       string `json:"from_session_id"`
	FromExpectedVersion int64  `json:"from_expected_version"`
	ToSwitchID          string `json:"to_switch_id"`
	Reason              string `json:"reason"`
	Confirmed           bool   `json:"confirmed"`
}
type ZTPReplacementApproval struct {
	ExpectedVersion int64  `json:"expected_version"`
	SnapshotSHA256  string `json:"snapshot_sha256"`
	Confirmed       bool   `json:"confirmed"`
}

const ztpReplacementFields = "id,project_id,state,state_version,snapshot,snapshot_sha256,reason,created_by,created_at,coalesce(approved_by,''),approved_at"

func scanZTPReplacement(row scanner) (*ZTPReplacement, error) {
	var out ZTPReplacement
	var raw []byte
	err := row.Scan(&out.ID, &out.ProjectID, &out.State, &out.StateVersion, &raw, &out.SnapshotSHA256, &out.Reason, &out.CreatedBy, &out.CreatedAt, &out.ApprovedBy, &out.ApprovedAt)
	if err == nil {
		err = json.Unmarshal(raw, &out.Snapshot)
	}
	return &out, err
}
func replacementDeviceTx(ctx context.Context, tx *sql.Tx, project, id, mode string) (ZTPReplacementDevice, error) {
	out := ZTPReplacementDevice{SwitchID: id}
	var raw []byte
	err := tx.QueryRowContext(ctx, `SELECT s.serial_number,b.position FROM switches s JOIN bom_items b ON b.item_uid=s.id WHERE s.id=$1 AND b.status<>'retired' AND EXISTS(SELECT 1 FROM bom_item_revisions r WHERE r.project_id=$2 AND r.item_uid=s.id) FOR SHARE OF s,b`, id, project).Scan(&out.Serial, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrNotFound
	}
	if err != nil {
		return out, err
	}
	if json.Unmarshal(raw, &out.Position) != nil || strings.TrimSpace(out.Position["dc"]) == "" || strings.TrimSpace(out.Position["rack"]) == "" || strings.TrimSpace(out.Position["idx"]) == "" {
		return out, apiError(409, "ZTP_RMA_POSITION_REQUIRED", "BOM datacenter, rack and rack index must be explicit")
	}
	err = tx.QueryRowContext(ctx, `SELECT mac FROM switch_mac_bindings WHERE switch_id=$1 AND kind='mgmt' FOR SHARE`, id).Scan(&out.MgmtMAC)
	if errors.Is(err, sql.ErrNoRows) {
		return out, apiError(409, "ZTP_RMA_MAC_REQUIRED", "Both BOM devices require management MAC identities")
	}
	if err != nil {
		return out, err
	}
	var retired bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM ztp_switch_project_bindings WHERE project_id=$1 AND switch_id=$2 AND mode=$3 AND state='retired')`, project, id, mode).Scan(&retired); err != nil {
		return out, err
	}
	if retired {
		return out, apiError(409, "ZTP_RMA_BINDING_RETIRED", "A retired project binding cannot be reused")
	}
	return out, nil
}
func replacementSnapshotTx(ctx context.Context, tx *sql.Tx, project string, in ZTPReplacementInput) (ZTPReplacementSnapshot, error) {
	var out ZTPReplacementSnapshot
	old, err := getZTPSessionTx(ctx, tx, project, in.FromSessionID, true)
	if err != nil {
		return out, err
	}
	if old.StateVersion != in.FromExpectedVersion {
		return out, apiError(409, "ZTP_STATE_VERSION_CONFLICT", "Original session changed")
	}
	if _, err = historySnapshotTx(ctx, tx, old); err != nil {
		return out, err
	}
	if old.SwitchID == in.ToSwitchID {
		return out, apiError(409, "ZTP_RMA_IDENTITY_INVALID", "RMA requires a separate physical identity")
	}
	out.FromSessionID, out.FromSessionVersion, out.Mode = old.ID, old.StateVersion, old.Mode
	out.Old, err = replacementDeviceTx(ctx, tx, project, old.SwitchID, old.Mode)
	if err != nil {
		return out, err
	}
	out.New, err = replacementDeviceTx(ctx, tx, project, in.ToSwitchID, old.Mode)
	if err != nil {
		return out, err
	}
	if out.Old.Serial == out.New.Serial || out.Old.MgmtMAC == out.New.MgmtMAC || fabricboot.ManagementPlanDigest(out.Old.Position) != fabricboot.ManagementPlanDigest(out.New.Position) {
		return out, apiError(409, "ZTP_RMA_IDENTITY_INVALID", "Check distinct old/new SN and MAC at the same BOM position")
	}
	var used bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM ztp_replacements WHERE project_id=$1 AND mode=$2 AND state='approved' AND (from_switch_id=$3 OR to_switch_id=$4))`, project, old.Mode, old.SwitchID, in.ToSwitchID).Scan(&used); err != nil {
		return out, err
	}
	if used {
		return out, apiError(409, "ZTP_RMA_BINDING_CONFLICT", "A device is already bound by an approved replacement")
	}
	return out, nil
}
func (s *Store) CreateZTPReplacement(ctx context.Context, a *AuthSession, p *ProjectSummary, in ZTPReplacementInput, trace string) (*ZTPReplacement, error) {
	in.Reason = strings.TrimSpace(in.Reason)
	if !in.Confirmed || !bootMediaID.MatchString(in.FromSessionID) || !bootMediaID.MatchString(in.ToSwitchID) || in.FromExpectedVersion < 1 || utf8.RuneCountInString(in.Reason) < 3 || utf8.RuneCountInString(in.Reason) > 1000 || strings.ContainsAny(in.Reason, "\x00\r\n") {
		return nil, apiError(400, "ZTP_RMA_INVALID", "Confirm original session, new BOM device and a bounded single-line reason")
	}
	tx, err := s.beginControl(ctx, a)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = requireFabricPermission(ctx, tx, a, p.ID, "fabric:plan"); err != nil {
		return nil, err
	}
	if err = lockKey(ctx, tx, "ztp.rma:"+p.ID); err != nil {
		return nil, err
	}
	snapshot, err := replacementSnapshotTx(ctx, tx, p.ID, in)
	if err != nil {
		return nil, err
	}
	hash := fabricboot.ManagementPlanDigest(snapshot)
	prior, err := scanZTPReplacement(tx.QueryRowContext(ctx, "SELECT "+ztpReplacementFields+" FROM ztp_replacements WHERE project_id=$1 AND snapshot_sha256=$2 AND created_by=$3 AND reason=$4 AND state='requested' ORDER BY created_at DESC LIMIT 1", p.ID, hash, a.User.ID, in.Reason))
	if err == nil {
		return prior, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	var pending bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM ztp_replacements WHERE project_id=$1 AND mode=$2 AND state='requested' AND (from_switch_id=$3 OR to_switch_id=$4))`, p.ID, snapshot.Mode, snapshot.Old.SwitchID, snapshot.New.SwitchID).Scan(&pending); err != nil {
		return nil, err
	}
	if pending {
		return nil, apiError(409, "ZTP_RMA_REQUEST_PENDING", "Review or reject the original pending replacement first")
	}
	out := &ZTPReplacement{ID: RandomID(), ProjectID: p.ID, State: "requested", StateVersion: 1, Snapshot: snapshot, SnapshotSHA256: hash, Reason: in.Reason, CreatedBy: a.User.ID, CreatedAt: time.Now().UTC()}
	rawSnapshot, err := json.Marshal(snapshot)
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO ztp_replacements(id,project_id,from_session_id,from_switch_id,to_switch_id,mode,state,snapshot,snapshot_sha256,reason,created_by,created_at) VALUES($1,$2,$3,$4,$5,$6,'requested',$7,$8,$9,$10,$11)`, out.ID, p.ID, in.FromSessionID, snapshot.Old.SwitchID, snapshot.New.SwitchID, snapshot.Mode, rawSnapshot, hash, in.Reason, a.User.ID, out.CreatedAt)
	if err != nil {
		return nil, err
	}
	if err = appendAudit(ctx, tx, a.User.ID, p.ID, "ztp.replacement.request", out.ID, trace, map[string]any{"snapshot_sha256": hash, "mode": snapshot.Mode}); err != nil {
		return nil, err
	}
	return out, tx.Commit()
}

func activeReplacementStaleTx(ctx context.Context, tx *sql.Tx, project, switchID, mode string) (bool, error) {
	var stale bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM ztp_switch_project_bindings pb JOIN ztp_replacements r ON r.id=pb.replacement_id JOIN switches s ON s.id=pb.switch_id JOIN bom_items b ON b.item_uid=s.id LEFT JOIN switch_mac_bindings m ON m.switch_id=s.id AND m.kind='mgmt' WHERE pb.project_id=$1 AND pb.switch_id=$2 AND pb.mode=$3 AND pb.state='active' AND (r.state<>'approved' OR (r.snapshot#>>'{new,serial}') IS DISTINCT FROM s.serial_number OR (r.snapshot#>>'{new,mgmt_mac}') IS DISTINCT FROM m.mac OR (r.snapshot#>'{new,position}') IS DISTINCT FROM b.position OR b.status='retired'))`, project, switchID, mode).Scan(&stale)
	return stale, err
}

func (s *Store) ApproveZTPReplacement(ctx context.Context, a *AuthSession, p *ProjectSummary, id string, in ZTPReplacementApproval, trace string) (*ZTPReplacement, error) {
	if !bootMediaID.MatchString(id) || !in.Confirmed || in.ExpectedVersion < 1 || len(in.SnapshotSHA256) != 64 {
		return nil, apiError(400, "ZTP_RMA_CONFIRM_REQUIRED", "Confirm exact replacement version and snapshot")
	}
	tx, err := s.beginControl(ctx, a)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = requireProjectAdmin(ctx, tx, a, p.ID); err != nil {
		return nil, err
	}
	if err = lockKey(ctx, tx, "ztp.rma:"+p.ID); err != nil {
		return nil, err
	}
	out, err := scanZTPReplacement(tx.QueryRowContext(ctx, "SELECT "+ztpReplacementFields+" FROM ztp_replacements WHERE id=$1 AND project_id=$2 FOR UPDATE", id, p.ID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if out.CreatedBy == a.User.ID {
		return nil, apiError(409, "ZTP_RMA_INDEPENDENT_APPROVAL_REQUIRED", "A different project administrator must approve")
	}
	if out.SnapshotSHA256 != in.SnapshotSHA256 {
		return nil, apiError(409, "ZTP_RMA_SNAPSHOT_CHANGED", "Replacement snapshot changed")
	}
	if out.State == "approved" && out.ApprovedBy == a.User.ID && in.ExpectedVersion == out.StateVersion-1 {
		return out, tx.Commit()
	}
	if out.State != "requested" || out.StateVersion != in.ExpectedVersion {
		return nil, apiError(409, "ZTP_RMA_STATE_CONFLICT", "Replacement state changed")
	}
	var active bool
	if err = tx.QueryRowContext(ctx, `SELECT u.enabled AND (u.is_admin OR EXISTS(SELECT 1 FROM memberships m WHERE m.user_id=u.id AND m.project_id=$2 AND m.role IN('Admin','NDE'))) FROM users u WHERE u.id=$1`, out.CreatedBy, p.ID).Scan(&active); err != nil {
		return nil, err
	}
	if !active {
		return nil, apiError(409, "ZTP_RMA_REQUESTER_REVOKED", "Original requester no longer has planning authority")
	}
	current, err := replacementSnapshotTx(ctx, tx, p.ID, ZTPReplacementInput{FromSessionID: out.Snapshot.FromSessionID, FromExpectedVersion: out.Snapshot.FromSessionVersion, ToSwitchID: out.Snapshot.New.SwitchID})
	if err != nil {
		return nil, err
	}
	if fabricboot.ManagementPlanDigest(current) != out.SnapshotSHA256 {
		return nil, apiError(409, "ZTP_RMA_SNAPSHOT_CHANGED", "Recheck old/new SN, MAC and location after BOM changes")
	}
	now := time.Now().UTC()
	for _, binding := range []struct{ id, state string }{{current.Old.SwitchID, "retired"}, {current.New.SwitchID, "active"}} {
		_, err = tx.ExecContext(ctx, `INSERT INTO ztp_switch_project_bindings(project_id,switch_id,mode,state,replacement_id,updated_at) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(project_id,switch_id,mode) DO UPDATE SET state=EXCLUDED.state,replacement_id=EXCLUDED.replacement_id,updated_at=EXCLUDED.updated_at`, p.ID, binding.id, current.Mode, binding.state, out.ID, now)
		if err != nil {
			return nil, err
		}
	}
	if current.Mode == "device" {
		// Retire only the mutable address projection, retaining its frozen plan.
		if _, err = tx.ExecContext(ctx, `DELETE FROM ztp_management_assignments WHERE project_id=$1 AND switch_id=$2`, p.ID, current.Old.SwitchID); err != nil {
			return nil, err
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE ztp_replacements SET state='approved',state_version=state_version+1,approved_by=$2,approved_at=$3 WHERE id=$1`, id, a.User.ID, now)
	if err != nil {
		return nil, err
	}
	if err = appendAudit(ctx, tx, a.User.ID, p.ID, "ztp.replacement.approve", id, trace, map[string]any{"snapshot_sha256": out.SnapshotSHA256, "mode": current.Mode, "old_binding": "retired", "new_binding": "active", "device_configuration_changed": false}); err != nil {
		return nil, err
	}
	out.State = "approved"
	out.StateVersion++
	out.ApprovedBy = a.User.ID
	out.ApprovedAt = &now
	return out, tx.Commit()
}
func (s *Store) ListZTPReplacements(ctx context.Context, a *AuthSession, p *ProjectSummary) ([]ZTPReplacement, error) {
	tx, err := s.beginControlRead(ctx, a)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = requireProjectMember(ctx, tx, a, p.ID); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT "+ztpReplacementFields+" FROM ztp_replacements WHERE project_id=$1 ORDER BY created_at DESC,id DESC LIMIT 100", p.ID)
	if err != nil {
		return nil, err
	}
	out := []ZTPReplacement{}
	for rows.Next() {
		v, e := scanZTPReplacement(rows)
		if e != nil {
			rows.Close()
			return nil, e
		}
		out = append(out, *v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	return out, tx.Commit()
}

// Rejection never reverses an approved retirement or changes a device binding.
func (s *Store) RejectZTPReplacement(ctx context.Context, a *AuthSession, p *ProjectSummary, id string, in ZTPReplacementApproval, trace string) (*ZTPReplacement, error) {
	if !bootMediaID.MatchString(id) || !in.Confirmed || in.ExpectedVersion < 1 || len(in.SnapshotSHA256) != 64 {
		return nil, apiError(400, "ZTP_RMA_CONFIRM_REQUIRED", "Confirm exact replacement version and snapshot")
	}
	tx, err := s.beginControl(ctx, a)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = requireProjectAdmin(ctx, tx, a, p.ID); err != nil {
		return nil, err
	}
	if err = lockKey(ctx, tx, "ztp.rma:"+p.ID); err != nil {
		return nil, err
	}
	out, err := scanZTPReplacement(tx.QueryRowContext(ctx, "SELECT "+ztpReplacementFields+" FROM ztp_replacements WHERE project_id=$1 AND id=$2 FOR UPDATE", p.ID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if out.SnapshotSHA256 != in.SnapshotSHA256 {
		return nil, apiError(409, "ZTP_RMA_SNAPSHOT_CHANGED", "Replacement snapshot changed")
	}
	if out.State == "rejected" && in.ExpectedVersion == out.StateVersion-1 {
		return out, tx.Commit()
	}
	if out.State != "requested" || out.StateVersion != in.ExpectedVersion {
		return nil, apiError(409, "ZTP_RMA_STATE_CONFLICT", "Only an exact pending request may be rejected")
	}
	if _, err = tx.ExecContext(ctx, `UPDATE ztp_replacements SET state='rejected',state_version=state_version+1 WHERE id=$1`, id); err != nil {
		return nil, err
	}
	if err = appendAudit(ctx, tx, a.User.ID, p.ID, "ztp.replacement.reject", id, trace, map[string]any{"snapshot_sha256": out.SnapshotSHA256, "binding_changed": false}); err != nil {
		return nil, err
	}
	out.State = "rejected"
	out.StateVersion++
	return out, tx.Commit()
}
