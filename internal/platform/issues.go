package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

type IssueAnchor struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}
type IssueEvidence struct {
	ObservedAt    time.Time `json:"observed_at"`
	SourceVersion string    `json:"source_version"`
	Expected      string    `json:"expected"`
	Actual        string    `json:"actual"`
	ExecutionID   string    `json:"execution_id,omitempty"`
}
type CreateIssueInput struct {
	Title    string        `json:"title"`
	Category string        `json:"category"`
	Source   string        `json:"source"`
	Phase    string        `json:"phase"`
	Anchors  []IssueAnchor `json:"anchors"`
	Evidence IssueEvidence `json:"evidence"`
}
type Issue struct {
	ID                  string        `json:"id"`
	ProjectID           string        `json:"project_id"`
	Title               string        `json:"title"`
	Category            string        `json:"category"`
	Source              string        `json:"source"`
	Phase               string        `json:"phase"`
	ResponsibleDomain   string        `json:"responsible_domain"`
	State               string        `json:"state"`
	Anchors             []IssueAnchor `json:"anchors"`
	Evidence            IssueEvidence `json:"evidence"`
	AssigneeID          string        `json:"assignee_id"`
	CreatedBy           string        `json:"created_by"`
	Version             int           `json:"version"`
	CreatedAt           time.Time     `json:"created_at"`
	UpdatedAt           time.Time     `json:"updated_at"`
	AllowedTransitions  []string      `json:"allowed_transitions"`
	VerificationBlocker string        `json:"verification_blocker"`
}
type IssueEvent struct {
	ID        string          `json:"id"`
	Version   int             `json:"version"`
	ActorID   string          `json:"actor_id"`
	Action    string          `json:"action"`
	Data      json.RawMessage `json:"data"`
	CreatedAt time.Time       `json:"created_at"`
}
type IssueDetail struct {
	Issue  Issue        `json:"issue"`
	Events []IssueEvent `json:"events"`
}
type IssueFilter struct{ ProjectID, State, Category, Source, Phase, ResponsibleDomain string }
type IssueActionInput struct {
	Version      int    `json:"version"`
	Reason       string `json:"reason"`
	AssigneeID   string `json:"assignee_id,omitempty"`
	RepairAction string `json:"repair_action,omitempty"`
	OldSerial    string `json:"old_serial,omitempty"`
	NewSerial    string `json:"new_serial,omitempty"`
}
type IssueAssignee struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Username    string `json:"username"`
}

func issueRoleTx(ctx context.Context, tx *sql.Tx, a *AuthSession, project string) (string, error) {
	var exists bool
	if e := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM projects WHERE id=$1)", project).Scan(&exists); e != nil {
		return "", e
	}
	if !exists {
		return "", ErrNotFound
	}
	if a.User.IsAdmin {
		return "Admin", nil
	}
	var role string
	e := tx.QueryRowContext(ctx, "SELECT role FROM memberships WHERE project_id=$1 AND user_id=$2", project, a.User.ID).Scan(&role)
	if errors.Is(e, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return role, e
}
func issuePermissions(role string) []string {
	switch role {
	case "Admin", "DL", "NE":
		return []string{"issue:read", "issue:create", "issue:assign", "issue:repair"}
	case "DE", "NDE", "NH", "SRE", "Viewer":
		return []string{"issue:read"}
	default:
		return []string{}
	}
}
func issueManager(role string) bool { return slices.Contains(issuePermissions(role), "issue:create") }
func issueTransitions(item *Issue, role, actor string) []string {
	out := []string{}
	if !issueManager(role) {
		return out
	}
	if slices.Contains([]string{"open", "assigned", "reopened"}, item.State) {
		out = append(out, "assign")
	}
	if role == "NE" && item.AssigneeID != actor {
		return out
	}
	if item.State == "assigned" {
		out = append(out, "start")
	}
	if item.State == "in_repair" {
		out = append(out, "repair")
	}
	return out
}
func validIssueCreate(in CreateIssueInput) error {
	if len(in.Title) == 0 || utf8.RuneCountInString(in.Title) > 240 || strings.TrimSpace(in.Title) == "" || !slices.Contains([]string{"T1", "T2", "T3", "T4", "T5", "T6", "T7"}, in.Category) || !slices.Contains([]string{"topology", "g2", "fabric", "g5", "g6", "portmon", "bmc"}, in.Source) || !slices.Contains([]string{"delivery", "post-delivery"}, in.Phase) || in.Phase == "post-delivery" && in.Source != "bmc" {
		return apiError(400, "VALIDATION_ERROR", "Invalid issue classification or title")
	}
	if len(in.Anchors) < 1 || len(in.Anchors) > 32 {
		return apiError(400, "VALIDATION_ERROR", "At least one stable object is required (maximum32)")
	}
	seen := map[string]bool{}
	for _, v := range in.Anchors {
		key := v.Kind + ":" + v.ID
		if !slices.Contains([]string{"machine", "component", "port", "switch", "cable"}, v.Kind) || !validEntityID(v.ID) || seen[key] {
			return apiError(400, "VALIDATION_ERROR", "Invalid or duplicate stable object")
		}
		seen[key] = true
	}
	ev := in.Evidence
	if ev.ObservedAt.IsZero() || ev.ObservedAt.After(time.Now().Add(time.Minute)) || len(strings.TrimSpace(ev.SourceVersion)) == 0 || utf8.RuneCountInString(ev.SourceVersion) > 240 || len(strings.TrimSpace(ev.Actual)) == 0 || utf8.RuneCountInString(ev.Actual) > 4000 || utf8.RuneCountInString(ev.Expected) > 4000 || ev.ExecutionID != "" && !validEntityID(ev.ExecutionID) {
		return apiError(400, "VALIDATION_ERROR", "Observation time, source version and actual evidence are required")
	}
	return nil
}
func validEntityID(id string) bool {
	if len(id) != 32 {
		return false
	}
	for _, c := range id {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

const issueColumns = `id,project_id,title,category,source,phase,responsible_domain,state,anchors,evidence,coalesce(assignee_id,''),created_by,version,created_at,updated_at`

func scanIssue(row scanner) (*Issue, error) {
	v := &Issue{AllowedTransitions: []string{}, VerificationBlocker: "TARGETED_REVALIDATION_REQUIRED"}
	var anchors, evidence []byte
	e := row.Scan(&v.ID, &v.ProjectID, &v.Title, &v.Category, &v.Source, &v.Phase, &v.ResponsibleDomain, &v.State, &anchors, &evidence, &v.AssigneeID, &v.CreatedBy, &v.Version, &v.CreatedAt, &v.UpdatedAt)
	if e != nil {
		return nil, e
	}
	if e = json.Unmarshal(anchors, &v.Anchors); e != nil {
		return nil, e
	}
	if e = json.Unmarshal(evidence, &v.Evidence); e != nil {
		return nil, e
	}
	return v, nil
}

// Resolve project membership from current stable identities, never from a label,
// OS interface name, caller-supplied mapping or another project's discovery.
func issueAnchorTx(ctx context.Context, tx *sql.Tx, project string, v IssueAnchor) error {
	var ok bool
	var q string
	switch v.Kind {
	case "machine":
		q = `SELECT EXISTS(SELECT 1 FROM project_assets WHERE project_id=$1 AND machine_id=$2)`
	case "port":
		q = `SELECT EXISTS(SELECT 1 FROM physical_ports p WHERE p.id=$2 AND (EXISTS(SELECT 1 FROM project_assets a WHERE a.project_id=$1 AND a.machine_id=p.machine_id) OR EXISTS(SELECT 1 FROM bom_item_revisions r JOIN bom_import_packages b ON b.id=r.package_id WHERE r.project_id=$1 AND r.item_uid=p.id AND b.state='committed')))`
	case "component", "switch", "cable":
		q = `SELECT EXISTS(SELECT 1 FROM bom_items b JOIN bom_item_revisions r ON r.item_uid=b.item_uid JOIN bom_import_packages p ON p.id=r.package_id WHERE r.project_id=$1 AND b.item_uid=$2 AND p.state='committed' AND `
		if v.Kind == "component" {
			q += `b.category IN('nic','gpu','psu','bmc','optic_module'))`
		} else {
			q += `b.category='` + v.Kind + `')`
		}
	}
	if e := tx.QueryRowContext(ctx, q, project, v.ID).Scan(&ok); e != nil {
		return e
	}
	if !ok {
		return ErrNotFound
	}
	return nil
}
func issueEventTx(ctx context.Context, tx *sql.Tx, a *AuthSession, item *Issue, action, trace string, data any) error {
	raw, e := json.Marshal(data)
	if e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO issue_events(id,issue_id,version,actor_id,action,data,created_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, RandomID(), item.ID, item.Version, a.User.ID, action, raw, item.UpdatedAt); e != nil {
		return e
	}
	return appendAudit(ctx, tx, a.User.ID, item.ProjectID, "issue."+action, item.ID, trace, map[string]any{"version": item.Version, "source": item.Source, "phase": item.Phase})
}
func insertIssueTx(ctx context.Context, tx *sql.Tx, a *AuthSession, project string, in CreateIssueInput, origin, trace string, refs map[string]any) (*Issue, error) {
	anchors, e := json.Marshal(in.Anchors)
	if e != nil {
		return nil, e
	}
	evidence, e := json.Marshal(in.Evidence)
	if e != nil {
		return nil, e
	}
	now := time.Now().UTC()
	v, e := scanIssue(tx.QueryRowContext(ctx, `INSERT INTO issues(id,project_id,title,category,source,phase,anchors,evidence,created_by,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$10) RETURNING `+issueColumns, RandomID(), project, in.Title, in.Category, in.Source, in.Phase, anchors, evidence, a.User.ID, now))
	if e != nil {
		return nil, e
	}
	data := map[string]any{"origin": origin, "evidence": in.Evidence}
	for k, v := range refs {
		data[k] = v
	}
	if e = issueEventTx(ctx, tx, a, v, "create", trace, data); e != nil {
		return nil, e
	}
	return v, nil
}

func (s *Store) CreateIssue(ctx context.Context, a *AuthSession, project string, in CreateIssueInput, key, trace string) (*Issue, error) {
	if e := validIssueCreate(in); e != nil {
		return nil, e
	}
	if len(strings.TrimSpace(key)) == 0 || len(key) > 128 {
		return nil, apiError(400, "VALIDATION_ERROR", "Idempotency-Key is required")
	}
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	role, e := issueRoleTx(ctx, tx, a, project)
	if e != nil {
		return nil, e
	}
	if !issueManager(role) {
		return nil, ErrForbidden
	}
	raw, e := json.Marshal(in)
	if e != nil {
		return nil, e
	}
	hash := digest(string(raw))
	identity := digest("issue.create:" + project + ":" + a.User.ID + ":" + key)
	if e = lockKey(ctx, tx, identity); e != nil {
		return nil, e
	}
	var oldHash string
	var prior []byte
	e = tx.QueryRowContext(ctx, `SELECT request_hash,response FROM idempotencies WHERE id=$1`, identity).Scan(&oldHash, &prior)
	if e == nil {
		if oldHash != hash {
			return nil, apiError(409, "IDEMPOTENCY_CONFLICT", "This key was used for another issue")
		}
		var saved struct {
			ID string `json:"id"`
		}
		if e = json.Unmarshal(prior, &saved); e != nil {
			return nil, e
		}
		v, e := scanIssue(tx.QueryRowContext(ctx, "SELECT "+issueColumns+" FROM issues WHERE id=$1 AND project_id=$2", saved.ID, project))
		if e != nil {
			return nil, e
		}
		v.AllowedTransitions = issueTransitions(v, role, a.User.ID)
		return v, tx.Commit()
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return nil, e
	}
	for _, v := range in.Anchors {
		if e = issueAnchorTx(ctx, tx, project, v); e != nil {
			return nil, e
		}
	}
	if in.Evidence.ExecutionID != "" {
		var ok bool
		if e = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM exec_executions WHERE id=$1 AND project_id=$2)", in.Evidence.ExecutionID, project).Scan(&ok); e != nil {
			return nil, e
		}
		if !ok {
			return nil, ErrNotFound
		}
	}
	now := time.Now().UTC()
	v, e := insertIssueTx(ctx, tx, a, project, in, "operator", trace, nil)
	if e != nil {
		return nil, e
	}
	response, _ := json.Marshal(map[string]string{"id": v.ID})
	if _, e = tx.ExecContext(ctx, `INSERT INTO idempotencies(id,actor_id,request_hash,response,created_at) VALUES($1,$2,$3,$4,$5)`, identity, a.User.ID, hash, response, now); e != nil {
		return nil, e
	}
	v.AllowedTransitions = issueTransitions(v, role, a.User.ID)
	return v, tx.Commit()
}
func (s *Store) ListIssues(ctx context.Context, a *AuthSession, p Pagination, f IssueFilter) (Page[Issue], error) {
	out := NewPage[Issue](p)
	if e := p.Validate(); e != nil {
		return out, e
	}
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	if f.ProjectID != "" {
		if _, e = issueRoleTx(ctx, tx, a, f.ProjectID); e != nil {
			return out, e
		}
	}
	where := ` WHERE ($1 OR EXISTS(SELECT 1 FROM memberships m WHERE m.user_id=$2 AND m.project_id=i.project_id))`
	args := []any{a.User.IsAdmin, a.User.ID}
	for _, filter := range []struct{ field, value string }{{"project_id", f.ProjectID}, {"state", f.State}, {"category", f.Category}, {"source", f.Source}, {"phase", f.Phase}, {"responsible_domain", f.ResponsibleDomain}} {
		if filter.value != "" {
			args = append(args, filter.value)
			where += fmt.Sprintf(" AND i.%s=$%d", filter.field, len(args))
		}
	}
	if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM issues i"+where, args...).Scan(&out.Total); e != nil {
		return out, e
	}
	args = append(args, p.PageSize, p.Offset())
	rows, e := tx.QueryContext(ctx, "SELECT "+issueColumns+" FROM issues i"+where+fmt.Sprintf(" ORDER BY updated_at DESC,id DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args)), args...)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		v, err := scanIssue(rows)
		if err != nil {
			rows.Close()
			return out, err
		}
		out.Items = append(out.Items, *v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	for i := range out.Items {
		v := &out.Items[i]
		role, err := issueRoleTx(ctx, tx, a, v.ProjectID)
		if err != nil {
			return out, err
		}
		v.AllowedTransitions = issueTransitions(v, role, a.User.ID)
	}
	return out, tx.Commit()
}
func (s *Store) GetIssue(ctx context.Context, a *AuthSession, id string) (*IssueDetail, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	v, e := scanIssue(tx.QueryRowContext(ctx, "SELECT "+issueColumns+" FROM issues WHERE id=$1", id))
	if errors.Is(e, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if e != nil {
		return nil, e
	}
	role, e := issueRoleTx(ctx, tx, a, v.ProjectID)
	if e != nil {
		return nil, e
	}
	v.AllowedTransitions = issueTransitions(v, role, a.User.ID)
	out := &IssueDetail{Issue: *v, Events: []IssueEvent{}}
	rows, e := tx.QueryContext(ctx, `SELECT id,version,actor_id,action,data,created_at FROM issue_events WHERE issue_id=$1 ORDER BY version`, id)
	if e != nil {
		return nil, e
	}
	for rows.Next() {
		var ev IssueEvent
		if e = rows.Scan(&ev.ID, &ev.Version, &ev.ActorID, &ev.Action, &ev.Data, &ev.CreatedAt); e != nil {
			rows.Close()
			return nil, e
		}
		out.Events = append(out.Events, ev)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	return out, tx.Commit()
}
func (s *Store) IssueAssignees(ctx context.Context, a *AuthSession, project string) ([]IssueAssignee, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	role, e := issueRoleTx(ctx, tx, a, project)
	if e != nil {
		return nil, e
	}
	if !issueManager(role) {
		return nil, ErrForbidden
	}
	rows, e := tx.QueryContext(ctx, `SELECT u.id,u.display_name,u.username FROM users u JOIN memberships m ON m.user_id=u.id WHERE m.project_id=$1 AND m.role='NE' AND u.enabled ORDER BY u.display_name,u.id`, project)
	if e != nil {
		return nil, e
	}
	out := []IssueAssignee{}
	for rows.Next() {
		var v IssueAssignee
		if e = rows.Scan(&v.ID, &v.DisplayName, &v.Username); e != nil {
			rows.Close()
			return nil, e
		}
		out = append(out, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	return out, tx.Commit()
}
func (s *Store) ActOnIssue(ctx context.Context, a *AuthSession, id, action string, in IssueActionInput, trace string) (*Issue, error) {
	if in.Version < 1 || len(strings.TrimSpace(in.Reason)) < 1 || utf8.RuneCountInString(in.Reason) > 2000 {
		return nil, apiError(400, "VALIDATION_ERROR", "Current version and reason are required")
	}
	if (action != "assign" && in.AssigneeID != "") || (action != "repair" && (in.RepairAction != "" || in.OldSerial != "" || in.NewSerial != "")) {
		return nil, apiError(400, "VALIDATION_ERROR", "Fields do not apply to this issue action")
	}
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	v, e := scanIssue(tx.QueryRowContext(ctx, "SELECT "+issueColumns+" FROM issues WHERE id=$1 FOR UPDATE", id))
	if errors.Is(e, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if e != nil {
		return nil, e
	}
	role, e := issueRoleTx(ctx, tx, a, v.ProjectID)
	if e != nil {
		return nil, e
	}
	if !issueManager(role) {
		return nil, ErrForbidden
	}
	if v.Version != in.Version {
		return nil, errVersion
	}
	if action == "verify" || action == "close" {
		return nil, apiError(409, "ISSUE_REVALIDATION_REQUIRED", "Current scoped revalidation evidence is required; this action is not available")
	}
	if !slices.Contains(issueTransitions(v, role, a.User.ID), action) {
		return nil, apiError(409, "ISSUE_TRANSITION_DENIED", "This action is not allowed in the current state or assignment")
	}
	state := v.State
	assignee := v.AssigneeID
	switch action {
	case "assign":
		if !validEntityID(in.AssigneeID) {
			return nil, apiError(400, "VALIDATION_ERROR", "An active project NE is required")
		}
		var ok bool
		e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users u JOIN memberships m ON m.user_id=u.id WHERE u.id=$1 AND u.enabled AND m.project_id=$2 AND m.role='NE')`, in.AssigneeID, v.ProjectID).Scan(&ok)
		if e != nil {
			return nil, e
		}
		if !ok {
			return nil, apiError(409, "ISSUE_ASSIGNEE_UNAVAILABLE", "Assignee must be an active NE in this project")
		}
		state = "assigned"
		assignee = in.AssigneeID
	case "start":
		state = "in_repair"
	case "repair":
		if !slices.Contains([]string{"reseat", "replace_cable", "replace_optic", "replace_nic", "rma_machine", "switch_repair", "configuration_fix", "flash_bios", "rma_board"}, in.RepairAction) || in.RepairAction == "flash_bios" && v.Category != "T7" {
			return nil, apiError(400, "VALIDATION_ERROR", "Invalid repair action for this category")
		}
		if slices.Contains([]string{"replace_cable", "replace_optic", "replace_nic", "rma_machine", "rma_board"}, in.RepairAction) && (strings.TrimSpace(in.OldSerial) == "" || strings.TrimSpace(in.NewSerial) == "" || strings.TrimSpace(in.OldSerial) == strings.TrimSpace(in.NewSerial)) {
			return nil, apiError(400, "VALIDATION_ERROR", "Replacement requires old and new physical serials/labels")
		}
		if utf8.RuneCountInString(in.OldSerial) > 240 || utf8.RuneCountInString(in.NewSerial) > 240 {
			return nil, apiError(400, "VALIDATION_ERROR", "Serial/label is too long")
		}
	}
	now := time.Now().UTC()
	v, e = scanIssue(tx.QueryRowContext(ctx, `UPDATE issues SET state=$2,assignee_id=$3,version=version+1,updated_at=$4 WHERE id=$1 AND version=$5 RETURNING `+issueColumns, id, state, assignee, now, in.Version))
	if e != nil {
		return nil, e
	}
	if e = issueEventTx(ctx, tx, a, v, action, trace, in); e != nil {
		return nil, e
	}
	v.AllowedTransitions = issueTransitions(v, role, a.User.ID)
	return v, tx.Commit()
}

// Targets are project-scoped stable identities, including unmapped ports. A
// human chooses an object label; no G3 or OS interface name is required.
type IssueTarget struct {
	Kind  string `json:"kind"`
	ID    string `json:"id"`
	Label string `json:"label"`
}

func (s *Store) IssueTargets(ctx context.Context, a *AuthSession, project, q string) ([]IssueTarget, error) {
	if utf8.RuneCountInString(q) > 120 {
		return nil, apiError(400, "VALIDATION_ERROR", "Search is too long")
	}
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	role, e := issueRoleTx(ctx, tx, a, project)
	if e != nil {
		return nil, e
	}
	if !issueManager(role) {
		return nil, ErrForbidden
	}
	rows, e := tx.QueryContext(ctx, `WITH scoped_bom AS (
 SELECT DISTINCT b.item_uid,b.category,b.serial FROM bom_items b JOIN bom_item_revisions r ON r.item_uid=b.item_uid JOIN bom_import_packages p ON p.id=r.package_id WHERE r.project_id=$1 AND p.state='committed'
 ), targets AS (
 SELECT 'machine' AS kind,m.id,a.hostname||' / '||m.serial_number AS label FROM project_assets a JOIN machines m ON m.id=a.machine_id WHERE a.project_id=$1
 UNION ALL SELECT CASE WHEN b.category IN('nic','gpu','psu','bmc','optic_module') THEN 'component' ELSE b.category END,b.item_uid,coalesce(nullif(b.serial,''),b.item_uid) FROM scoped_bom b WHERE b.category IN('switch','cable','nic','gpu','psu','bmc','optic_module')
 UNION ALL SELECT 'port',p.id,coalesce(m.serial_number,s.serial_number,'')||' / '||p.port_name FROM physical_ports p LEFT JOIN machines m ON m.id=p.machine_id LEFT JOIN switches s ON s.id=p.switch_id WHERE EXISTS(SELECT 1 FROM project_assets a WHERE a.project_id=$1 AND a.machine_id=p.machine_id) OR EXISTS(SELECT 1 FROM scoped_bom b WHERE b.item_uid=p.id)
 ) SELECT kind,id,label FROM targets WHERE $2='' OR strpos(lower(label),lower($2))>0 ORDER BY kind,label,id LIMIT 50`, project, q)
	if e != nil {
		return nil, e
	}
	out := []IssueTarget{}
	for rows.Next() {
		var v IssueTarget
		if e = rows.Scan(&v.Kind, &v.ID, &v.Label); e != nil {
			rows.Close()
			return nil, e
		}
		out = append(out, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	return out, tx.Commit()
}
