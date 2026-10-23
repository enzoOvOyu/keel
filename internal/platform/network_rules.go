package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/enzoOvOyu/keel/internal/fabricmon"
	"github.com/enzoOvOyu/keel/internal/netbaseline"
	"reflect"
	"strings"
	"time"
)

type NetworkRuleInput struct {
	ConditionParameters map[string]json.Number `json:"condition_parameters,omitempty"`
	BaseVersionID       string                 `json:"base_version_id"`
	Rule                fabricmon.LosslessRule `json:"rule"`
	MetricUnit          string                 `json:"metric_unit"`
	ChangeReason        string                 `json:"change_reason"`
	OverrideReason      string                 `json:"override_reason"`
	ConfirmOverride     bool                   `json:"confirm_override"`
	ExpectedPreviewHash string                 `json:"expected_preview_hash"`
}
type NetworkRuleSignature struct {
	UserID          string    `json:"user_id"`
	Role            string    `json:"approval_role"`
	Authority       string    `json:"authority_role"`
	SecurityVersion int       `json:"security_version"`
	PreviewHash     string    `json:"preview_hash"`
	CreatedAt       time.Time `json:"created_at"`
}
type NetworkRuleVersion struct {
	ConditionParameters map[string]json.Number `json:"condition_parameters"`
	ID                  string                 `json:"id"`
	ProjectID           string                 `json:"project_id"`
	RuleID              string                 `json:"rule_id"`
	Domain              string                 `json:"domain"`
	Version             int                    `json:"version"`
	State               string                 `json:"state"`
	TargetState         string                 `json:"target_state"`
	Rule                fabricmon.LosslessRule `json:"rule"`
	MetricUnit          string                 `json:"metric_unit"`
	ScopeHash           string                 `json:"scope_hash"`
	ContentHash         string                 `json:"content_hash"`
	PreviewHash         string                 `json:"preview_hash"`
	Preview             *NetworkRulePreview    `json:"preview_report"`
	ChangeReason        string                 `json:"change_reason"`
	OverrideReason      string                 `json:"override_reason"`
	SupersedesID        string                 `json:"supersedes_id"`
	CreatedBy           string                 `json:"created_by"`
	CreatedAt           time.Time              `json:"created_at"`
	PublishedBy         string                 `json:"published_by"`
	PublishedAt         *time.Time             `json:"published_at"`
	Signatures          []NetworkRuleSignature `json:"signatures"`
}
type NetworkRulePreview struct {
	CandidateVersion int                           `json:"candidate_version"`
	EvaluationPoints int                           `json:"evaluation_points"`
	ReplayComplete   bool                          `json:"replay_complete"`
	Version          string                        `json:"version"`
	ProjectID        string                        `json:"project_id"`
	PackageID        string                        `json:"package_id"`
	EvidenceSource   string                        `json:"evidence_source"`
	CollectionRef    string                        `json:"collection_ref"`
	IntentHash       string                        `json:"intent_hash"`
	CurrentRulesHash string                        `json:"current_rules_hash"`
	CandidateHash    string                        `json:"candidate_hash"`
	PreviewHash      string                        `json:"preview_hash"`
	WindowHours      int                           `json:"window_hours"`
	SampleCount      int                           `json:"sample_count"`
	Diff             fabricmon.LosslessPreviewDiff `json:"diff"`
}
type NetworkRuleCatalog struct {
	ConditionParameters []fabricmon.LosslessConditionParameter `json:"condition_parameters"`
	Domain              string                                 `json:"domain"`
	SupportedDomains    []string                               `json:"supported_domains"`
	Defaults            []fabricmon.LosslessRule               `json:"defaults"`
	Metrics             []fabricmon.GovernanceMetric           `json:"metrics"`
	Versions            []NetworkRuleVersion                   `json:"versions"`
}

const networkRuleFields = `id,project_id,rule_id,domain,version,state,target_state,definition,metric_unit,scope_hash,content_hash,preview_hash,preview_report,change_reason,override_reason,coalesce(supersedes_id,''),created_by,created_at,coalesce(published_by,''),published_at`

func scanNetworkRule(row scanner) (*NetworkRuleVersion, error) {
	var v NetworkRuleVersion
	var raw, preview []byte
	e := row.Scan(&v.ID, &v.ProjectID, &v.RuleID, &v.Domain, &v.Version, &v.State, &v.TargetState, &raw, &v.MetricUnit, &v.ScopeHash, &v.ContentHash, &v.PreviewHash, &preview, &v.ChangeReason, &v.OverrideReason, &v.SupersedesID, &v.CreatedBy, &v.CreatedAt, &v.PublishedBy, &v.PublishedAt)
	if e != nil {
		return nil, e
	}
	if e = json.Unmarshal(raw, &v.Rule); e != nil {
		return nil, e
	}
	if e = json.Unmarshal(preview, &v.Preview); e != nil {
		return nil, e
	}
	v.ConditionParameters, e = fabricmon.LosslessConditionValues(v.Rule)
	if e != nil {
		return nil, e
	}
	v.Signatures = []NetworkRuleSignature{}
	return &v, nil
}
func loadNetworkRuleTx(ctx context.Context, tx *sql.Tx, project, id string, lock bool) (*NetworkRuleVersion, error) {
	query := "SELECT " + networkRuleFields + " FROM network_rule_versions WHERE project_id=$1 AND id=$2"
	if lock {
		query += " FOR UPDATE"
	}
	v, e := scanNetworkRule(tx.QueryRowContext(ctx, query, project, id))
	if errors.Is(e, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return v, e
}
func loadNetworkRuleSignaturesTx(ctx context.Context, tx *sql.Tx, v *NetworkRuleVersion) error {
	rows, e := tx.QueryContext(ctx, `SELECT user_id,approval_role,authority_role,security_version,preview_hash,created_at FROM network_rule_approvals WHERE version_id=$1 ORDER BY approval_role`, v.ID)
	if e != nil {
		return e
	}
	defer rows.Close()
	for rows.Next() {
		var sig NetworkRuleSignature
		if e = rows.Scan(&sig.UserID, &sig.Role, &sig.Authority, &sig.SecurityVersion, &sig.PreviewHash, &sig.CreatedAt); e != nil {
			return e
		}
		v.Signatures = append(v.Signatures, sig)
	}
	return rows.Err()
}
func builtinNetworkRule(id string) (fabricmon.LosslessRule, error) {
	catalog, e := fabricmon.DefaultLosslessCatalog()
	if e != nil {
		return fabricmon.LosslessRule{}, e
	}
	for _, r := range catalog.Rules {
		if r.ID == id {
			return r, nil
		}
	}
	return fabricmon.LosslessRule{}, apiError(400, "NETWORK_RULE_UNKNOWN", "Select a registered built-in lossless rule")
}
func requireNetworkRuleEditor(ctx context.Context, tx *sql.Tx, a *AuthSession, project string) error {
	if e := requireFabricPermission(ctx, tx, a, project, "fabric:read"); e != nil {
		return e
	}
	role, e := networkRoleTx(ctx, tx, a, project)
	if e != nil {
		return e
	}
	if role != "NE" && role != "Admin" {
		return ErrForbidden
	}
	return nil
}

type networkRulePreviewKey struct{}
type networkRuleEvaluationOverride struct{ Rules []fabricmon.LosslessRule }

// Called by networkHealthTx after parameter weights are loaded. No cache can
// retain disabled rules beyond the next request/30-second readiness refresh.
func networkLosslessRulesTx(ctx context.Context, tx *sql.Tx, project string, defaults []fabricmon.LosslessRule) ([]fabricmon.LosslessRule, error) {
	if v, ok := ctx.Value(networkRulePreviewKey{}).(networkRuleEvaluationOverride); ok {
		return v.Rules, nil
	}
	return effectiveNetworkRulesTx(ctx, tx, project, defaults)
}
func effectiveNetworkRulesTx(ctx context.Context, tx *sql.Tx, project string, defaults []fabricmon.LosslessRule) ([]fabricmon.LosslessRule, error) {
	out := append([]fabricmon.LosslessRule{}, defaults...)
	rows, e := tx.QueryContext(ctx, `SELECT definition,version,state FROM network_rule_versions v WHERE v.project_id=$1 AND v.state IN('released','disabled') AND NOT EXISTS(SELECT 1 FROM network_rule_versions n WHERE n.project_id=v.project_id AND n.supersedes_id=v.id AND n.state IN('released','disabled')) ORDER BY rule_id,version`, project)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		var r fabricmon.LosslessRule
		var version int
		var state string
		if e = rows.Scan(&raw, &version, &state); e != nil {
			return nil, e
		}
		if e = json.Unmarshal(raw, &r); e != nil {
			return nil, e
		}
		r.Version = version
		r.State = state
		// A fabric-wide override must replace, not tie with, the same default scope.
		if reflect.DeepEqual(r.Scope, fabricmon.RuleScope{}) {
			kept := out[:0]
			for _, old := range out {
				if old.ID != r.ID || !reflect.DeepEqual(old.Scope, r.Scope) {
					kept = append(kept, old)
				}
			}
			out = kept
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func validateNetworkRuleScopeTx(ctx context.Context, tx *sql.Tx, project string, scope fabricmon.RuleScope) error {
	_, devices, _, e := fabricInventoryTx(ctx, tx, project)
	if e != nil {
		return e
	}
	if reflect.DeepEqual(scope, fabricmon.RuleScope{}) {
		return nil
	}
	matched := false
	for _, d := range devices {
		if scope.Device != "" && scope.Device != d.ID || scope.Pod != "" && scope.Pod != d.Pod || scope.Role != "" && scope.Role != d.Role {
			continue
		}
		if scope.Port != "" {
			var exists bool
			e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM bom_items b JOIN bom_item_revisions r ON r.item_uid=b.item_uid JOIN bom_import_packages p ON p.id=r.package_id WHERE p.project_id=$1 AND p.id=(SELECT id FROM bom_import_packages WHERE project_id=$1 AND state='committed' ORDER BY committed_at DESC,id DESC LIMIT 1) AND b.category='port' AND b.parent_uid=$2 AND b.port_name=$3 AND b.status<>'retired')`, project, d.ID, scope.Port).Scan(&exists)
			if e != nil {
				return e
			}
			if !exists {
				continue
			}
		}
		if scope.Priority != nil {
			variables, e := loadLosslessIntentTx(ctx, tx, project)
			if e != nil {
				return e
			}
			units, _ := deviceLosslessUnits(d, variables[d.Serial])
			priorityExists := false
			for _, u := range units {
				if u.Priority == *scope.Priority && (scope.Port == "" || scope.Port == u.Port) {
					priorityExists = true
					break
				}
			}
			if !priorityExists {
				continue
			}
		}
		matched = true
		break
	}
	if !matched {
		return apiError(400, "NETWORK_RULE_SCOPE_EMPTY", "Every populated scope dimension must resolve to a current BOM/approved-intent unit")
	}
	return nil
}
func networkRuleInputTx(ctx context.Context, tx *sql.Tx, project string, input NetworkRuleInput) (fabricmon.LosslessRule, *NetworkRuleVersion, error) {
	base, e := builtinNetworkRule(input.Rule.ID)
	if e != nil {
		return base, nil, e
	}
	var parent *NetworkRuleVersion
	if input.BaseVersionID != "" {
		parent, e = loadNetworkRuleTx(ctx, tx, project, input.BaseVersionID, false)
		if e != nil {
			return base, nil, e
		}
		if parent.RuleID != input.Rule.ID || parent.State == "draft" {
			return base, nil, apiError(409, "NETWORK_RULE_BASE_INVALID", "Base must be a published version of this rule")
		}
	}
	input.Rule.Version = 1
	input.Rule, e = fabricmon.ApplyLosslessConditionParameters(input.Rule, input.ConditionParameters)
	if e != nil {
		return base, nil, apiError(400, "NETWORK_RULE_CONDITION_INVALID", e.Error())
	}
	if len(strings.TrimSpace(input.ChangeReason)) < 8 || len(input.ChangeReason) > 1000 {
		return base, nil, apiError(400, "NETWORK_RULE_REASON_REQUIRED", "Provide a change reason of 8 through 1000 characters")
	}
	if e = fabricmon.ValidateGovernedLossless(input.Rule, base, input.MetricUnit, input.OverrideReason, input.ConfirmOverride); e != nil {
		return base, nil, apiError(400, "NETWORK_RULE_INVALID", e.Error())
	}
	if e = validateNetworkRuleScopeTx(ctx, tx, project, input.Rule.Scope); e != nil {
		return base, nil, e
	}
	return input.Rule, parent, nil
}
func allNetworkHealthFindings(report *NetworkHealthReport) []fabricmon.LosslessFinding {
	out := []fabricmon.LosslessFinding{}
	for _, d := range report.Devices {
		out = append(out, d.Lossless.Findings...)
	}
	return out
}

// The preview runs with a read-only transaction. Reports and their digests only
// become durable as immutable evidence when a user explicitly creates a draft.
func (s *Store) previewNetworkRuleTx(ctx context.Context, tx *sql.Tx, project string, input NetworkRuleInput) (*NetworkRulePreview, error) {
	rule, parent, e := networkRuleInputTx(ctx, tx, project, input)
	if e != nil {
		return nil, e
	}
	defaults, e := networkRuleDefaultsTx(ctx, tx, project)
	if e != nil {
		return nil, e
	}
	current, e := effectiveNetworkRulesTx(ctx, tx, project, defaults)
	if e != nil {
		return nil, e
	}
	candidate := []fabricmon.LosslessRule{}
	if parent != nil {
		active := false
		for _, r := range current {
			if r.ID == parent.RuleID && r.Version == parent.Version {
				active = true
			}
		}
		if !active {
			return nil, apiError(409, "NETWORK_RULE_BASE_STALE", "Base version has been superseded")
		}
	}
	for _, r := range current {
		if parent == nil && r.ID == rule.ID && r.Version > 1 && reflect.DeepEqual(r.Scope, rule.Scope) {
			return nil, apiError(409, "NETWORK_RULE_BASE_REQUIRED", "Select the current published version before revising this scope")
		}
		if r.ID == rule.ID && ((parent != nil && r.Version == parent.Version) || (r.Version == 1 && reflect.DeepEqual(r.Scope, rule.Scope))) {
			continue
		}
		candidate = append(candidate, r)
	}
	// The preview reserves no row, but binds the next monotonic version. A
	// concurrent draft changes this digest and requires a new preview before save.
	var nextVersion int
	e = tx.QueryRowContext(ctx, `SELECT coalesce(max(version),1)+1 FROM network_rule_versions WHERE project_id=$1 AND rule_id=$2`, project, rule.ID).Scan(&nextVersion)
	if e != nil {
		return nil, e
	}
	rule.Version = nextVersion

	candidate = append(candidate, rule)
	devicesBefore, devicesAfter, samplesCount, evaluationCount, replayWarnings, e := networkRuleReplayTx(ctx, tx, project, current, candidate)
	if e != nil {
		return nil, e
	}

	collection, e := latestFabricCollectionTx(ctx, tx, project)
	if e != nil {
		return nil, e
	}
	pkg, _, _, e := fabricInventoryTx(ctx, tx, project)
	if e != nil {
		return nil, e
	}
	out := &NetworkRulePreview{Version: "network-rule-preview/v1", ProjectID: project, PackageID: pkg, CurrentRulesHash: netbaseline.ContentHash(current), CandidateHash: netbaseline.ContentHash(input), WindowHours: 48, Diff: fabricmon.DiffLosslessFindings(devicesBefore, devicesAfter)}
	if collection != nil {
		out.CollectionRef = collection.ID
		out.EvidenceSource = collection.EvidenceSource
	}
	if collection == nil || collection.Stale {
		out.Diff.Warnings = append(out.Diff.Warnings, "current_collection_missing_or_stale")
	}
	out.CandidateVersion = nextVersion
	out.SampleCount = samplesCount
	out.EvaluationPoints = evaluationCount
	out.ReplayComplete = true
	out.Diff.Warnings = append(out.Diff.Warnings, replayWarnings...)

	intent, e := loadLosslessIntentTx(ctx, tx, project)
	if e != nil {
		return nil, e
	}
	out.IntentHash = netbaseline.ContentHash(intent)
	out.PreviewHash = netbaseline.ContentHash(out)
	return out, nil
}
func networkRuleIncludeFindings(ctx context.Context) bool {
	_, ok := ctx.Value(networkRulePreviewKey{}).(networkRuleEvaluationOverride)
	return ok
}
func (s *Store) ruleHealthFindingsTx(ctx context.Context, tx *sql.Tx, project string) ([]fabricmon.LosslessFinding, error) {
	report, e := networkHealthTx(ctx, tx, project)
	if e != nil {
		return nil, e
	}
	return allNetworkHealthFindings(report), nil
}
func (s *Store) PreviewNetworkRule(ctx context.Context, a *AuthSession, p *ProjectSummary, input NetworkRuleInput) (*NetworkRulePreview, error) {
	input.ExpectedPreviewHash = ""
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireNetworkRuleEditor(ctx, tx, a, p.ID); e != nil {
		return nil, e
	}
	return s.previewNetworkRuleTx(ctx, tx, p.ID, input)
}
func (s *Store) ListNetworkRules(ctx context.Context, a *AuthSession, p *ProjectSummary) (*NetworkRuleCatalog, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireFabricPermission(ctx, tx, a, p.ID, "fabric:read"); e != nil {
		return nil, e
	}
	catalog, e := fabricmon.DefaultLosslessCatalog()
	if e != nil {
		return nil, e
	}
	out := &NetworkRuleCatalog{ConditionParameters: fabricmon.LosslessConditionParameters(), Domain: "lossless", SupportedDomains: []string{"lossless"}, Defaults: catalog.Rules, Metrics: fabricmon.LosslessGovernanceMetrics(), Versions: []NetworkRuleVersion{}}
	rows, e := tx.QueryContext(ctx, `SELECT `+networkRuleFields+` FROM network_rule_versions WHERE project_id=$1 ORDER BY rule_id,version DESC LIMIT 1000`, p.ID)
	if e != nil {
		return nil, e
	}
	for rows.Next() {
		v, e := scanNetworkRule(rows)
		if e != nil {
			rows.Close()
			return nil, e
		}
		out.Versions = append(out.Versions, *v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	for i := range out.Versions {
		if e = loadNetworkRuleSignaturesTx(ctx, tx, &out.Versions[i]); e != nil {
			return nil, e
		}
	}
	return out, nil
}
func (s *Store) CreateNetworkRule(ctx context.Context, a *AuthSession, p *ProjectSummary, input NetworkRuleInput, trace string) (*NetworkRuleVersion, error) {
	expected := input.ExpectedPreviewHash
	input.ExpectedPreviewHash = ""
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireNetworkRuleEditor(ctx, tx, a, p.ID); e != nil {
		return nil, e
	}
	if e = lockKey(ctx, tx, "network-rule:"+p.ID+":"+input.Rule.ID); e != nil {
		return nil, e
	}
	preview, e := s.previewNetworkRuleTx(ctx, tx, p.ID, input)
	if e != nil {
		return nil, e
	}
	if expected == "" || expected != preview.PreviewHash {
		return nil, apiError(409, "NETWORK_RULE_PREVIEW_CHANGED", "Run preview against the current samples/rules and use its exact hash")
	}
	var version int
	e = tx.QueryRowContext(ctx, `SELECT coalesce(max(version),1)+1 FROM network_rule_versions WHERE project_id=$1 AND rule_id=$2`, p.ID, input.Rule.ID).Scan(&version)
	if e != nil {
		return nil, e
	}
	definition, _, e := networkRuleInputTx(ctx, tx, p.ID, input)
	if e != nil {
		return nil, e
	}
	definition.Version = version
	v := &NetworkRuleVersion{ID: RandomID(), ProjectID: p.ID, RuleID: definition.ID, Domain: "lossless", Version: version, State: "draft", TargetState: definition.State, Rule: definition, MetricUnit: input.MetricUnit, ScopeHash: netbaseline.ContentHash(definition.Scope), ContentHash: netbaseline.ContentHash(definition), PreviewHash: preview.PreviewHash, Preview: preview, ChangeReason: input.ChangeReason, OverrideReason: input.OverrideReason, SupersedesID: input.BaseVersionID, CreatedBy: a.User.ID, CreatedAt: time.Now().UTC(), Signatures: []NetworkRuleSignature{}}
	_, e = tx.ExecContext(ctx, `INSERT INTO network_rule_versions(id,project_id,rule_id,domain,version,state,target_state,definition,metric_unit,scope_hash,content_hash,preview_hash,preview_report,change_reason,override_reason,supersedes_id,created_by,created_at) VALUES($1,$2,$3,'lossless',$4,'draft',$5,$6,$7,$8,$9,$10,$11,$12,$13,nullif($14,''),$15,$16)`, v.ID, p.ID, v.RuleID, version, v.TargetState, networkJSON(v.Rule), v.MetricUnit, v.ScopeHash, v.ContentHash, v.PreviewHash, networkJSON(v.Preview), v.ChangeReason, v.OverrideReason, v.SupersedesID, a.User.ID, v.CreatedAt)
	if e != nil {
		return nil, e
	}
	if e = appendAudit(ctx, tx, a.User.ID, p.ID, "network.rule.draft", v.ID, trace, map[string]any{"rule_id": v.RuleID, "version": version, "preview_hash": v.PreviewHash, "change_reason": v.ChangeReason, "override_reason": v.OverrideReason, "old_version_id": v.SupersedesID, "new": v.Rule}); e != nil {
		return nil, e
	}
	v.ConditionParameters, _ = fabricmon.LosslessConditionValues(v.Rule)
	return v, tx.Commit()
}
func ruleNeedsDual(v *NetworkRuleVersion, parent *NetworkRuleVersion) bool {
	oldCritical := false
	if parent != nil {
		oldCritical = parent.Rule.Severity == "critical"
	} else {
		builtin, _ := builtinNetworkRule(v.RuleID)
		oldCritical = builtin.Severity == "critical"
	}
	return v.Rule.Severity == "critical" || oldCritical
}
func ruleApprovalAuthorityTx(ctx context.Context, tx *sql.Tx, project, user string) (string, int, error) {
	var admin, enabled bool
	var security int
	e := tx.QueryRowContext(ctx, `SELECT enabled,is_admin,security_version FROM users WHERE id=$1 FOR SHARE`, user).Scan(&enabled, &admin, &security)
	if errors.Is(e, sql.ErrNoRows) || e == nil && !enabled {
		return "", 0, ErrForbidden
	}
	if e != nil {
		return "", 0, e
	}
	if admin {
		return "Admin", security, nil
	}
	var role string
	e = tx.QueryRowContext(ctx, `SELECT role FROM memberships WHERE user_id=$1 AND project_id=$2 FOR SHARE`, user, project).Scan(&role)
	if errors.Is(e, sql.ErrNoRows) {
		return "", 0, ErrForbidden
	}
	return role, security, e
}
func (s *Store) ApproveNetworkRule(ctx context.Context, a *AuthSession, p *ProjectSummary, id, slot, hash, trace string) (*NetworkRuleVersion, error) {
	if slot != "NE" && slot != "NH" {
		return nil, apiError(400, "NETWORK_RULE_APPROVAL_ROLE", "Choose NE or NH approval")
	}
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireFabricPermission(ctx, tx, a, p.ID, "fabric:read"); e != nil {
		return nil, e
	}
	v, e := loadNetworkRuleTx(ctx, tx, p.ID, id, true)
	if e != nil {
		return nil, e
	}
	if v.State != "draft" || v.CreatedBy == a.User.ID || hash != v.PreviewHash {
		return nil, apiError(409, "NETWORK_RULE_APPROVAL_INVALID", "Approval requires a current draft, exact preview hash and an independent account")
	}
	authority, security, e := ruleApprovalAuthorityTx(ctx, tx, p.ID, a.User.ID)
	if e != nil {
		return nil, e
	}
	if authority != slot && authority != "Admin" {
		return nil, ErrForbidden
	}
	if e = loadNetworkRuleSignaturesTx(ctx, tx, v); e != nil {
		return nil, e
	}
	for _, sig := range v.Signatures {
		if sig.UserID == a.User.ID || sig.Role == slot {
			return nil, apiError(409, "NETWORK_RULE_DUPLICATE_APPROVAL", "Each role requires a distinct account")
		}
	}
	now := time.Now().UTC()
	_, e = tx.ExecContext(ctx, `INSERT INTO network_rule_approvals(id,version_id,user_id,approval_role,authority_role,security_version,preview_hash,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, RandomID(), id, a.User.ID, slot, authority, security, hash, now)
	if e != nil {
		return nil, e
	}
	v.Signatures = append(v.Signatures, NetworkRuleSignature{a.User.ID, slot, authority, security, hash, now})
	if e = appendAudit(ctx, tx, a.User.ID, p.ID, "network.rule.approve", id, trace, map[string]any{"approval_role": slot, "preview_hash": hash}); e != nil {
		return nil, e
	}
	v.ConditionParameters, _ = fabricmon.LosslessConditionValues(v.Rule)
	return v, tx.Commit()
}
func (s *Store) PublishNetworkRule(ctx context.Context, a *AuthSession, p *ProjectSummary, id, hash, trace string) (*NetworkRuleVersion, error) {
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireNetworkRuleEditor(ctx, tx, a, p.ID); e != nil {
		return nil, e
	}
	v, e := loadNetworkRuleTx(ctx, tx, p.ID, id, true)
	if e != nil {
		return nil, e
	}
	if v.State != "draft" || hash != v.PreviewHash {
		return nil, apiError(409, "NETWORK_RULE_PUBLISH_INVALID", "Select an unpublished draft with its exact preview hash")
	}
	if e = lockKey(ctx, tx, "network-rule:"+p.ID+":"+v.RuleID); e != nil {
		return nil, e
	}
	var parent *NetworkRuleVersion
	if v.SupersedesID != "" {
		parent, e = loadNetworkRuleTx(ctx, tx, p.ID, v.SupersedesID, false)
		if e != nil {
			return nil, e
		}
	}
	if e = loadNetworkRuleSignaturesTx(ctx, tx, v); e != nil {
		return nil, e
	}
	slots := map[string]bool{}
	for _, sig := range v.Signatures {
		authority, security, e := ruleApprovalAuthorityTx(ctx, tx, p.ID, sig.UserID)
		if e != nil || authority != sig.Authority || security != sig.SecurityVersion || sig.PreviewHash != hash || sig.UserID == v.CreatedBy {
			return nil, apiError(409, "NETWORK_RULE_APPROVER_CHANGED", "An independent signer no longer holds the signed authority")
		}
		slots[sig.Role] = true
	}
	if !slots["NE"] || ruleNeedsDual(v, parent) && !slots["NH"] {
		return nil, apiError(409, "NETWORK_RULE_QUORUM_REQUIRED", "Complete current NE and, for critical changes, independent NH approval")
	}
	// A preview is tied to the collection and effective rule set, not a client claim.
	defaults, e := networkRuleDefaultsTx(ctx, tx, p.ID)
	if e != nil {
		return nil, e
	}
	current, e := effectiveNetworkRulesTx(ctx, tx, p.ID, defaults)
	if e != nil {
		return nil, e
	}
	collection, e := latestFabricCollectionTx(ctx, tx, p.ID)
	if e != nil {
		return nil, e
	}
	collectionID := ""
	if collection != nil {
		collectionID = collection.ID
	}
	pkg, _, _, e := fabricInventoryTx(ctx, tx, p.ID)
	if e != nil {
		return nil, e
	}
	intent, e := loadLosslessIntentTx(ctx, tx, p.ID)
	if e != nil {
		return nil, e
	}
	if netbaseline.ContentHash(intent) != v.Preview.IntentHash || netbaseline.ContentHash(current) != v.Preview.CurrentRulesHash || collectionID != v.Preview.CollectionRef || pkg != v.Preview.PackageID {
		return nil, apiError(409, "NETWORK_RULE_PREVIEW_STALE", "Samples, BOM or effective rules changed; create a newly previewed version")
	}
	now := time.Now().UTC()
	_, e = tx.ExecContext(ctx, `UPDATE network_rule_versions SET state=target_state,published_by=$3,published_at=$4 WHERE id=$1 AND project_id=$2`, id, p.ID, a.User.ID, now)
	if e != nil {
		return nil, e
	}
	v.State = v.TargetState
	v.PublishedBy = a.User.ID
	v.PublishedAt = &now
	if e = appendAudit(ctx, tx, a.User.ID, p.ID, "network.rule.publish", id, trace, map[string]any{"rule_id": v.RuleID, "version": v.Version, "state": v.State, "old_version_id": v.SupersedesID, "new": v.Rule, "change_reason": v.ChangeReason, "approvals": v.Signatures, "preview_hash": hash}); e != nil {
		return nil, e
	}
	v.ConditionParameters, _ = fabricmon.LosslessConditionValues(v.Rule)
	return v, tx.Commit()
}

// This endpoint constructs a reviewed rollback candidate; saving it still
// requires a fresh preview, creates a new version and uses the same approvals.
func (s *Store) NetworkRuleRollbackInput(ctx context.Context, a *AuthSession, p *ProjectSummary, id, target string) (*NetworkRuleInput, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireNetworkRuleEditor(ctx, tx, a, p.ID); e != nil {
		return nil, e
	}
	current, e := loadNetworkRuleTx(ctx, tx, p.ID, id, false)
	if e != nil {
		return nil, e
	}
	rule, e := builtinNetworkRule(current.RuleID)
	if e != nil {
		return nil, e
	}
	rule.Scope = current.Rule.Scope
	if target != "default" {
		old, e := loadNetworkRuleTx(ctx, tx, p.ID, target, false)
		if e != nil {
			return nil, e
		}
		if old.RuleID != current.RuleID || old.State == "draft" {
			return nil, apiError(400, "NETWORK_RULE_ROLLBACK_INVALID", "Choose a published historical version of the same rule")
		}
		rule = old.Rule
	}
	return &NetworkRuleInput{BaseVersionID: id, Rule: rule, ConditionParameters: mustLosslessConditionValues(rule), MetricUnit: current.MetricUnit, ChangeReason: fmt.Sprintf("Restore rule from %s; retain history", target)}, nil
}

func networkRuleDefaultsTx(ctx context.Context, tx *sql.Tx, project string) ([]fabricmon.LosslessRule, error) {
	catalog, e := fabricmon.DefaultLosslessCatalog()
	if e != nil {
		return nil, e
	}
	rows, e := tx.QueryContext(ctx, `SELECT default_value FROM param_keys WHERE key='fabric.health_score.weights' AND state='active' AND scope_key IN('global',$1) ORDER BY CASE WHEN scope_key='global' THEN 0 ELSE 1 END`, "project:"+project)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		if e = rows.Scan(&raw); e != nil {
			return nil, e
		}
		var weights map[string]float64
		if e = json.Unmarshal([]byte(raw), &weights); e != nil {
			return nil, apiError(409, "FABRIC_HEALTH_WEIGHTS_INVALID", "Health weights must map rule IDs to numbers")
		}
		for i := range catalog.Rules {
			if v, ok := weights[catalog.Rules[i].ID]; ok {
				if v < 0 || v > 100 {
					return nil, apiError(409, "FABRIC_HEALTH_WEIGHTS_INVALID", "Health weights must be 0 through 100")
				}
				catalog.Rules[i].Weight = v
			}
		}
	}
	return catalog.Rules, rows.Err()
}

func mustLosslessConditionValues(rule fabricmon.LosslessRule) map[string]json.Number {
	v, _ := fabricmon.LosslessConditionValues(rule)
	return v
}
