package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

type TopologyRetestInput struct {
	Version    int    `json:"version"`
	SnapshotID string `json:"snapshot_id"`
	Reason     string `json:"reason"`
}
type TopologyRetest struct {
	ID                  string    `json:"id"`
	IssueID             string    `json:"issue_id"`
	ProjectID           string    `json:"project_id"`
	AssetID             string    `json:"asset_id"`
	SnapshotID          string    `json:"snapshot_id"`
	ExecutionID         string    `json:"execution_id"`
	RepairVersion       int       `json:"repair_version"`
	AppliedIssueVersion int       `json:"applied_issue_version"`
	ScopeHash           string    `json:"scope_hash"`
	Verdict             string    `json:"verdict"`
	Reason              string    `json:"reason"`
	CreatedBy           string    `json:"created_by"`
	CreatedAt           time.Time `json:"created_at"`
	Stale               bool      `json:"stale"`
	CanClose            bool      `json:"can_close"`
	Escalated           bool      `json:"escalated"`
	RecipientIDs        []string  `json:"recipient_ids"`
}

type TopologyRetestScope struct {
	IssueID         string     `json:"issue_id"`
	ProjectID       string     `json:"project_id"`
	AssetID         string     `json:"asset_id"`
	IssueVersion    int        `json:"issue_version"`
	RepairVersion   int        `json:"repair_version"`
	RepairedAt      *time.Time `json:"repaired_at"`
	SnapshotID      string     `json:"snapshot_id"`
	SnapshotVerdict string     `json:"snapshot_verdict"`
	Ready           bool       `json:"ready"`
	Blockers        []string   `json:"blockers"`
}

func (s *Store) PreviewTopologyRetest(ctx context.Context, a *AuthSession, issue string) (*TopologyRetestScope, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	item, e := scanIssue(tx.QueryRowContext(ctx, `SELECT `+issueColumns+` FROM issues WHERE id=$1`, issue))
	if errors.Is(e, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if e != nil {
		return nil, e
	}
	role, e := issueRoleTx(ctx, tx, a, item.ProjectID)
	if e != nil {
		return nil, e
	}
	out := &TopologyRetestScope{IssueID: issue, ProjectID: item.ProjectID, IssueVersion: item.Version, Ready: true, Blockers: []string{}}
	block := func(code string) { out.Ready = false; out.Blockers = append(out.Blockers, code) }
	if !canConfirmIssueVerification(item, role, a.User.ID) {
		block("TOPOLOGY_RETEST_AUTHORITY_REQUIRED")
	}
	if item.Source != "topology" || item.Category != "T7" {
		block("TOPOLOGY_ISSUE_REQUIRED")
		return out, nil
	}
	if item.State != "in_repair" {
		block("TOPOLOGY_REPAIR_REQUIRED")
	}
	var projectState string
	if e = tx.QueryRowContext(ctx, `SELECT state FROM projects WHERE id=$1`, item.ProjectID).Scan(&projectState); e != nil {
		return nil, e
	}
	if projectState == "archived" || projectState == "delivered" {
		block("PROJECT_READ_ONLY")
	}
	var origin string
	e = tx.QueryRowContext(ctx, `SELECT asset_id,origin FROM topology_issue_bindings WHERE issue_id=$1`, issue).Scan(&out.AssetID, &origin)
	if errors.Is(e, sql.ErrNoRows) {
		block("TOPOLOGY_DISCOVERY_BINDING_REQUIRED")
		return out, nil
	}
	if e != nil {
		return nil, e
	}
	var repaired time.Time
	e = tx.QueryRowContext(ctx, `SELECT version,created_at FROM issue_events WHERE issue_id=$1 AND action='repair' ORDER BY version DESC LIMIT 1`, issue).Scan(&out.RepairVersion, &repaired)
	if errors.Is(e, sql.ErrNoRows) {
		block("TOPOLOGY_REPAIR_REQUIRED")
		return out, nil
	}
	if e != nil {
		return nil, e
	}
	out.RepairedAt = &repaired
	var used bool
	if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM topology_issue_retests WHERE issue_id=$1 AND repair_version=$2)`, issue, out.RepairVersion).Scan(&used); e != nil {
		return nil, e
	}
	if used {
		block("TOPOLOGY_RETEST_REPAIR_ALREADY_USED")
	}
	e = tx.QueryRowContext(ctx, `SELECT id FROM pci_topology_snapshots WHERE project_id=$1 AND asset_id=$2 ORDER BY version DESC LIMIT 1`, item.ProjectID, out.AssetID).Scan(&out.SnapshotID)
	if errors.Is(e, sql.ErrNoRows) {
		block("TOPOLOGY_CAPTURE_REQUIRED")
		return out, nil
	}
	if e != nil {
		return nil, e
	}
	snapshot, e := s.topologyDetailTx(ctx, tx, item.ProjectID, out.AssetID, out.SnapshotID)
	if e != nil {
		return nil, e
	}
	out.SnapshotVerdict = snapshot.Verdict
	if snapshot.Validity != "fresh" || snapshot.Origin != origin {
		block("TOPOLOGY_RETEST_NOT_CURRENT")
	}
	var requestedAt time.Time
	if e = tx.QueryRowContext(ctx, `SELECT created_at FROM validation_precollect_requests WHERE id=$1`, snapshot.PrecollectID).Scan(&requestedAt); e != nil {
		return nil, e
	}
	if !requestedAt.After(repaired) || !snapshot.CreatedAt.After(repaired) {
		block("TOPOLOGY_POST_REPAIR_CAPTURE_REQUIRED")
	}
	return out, nil
}

const topologyRetestFields = `r.id,r.issue_id,r.project_id,b.asset_id,r.snapshot_id,r.execution_id,r.repair_version,r.applied_issue_version,r.scope_hash,r.verdict,r.reason,r.created_by,r.created_at`

func scanTopologyRetest(row scanner) (*TopologyRetest, error) {
	var out TopologyRetest
	e := row.Scan(&out.ID, &out.IssueID, &out.ProjectID, &out.AssetID, &out.SnapshotID, &out.ExecutionID, &out.RepairVersion, &out.AppliedIssueVersion, &out.ScopeHash, &out.Verdict, &out.Reason, &out.CreatedBy, &out.CreatedAt)
	out.RecipientIDs = []string{}
	return &out, e
}
func (s *Store) CreateTopologyRetest(ctx context.Context, a *AuthSession, issue string, in TopologyRetestInput, key, trace string) (*TopologyRetest, error) {
	in.Reason = strings.TrimSpace(in.Reason)
	if in.Version < 1 || !validEntityID(in.SnapshotID) || utf8.RuneCountInString(in.Reason) < 3 || utf8.RuneCountInString(in.Reason) > 1000 || strings.TrimSpace(key) == "" || len(key) > 128 {
		return nil, apiError(400, "VALIDATION_ERROR", "Current issue version, snapshot, reason and Idempotency-Key required")
	}
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	item, e := scanIssue(tx.QueryRowContext(ctx, `SELECT `+issueColumns+` FROM issues WHERE id=$1 FOR UPDATE`, issue))
	if errors.Is(e, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if e != nil {
		return nil, e
	}
	role, e := issueRoleTx(ctx, tx, a, item.ProjectID)
	if e != nil {
		return nil, e
	}
	if !canConfirmIssueVerification(item, role, a.User.ID) {
		return nil, ErrForbidden
	}
	if item.Source != "topology" || item.Category != "T7" {
		return nil, apiError(409, "TOPOLOGY_ISSUE_REQUIRED", "Trusted T7 topology issue required")
	}
	var projectState string
	if e = tx.QueryRowContext(ctx, `SELECT state FROM projects WHERE id=$1 FOR SHARE`, item.ProjectID).Scan(&projectState); e != nil {
		return nil, e
	}
	if projectState == "archived" || projectState == "delivered" {
		return nil, apiError(409, "PROJECT_READ_ONLY", "Project is read-only")
	}
	identity := digest("topology.retest:" + issue + ":" + a.User.ID + ":" + key)
	hash := digest(string(networkJSON(in)))
	if e = lockKey(ctx, tx, identity); e != nil {
		return nil, e
	}
	var oldHash string
	var prior []byte
	e = tx.QueryRowContext(ctx, `SELECT request_hash,response FROM idempotencies WHERE id=$1`, identity).Scan(&oldHash, &prior)
	if e == nil {
		if oldHash != hash {
			return nil, apiError(409, "IDEMPOTENCY_CONFLICT", "This key was used for another retest")
		}
		var ref struct {
			ID string `json:"id"`
		}
		if e = json.Unmarshal(prior, &ref); e != nil {
			return nil, e
		}
		out, e := s.topologyRetestDetailTx(ctx, tx, item, role, a.User.ID, ref.ID)
		if e != nil {
			return nil, e
		}
		return out, tx.Commit()
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return nil, e
	}
	if item.Version != in.Version {
		return nil, errVersion
	}
	if item.State != "in_repair" {
		return nil, apiError(409, "TOPOLOGY_REPAIR_REQUIRED", "Register a repair before submitting new topology evidence")
	}
	var asset, origin string
	e = tx.QueryRowContext(ctx, `SELECT asset_id,origin FROM topology_issue_bindings WHERE issue_id=$1 AND project_id=$2`, issue, item.ProjectID).Scan(&asset, &origin)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, apiError(409, "TOPOLOGY_DISCOVERY_BINDING_REQUIRED", "Trusted topology discovery binding required")
	}
	if e != nil {
		return nil, e
	}
	var repairVersion int
	var repairedAt time.Time
	e = tx.QueryRowContext(ctx, `SELECT version,created_at FROM issue_events WHERE issue_id=$1 AND action='repair' ORDER BY version DESC LIMIT 1`, issue).Scan(&repairVersion, &repairedAt)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, apiError(409, "TOPOLOGY_REPAIR_REQUIRED", "A recorded repair is required")
	}
	if e != nil {
		return nil, e
	}
	var duplicate bool
	if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM topology_issue_retests WHERE issue_id=$1 AND repair_version=$2)`, issue, repairVersion).Scan(&duplicate); e != nil {
		return nil, e
	}
	if duplicate {
		return nil, apiError(409, "TOPOLOGY_RETEST_REPAIR_ALREADY_USED", "This repair already has a retest; register a new repair before another attempt")
	}
	snapshot, e := s.topologyDetailTx(ctx, tx, item.ProjectID, asset, in.SnapshotID)
	if e != nil {
		return nil, e
	}
	if snapshot.Validity != "fresh" || snapshot.Origin != origin {
		return nil, apiError(409, "TOPOLOGY_RETEST_NOT_CURRENT", "Retest requires current evidence in the original asset and source domain")
	}
	var requestedAt time.Time
	var execution string
	if e = tx.QueryRowContext(ctx, `SELECT created_at,execution_id FROM validation_precollect_requests WHERE id=$1`, snapshot.PrecollectID).Scan(&requestedAt, &execution); e != nil {
		return nil, e
	}
	if !requestedAt.After(repairedAt) || !snapshot.CreatedAt.After(repairedAt) {
		return nil, apiError(409, "TOPOLOGY_POST_REPAIR_CAPTURE_REQUIRED", "Start a new precollection after the recorded repair, then validate its new G1 topology")
	}
	state := "in_repair"
	if snapshot.Verdict == "PASS" {
		state = "verify"
	}
	updated, e := scanIssue(tx.QueryRowContext(ctx, `UPDATE issues SET state=$2,version=version+1,updated_at=now() WHERE id=$1 AND version=$3 RETURNING `+issueColumns, item.ID, state, item.Version))
	if e != nil {
		return nil, e
	}
	id := RandomID()
	_, e = tx.ExecContext(ctx, `INSERT INTO topology_issue_retests(id,issue_id,project_id,snapshot_id,execution_id,repair_version,applied_issue_version,scope_hash,verdict,reason,created_by,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,now())`, id, issue, item.ProjectID, snapshot.ID, execution, repairVersion, updated.Version, snapshot.ScopeHash, snapshot.Verdict, in.Reason, a.User.ID)
	if e != nil {
		return nil, e
	}
	if e = issueEventTx(ctx, tx, a, updated, "topology_retest", trace, map[string]any{"retest_id": id, "snapshot_id": snapshot.ID, "execution_id": execution, "repair_version": repairVersion, "verdict": snapshot.Verdict, "reason": in.Reason}); e != nil {
		return nil, e
	}
	if snapshot.Verdict == "FAIL" {
		if e = topologyRetestEscalationTx(ctx, tx, a, id, issue, item.ProjectID, execution, trace); e != nil {
			return nil, e
		}
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO idempotencies(id,actor_id,request_hash,response,created_at) VALUES($1,$2,$3,$4,now())`, identity, a.User.ID, hash, networkJSON(map[string]string{"id": id})); e != nil {
		return nil, e
	}
	out, e := s.topologyRetestDetailTx(ctx, tx, updated, role, a.User.ID, id)
	if e != nil {
		return nil, e
	}
	return out, tx.Commit()
}

func (s *Store) topologyRetestDetailTx(ctx context.Context, tx *sql.Tx, item *Issue, role, actor, id string) (*TopologyRetest, error) {
	out, e := scanTopologyRetest(tx.QueryRowContext(ctx, `SELECT `+topologyRetestFields+` FROM topology_issue_retests r JOIN topology_issue_bindings b ON b.issue_id=r.issue_id WHERE r.id=$1 AND r.issue_id=$2`, id, item.ID))
	if e != nil {
		return nil, e
	}
	snapshot, e := s.topologyDetailTx(ctx, tx, item.ProjectID, out.AssetID, out.SnapshotID)
	if e != nil {
		return nil, e
	}
	out.Stale = snapshot.Validity != "fresh" || out.AppliedIssueVersion != item.Version
	var recipients []byte
	e = tx.QueryRowContext(ctx, `SELECT recipient_ids FROM topology_issue_escalations WHERE request_id=$1`, id).Scan(&recipients)
	if e == nil {
		out.Escalated = true
		if e = json.Unmarshal(recipients, &out.RecipientIDs); e != nil {
			return nil, e
		}
	} else if !errors.Is(e, sql.ErrNoRows) {
		return nil, e
	}
	if !out.Stale && out.Verdict == "PASS" && item.State == "verify" && canConfirmIssueVerification(item, role, actor) {
		_, e := s.topologyCloseEvidenceTx(ctx, tx, item)
		if e == nil {
			out.CanClose = true
		} else {
			var business *Error
			if !errors.As(e, &business) || business.Status != 409 {
				return nil, e
			}
		}
	}
	return out, nil
}

func (s *Store) ListTopologyRetests(ctx context.Context, a *AuthSession, issue string, page Pagination) (Page[TopologyRetest], error) {
	out := NewPage[TopologyRetest](page)
	if e := page.Validate(); e != nil {
		return out, e
	}
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	item, e := scanIssue(tx.QueryRowContext(ctx, `SELECT `+issueColumns+` FROM issues WHERE id=$1`, issue))
	if errors.Is(e, sql.ErrNoRows) {
		return out, ErrNotFound
	}
	if e != nil {
		return out, e
	}
	role, e := issueRoleTx(ctx, tx, a, item.ProjectID)
	if e != nil {
		return out, e
	}
	if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM topology_issue_retests WHERE issue_id=$1`, issue).Scan(&out.Total); e != nil {
		return out, e
	}
	rows, e := tx.QueryContext(ctx, `SELECT id FROM topology_issue_retests WHERE issue_id=$1 ORDER BY created_at DESC,id DESC LIMIT $2 OFFSET $3`, issue, page.PageSize, page.Offset())
	if e != nil {
		return out, e
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return out, e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	for _, id := range ids {
		v, e := s.topologyRetestDetailTx(ctx, tx, item, role, a.User.ID, id)
		if e != nil {
			return out, e
		}
		out.Items = append(out.Items, *v)
	}
	return out, nil
}

// Reuses the common explicit issue close action. A successful comparison alone
// does not close T7; current authority, scope and repair epoch are checked again.
func (s *Store) topologyCloseEvidenceTx(ctx context.Context, tx *sql.Tx, item *Issue) (*IssueVerificationRequest, error) {
	var id, asset, snapshotID, execution, hash, creator string
	var repairVersion int
	e := tx.QueryRowContext(ctx, `SELECT r.id,b.asset_id,r.snapshot_id,r.execution_id,r.scope_hash,r.created_by,r.repair_version FROM topology_issue_retests r JOIN topology_issue_bindings b ON b.issue_id=r.issue_id WHERE r.issue_id=$1 AND r.applied_issue_version=$2 AND r.verdict='PASS' ORDER BY r.created_at DESC,r.id DESC LIMIT 1`, item.ID, item.Version).Scan(&id, &asset, &snapshotID, &execution, &hash, &creator, &repairVersion)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, apiError(409, "ISSUE_REVALIDATION_REQUIRED", "Current post-repair topology PASS required")
	}
	if e != nil {
		return nil, e
	}
	var currentRepair int
	var authorized bool
	if e = tx.QueryRowContext(ctx, `SELECT coalesce((SELECT max(version) FROM issue_events WHERE issue_id=$1 AND action='repair'),0),EXISTS(SELECT 1 FROM users u WHERE u.id=$2 AND u.enabled AND (u.is_admin OR EXISTS(SELECT 1 FROM memberships m WHERE m.user_id=u.id AND m.project_id=$3 AND (m.role IN('Admin','DL') OR (m.role='NE' AND u.id=$4)))))`, item.ID, creator, item.ProjectID, item.AssigneeID).Scan(&currentRepair, &authorized); e != nil {
		return nil, e
	}
	if currentRepair != repairVersion || !authorized {
		return nil, apiError(409, "ISSUE_VERIFICATION_SCOPE_CHANGED", "Repair or verification authority changed")
	}
	snapshot, e := s.topologyDetailTx(ctx, tx, item.ProjectID, asset, snapshotID)
	if e != nil {
		return nil, e
	}
	if snapshot.Validity != "fresh" || snapshot.EffectiveVerdict != "PASS" || snapshot.Origin != "real" || snapshot.ScopeHash != hash {
		return nil, apiError(409, "ISSUE_VERIFICATION_SCOPE_CHANGED", "Current original G1/model/topology proof required")
	}
	return &IssueVerificationRequest{ID: id, IssueID: item.ID, ProjectID: item.ProjectID, ExecutionID: execution, ScopeHash: hash}, nil
}
