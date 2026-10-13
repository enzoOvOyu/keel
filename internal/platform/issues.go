package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Issue lifecycle (WP-2.7, FR-4.7). One board unifies the difference
// sources; each issue carries source/phase dual markers and (when created
// from a G2 difference) references the difference as its evidence. The
// state machine is strict: open -> assigned -> in_repair -> verify ->
// closed, reopened allowed from closed. Every transition demands authority,
// the expected resource version (optimistic lock — a stale version answers
// the current state instead of applying), and the full event history is
// kept in issue_events.

var repairActions = map[string]bool{
	"reseat": true, "replace_cable": true, "replace_module": true, "replace_nic": true,
	"rma_machine": true, "switch_side": true, "config_fix": true, "bios_update": true, "board_rma": true,
}

type Issue struct {
	ID            string     `json:"id"`
	MachineID     string     `json:"machine_id"`
	Hostname      string     `json:"hostname"`
	LocalPort     string     `json:"local_port,omitempty"`
	DifferenceID  string     `json:"difference_id,omitempty"`
	Source        string     `json:"source"`
	Phase         string     `json:"phase"`
	Category      string     `json:"category"`
	Title         string     `json:"title"`
	Evidence      string     `json:"evidence"`
	RepairAction  string     `json:"repair_action,omitempty"`
	RepairNote    string     `json:"repair_note,omitempty"`
	Status        string     `json:"status"`
	Assignee      string     `json:"assignee,omitempty"`
	AssigneeName  string     `json:"assignee_name,omitempty"`
	Version       int        `json:"version"`
	CreatedBy     string     `json:"created_by"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type CreateIssueInput struct {
	DifferenceID string `json:"difference_id"`
	Source       string `json:"source"`
	Phase        string `json:"phase"`
}

type RepairIssueInput struct {
	IssueID  string `json:"issue_id"`
	Action   string `json:"action"`
	Note     string `json:"note"`
	OldTag   string `json:"old_tag"`
	NewTag   string `json:"new_tag"`
}

type TransitionIssueInput struct {
	IssueID string `json:"issue_id"`
	Version int    `json:"version"`
	Note    string `json:"note"`
}

// CreateIssueFromDifference materializes an issue for an unresolved G2
// difference, carrying the difference as the evidence package.
func (s *Store) CreateIssueFromDifference(ctx context.Context, a *AuthSession, project *ProjectSummary, input CreateIssueInput, trace string) (*Issue, error) {
	if input.DifferenceID == "" {
		return nil, apiError(400, "VALIDATION_ERROR", "difference_id is required")
	}
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e := requireProjectAdmin(ctx, tx, a, project.ID); e != nil {
		return nil, e
	}
	var machineID, localPort, category string
	var resolvedAt sql.NullTime
	var detail string
	if e = tx.QueryRowContext(ctx, `SELECT d.machine_id,d.local_port,d.category,d.detail,d.resolved_at
		FROM lldp_differences d WHERE d.id=$1 AND d.project_id=$2`, input.DifferenceID, project.ID).Scan(&machineID, &localPort, &category, &detail, &resolvedAt); e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return nil, apiError(409, "VALIDATION_ERROR", "difference not found in this project")
		}
		return nil, e
	}
	if resolvedAt.Valid {
		return nil, apiError(409, "VALIDATION_ERROR", "difference already resolved; no issue needed")
	}
	var hostname string
	if e = tx.QueryRowContext(ctx, "SELECT hostname FROM project_assets WHERE project_id=$1 AND machine_id=$2", project.ID, machineID).Scan(&hostname); e != nil {
		return nil, e
	}
	now := time.Now().UTC()
	issue := Issue{
		ID: RandomID(), MachineID: machineID, Hostname: hostname, LocalPort: localPort,
		DifferenceID: input.DifferenceID, Source: "g2", Phase: "delivery", Category: category,
		Title: fmt.Sprintf("%s %s port %s: %s", hostname, category, localPort, categoryTitle(category)),
		Evidence: detail, Status: "open", Version: 1, CreatedBy: a.User.ID, CreatedAt: now, UpdatedAt: now,
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO issues(id,project_id,machine_id,difference_id,source,phase,category,title,evidence,status,version,created_by,created_at,updated_at)
		VALUES($1,$2,$3,$4,'g2',$5,$6,$7,$8,'open',1,$9,$10,$10)`,
		issue.ID, project.ID, machineID, input.DifferenceID, input.Phase, issue.Category, issue.Title, issue.Evidence, a.User.ID, now); e != nil {
		return nil, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO issue_events(id,issue_id,actor,action,from_status,to_status,note,created_at)
		VALUES($1,$2,$3,'create','','open','',$4)`,
		RandomID(), issue.ID, a.User.ID, now); e != nil {
		return nil, e
	}
	if e = appendAudit(ctx, tx, a.User.ID, project.ID, "issue.create", issue.ID, trace, map[string]any{
		"difference": input.DifferenceID, "category": issue.Category,
	}); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return &issue, nil
}

func categoryTitle(category string) string {
	switch category {
	case "T1":
		return "线路不通"
	case "T2":
		return "布线纠错"
	case "T3":
		return "模块更换"
	case "T4":
		return "线缆更换"
	case "T5":
		return "网卡 RMA"
	}
	return category
}

// RepairIssue registers the NE repair action with physical verification
// (old/new tags). Registering a repair moves assigned issues forward; the
// optimistic version lock keeps concurrent edits honest.
func (s *Store) RepairIssue(ctx context.Context, a *AuthSession, project *ProjectSummary, input RepairIssueInput, trace string) (*Issue, error) {
	if !repairActions[input.Action] {
		return nil, apiError(400, "VALIDATION_ERROR", "action must be a known repair action")
	}
	if len(input.Note) < 4 || len(input.Note) > 4000 {
		return nil, apiError(400, "VALIDATION_ERROR", "a meaningful repair note is required")
	}
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e := requireProjectAdmin(ctx, tx, a, project.ID); e != nil {
		return nil, e
	}
	issue, e := loadIssue(ctx, tx, project.ID, input.IssueID)
	if e != nil {
		return nil, e
	}
	if issue.Status != "in_repair" && issue.Status != "assigned" {
		return nil, apiError(409, "ISSUE_STATE", "repairs register on assigned or in_repair issues")
	}
	now := time.Now().UTC()
	proof, _ := json.Marshal(map[string]any{"old_tag": input.OldTag, "new_tag": input.NewTag})
	if _, e = tx.ExecContext(ctx, `UPDATE issues SET repair_action=$2, repair_note=$3, repair_evidence=$4, version=version+1, updated_at=$5 WHERE id=$1`,
		issue.ID, input.Action, input.Note, string(proof), now); e != nil {
		return nil, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO issue_events(id,issue_id,actor,action,from_status,to_status,note,created_at)
		VALUES($1,$2,$3,'repair',$4,$4,$5,$6)`,
		RandomID(), issue.ID, a.User.ID, issue.Status, input.Note, now); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	issue.RepairAction, issue.RepairNote = input.Action, input.Note
	issue.Version++
	issue.UpdatedAt = now
	return issue, nil
}

// TransitionIssue advances the lifecycle one step at a time. A stale version
// answers the current state (409 ISSUE_VERSION) instead of applying; the
// closed -> open path is the reopen exception and verify -> closed requires
// the underlying difference to have been resolved by a passing re-run.
func (s *Store) TransitionIssue(ctx context.Context, a *AuthSession, project *ProjectSummary, input TransitionIssueInput, action string, trace string) (*Issue, error) {
	switch action {
	case "assign", "start", "verify", "close", "reopen":
	default:
		return nil, apiError(400, "VALIDATION_ERROR", "unknown transition")
	}
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e := requireProjectAdmin(ctx, tx, a, project.ID); e != nil {
		return nil, e
	}
	issue, e := loadIssue(ctx, tx, project.ID, input.IssueID)
	if e != nil {
		return nil, e
	}
	if input.Version != issue.Version {
		return nil, apiError(409, "ISSUE_VERSION", fmt.Sprintf("version mismatch: current version is %d", issue.Version))
	}
	// Strict action-to-status pairing: each action is legal from exactly one
	// status, so a mislabelled action can never sneak a state forward.
	var next string
	switch {
	case issue.Status == "open" && action == "assign":
		next = "assigned"
	case issue.Status == "assigned" && action == "start":
		next = "in_repair"
	case issue.Status == "in_repair" && action == "verify":
		if issue.RepairAction == "" {
			return nil, apiError(409, "ISSUE_STATE", "register a repair action before requesting verification")
		}
		next = "verify"
	case issue.Status == "verify" && action == "close":
		// Closing requires the evidence difference to be resolved.
		var resolvedAt sql.NullTime
		if e = tx.QueryRowContext(ctx, "SELECT resolved_at FROM lldp_differences WHERE id=$1", issue.DifferenceID).Scan(&resolvedAt); e != nil && !errors.Is(e, sql.ErrNoRows) {
			return nil, e
		}
		if !resolvedAt.Valid {
			return nil, apiError(409, "ISSUE_STATE", "the difference is not resolved yet; run the targeted re-verification first")
		}
		next = "closed"
	case issue.Status == "closed" && action == "reopen":
		next = "open"
	default:
		return nil, apiError(409, "ISSUE_STATE", fmt.Sprintf("cannot %s from %s", action, issue.Status))
	}
	now := time.Now().UTC()
	if _, e = tx.ExecContext(ctx, `UPDATE issues SET status=$2, assignee=COALESCE($3,assignee), version=version+1, updated_at=$4 WHERE id=$1`,
		issue.ID, next, assigneeFor(action, a.User.ID), now); e != nil {
		return nil, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO issue_events(id,issue_id,actor,action,from_status,to_status,note,created_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8)`,
		RandomID(), issue.ID, a.User.ID, action, issue.Status, next, input.Note, now); e != nil {
		return nil, e
	}
	if e = appendAudit(ctx, tx, a.User.ID, project.ID, "issue."+action, issue.ID, trace, map[string]any{
		"from": issue.Status, "to": next,
	}); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	issue.Status = next
	if action == "assign" {
		issue.Assignee = a.User.ID
	}
	issue.Version++
	issue.UpdatedAt = now
	return issue, nil
}

func assigneeFor(action, userID string) any {
	if action == "assign" {
		return userID
	}
	return nil
}

func loadIssue(ctx context.Context, tx *sql.Tx, projectID, issueID string) (*Issue, error) {
	var issue Issue
	var differenceID, assignee sql.NullString
	e := tx.QueryRowContext(ctx, `SELECT i.id,i.machine_id,pa.hostname,i.difference_id,i.source,i.phase,i.category,i.title,i.evidence,
			i.repair_action,i.repair_note,i.status,i.assignee,i.version,i.created_by,i.created_at,i.updated_at
		FROM issues i JOIN project_assets pa ON pa.machine_id=i.machine_id AND pa.project_id=i.project_id
		WHERE i.id=$1 AND i.project_id=$2`, issueID, projectID).Scan(
		&issue.ID, &issue.MachineID, &issue.Hostname, &differenceID, &issue.Source, &issue.Phase, &issue.Category, &issue.Title, &issue.Evidence,
		&issue.RepairAction, &issue.RepairNote, &issue.Status, &assignee, &issue.Version, &issue.CreatedBy, &issue.CreatedAt, &issue.UpdatedAt)
	if e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return nil, apiError(404, "NOT_FOUND", "issue not found")
		}
		return nil, e
	}
	issue.DifferenceID, issue.Assignee = differenceID.String, assignee.String
	return &issue, nil
}

// ListIssues filters the board by status/source/phase.
func (s *Store) ListIssues(ctx context.Context, a *AuthSession, project *ProjectSummary, status, source, phase string, p Pagination) (Page[Issue], error) {
	out := NewPage[Issue](p)
	if e := p.Validate(); e != nil {
		return out, e
	}
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	if e := requireProjectMember(ctx, tx, a, project.ID); e != nil {
		return out, e
	}
	filters := ""
	filterArgs := []any{}
	if status != "" {
		filterArgs = append(filterArgs, status)
		filters += fmt.Sprintf(" AND i.status=$%d", len(filterArgs)+1)
	}
	if source != "" {
		filterArgs = append(filterArgs, source)
		filters += fmt.Sprintf(" AND i.source=$%d", len(filterArgs)+1)
	}
	if phase != "" {
		filterArgs = append(filterArgs, phase)
		filters += fmt.Sprintf(" AND i.phase=$%d", len(filterArgs)+1)
	}
	where := "i.project_id=$1" + filters
	countArgs := append([]any{project.ID}, filterArgs...)
	if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM issues i WHERE "+where, countArgs...).Scan(&out.Total); e != nil {
		return out, e
	}
	args := append(countArgs, p.PageSize, p.Offset())
	rows, e := tx.QueryContext(ctx, `SELECT i.id,i.machine_id,pa.hostname,i.difference_id,i.source,i.phase,i.category,i.title,i.evidence,
			i.repair_action,i.repair_note,i.status,i.assignee,i.version,i.created_by,i.created_at,i.updated_at
		FROM issues i JOIN project_assets pa ON pa.machine_id=i.machine_id AND pa.project_id=i.project_id
		WHERE `+where+` ORDER BY i.created_at DESC, i.id LIMIT $`+fmt.Sprint(len(args)-1)+` OFFSET $`+fmt.Sprint(len(args)), args...)
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var issue Issue
		var differenceID, assignee sql.NullString
		if e = rows.Scan(&issue.ID, &issue.MachineID, &issue.Hostname, &differenceID, &issue.Source, &issue.Phase, &issue.Category, &issue.Title, &issue.Evidence,
			&issue.RepairAction, &issue.RepairNote, &issue.Status, &assignee, &issue.Version, &issue.CreatedBy, &issue.CreatedAt, &issue.UpdatedAt); e != nil {
			return out, e
		}
		issue.DifferenceID, issue.Assignee = differenceID.String, assignee.String
		out.Items = append(out.Items, issue)
	}
	if e = rows.Err(); e != nil {
		return out, e
	}
	if out.Items == nil {
		out.Items = []Issue{}
	}
	return out, nil
}
