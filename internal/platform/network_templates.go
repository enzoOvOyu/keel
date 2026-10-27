package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/enzoOvOyu/keel/internal/netbaseline"
	"regexp"
	"strings"
	"sync"
	"time"
)

type NetworkTemplateInput struct {
	ParentVersionID    string               `json:"parent_version_id,omitempty"`
	ExpectedParentHash string               `json:"expected_parent_hash,omitempty"`
	ChangeReason       string               `json:"change_reason,omitempty"`
	Name               string               `json:"name"`
	SourceRef          string               `json:"source_ref"`
	Definition         netbaseline.Template `json:"definition"`
}
type NetworkTemplateVersion struct {
	ParentVersionID string                 `json:"parent_version_id,omitempty"`
	RollbackFromID  string                 `json:"rollback_from_id,omitempty"`
	ChangeReason    string                 `json:"change_reason,omitempty"`
	ID              string                 `json:"id"`
	ProjectID       string                 `json:"project_id"`
	Name            string                 `json:"name"`
	Version         int64                  `json:"version"`
	State           string                 `json:"state"`
	SourceRef       string                 `json:"source_ref"`
	Definition      netbaseline.Template   `json:"definition"`
	ContentHash     string                 `json:"content_hash"`
	TestReport      *NetworkTemplateReport `json:"test_report,omitempty"`
	CreatedBy       string                 `json:"created_by"`
	CreatedAt       time.Time              `json:"created_at"`
}
type NetworkPlanInput struct {
	TemplateChangeID   string `json:"template_change_id,omitempty"`
	ExpectedImpactHash string `json:"expected_impact_hash,omitempty"`
	TemplateID         string `json:"template_id"`
	BaselineRef        string `json:"baseline_ref"`
	BatchRef           string `json:"batch_ref"`
}
type NetworkPlannedDevice struct {
	CatalogSelections  []CatalogSelection `json:"catalog_selections,omitempty"`
	ApprovedVariables  map[string]any     `json:"approved_variables"`
	IntendedID         string             `json:"intended_id"`
	ID                 string             `json:"id"`
	Serial             string             `json:"serial"`
	Hostname           string             `json:"hostname"`
	Address            string             `json:"management_ip"`
	ModelVersionID     string             `json:"model_version_id"`
	CredentialBatchRef string             `json:"credential_batch_ref"`
	Intended           json.RawMessage    `json:"-"`
	IntendedHash       string             `json:"intended_hash"`
}
type NetworkTemplateReport struct {
	ID             string                        `json:"id"`
	BaselineID     string                        `json:"baseline_id"`
	BaselineRef    string                        `json:"baseline_ref"`
	BatchRef       string                        `json:"batch_ref"`
	FactsHash      string                        `json:"facts_hash"`
	ConfigHash     string                        `json:"config_hash"`
	EvidenceSource string                        `json:"evidence_source"`
	CheckedAt      time.Time                     `json:"checked_at"`
	Devices        []NetworkPlannedDevice        `json:"devices"`
	Checks         []netbaseline.OperationResult `json:"checks"`
}

const networkTemplateFields = "id,project_id,name,version,state,source_ref,definition,content_hash,test_report,created_by,created_at,coalesce(parent_version_id,''),coalesce(rollback_from_id,''),change_reason"

func scanNetworkTemplate(row scanner) (*NetworkTemplateVersion, error) {
	var v NetworkTemplateVersion
	var raw, report []byte
	e := row.Scan(&v.ID, &v.ProjectID, &v.Name, &v.Version, &v.State, &v.SourceRef, &raw, &v.ContentHash, &report, &v.CreatedBy, &v.CreatedAt, &v.ParentVersionID, &v.RollbackFromID, &v.ChangeReason)
	if e != nil {
		return nil, e
	}
	if e = json.Unmarshal(raw, &v.Definition); e != nil {
		return nil, e
	}
	if len(report) > 0 {
		e = json.Unmarshal(report, &v.TestReport)
	}
	return &v, e
}
func networkRoleTx(ctx context.Context, tx *sql.Tx, a *AuthSession, project string) (string, error) {
	if a.User.IsAdmin {
		return "Admin", nil
	}
	var role string
	e := tx.QueryRowContext(ctx, "SELECT role FROM memberships WHERE user_id=$1 AND project_id=$2", a.User.ID, project).Scan(&role)
	if errors.Is(e, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return role, e
}
func (s *Store) CreateNetworkTemplate(ctx context.Context, a *AuthSession, p *ProjectSummary, input NetworkTemplateInput, trace string) (*NetworkTemplateVersion, error) {
	input.Name = strings.TrimSpace(input.Name)
	if len(input.Name) < 1 || len(input.Name) > 120 || !validNetworkEvidenceRef(input.SourceRef) {
		return nil, apiError(400, "NETWORK_TEMPLATE_INVALID", "Template name and approved source reference are required")
	}
	if e := input.Definition.Validate(); e != nil {
		return nil, apiError(400, "NETWORK_TEMPLATE_INVALID", e.Error())
	}
	literalSecret := regexp.MustCompile(`(?i)"(?:password|token|secret|private[-_]key|community)"\s*:\s*"`)
	if strings.Contains(input.Definition.Source, "PRIVATE KEY-----") || literalSecret.MatchString(input.Definition.Source) {
		return nil, apiError(400, "NETWORK_TEMPLATE_SECRET_LITERAL", "Secret fields must use credential_ref objects")
	}
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireFabricPermission(ctx, tx, a, p.ID, "fabric:template:edit"); e != nil {
		return nil, e
	}
	input.ChangeReason = strings.TrimSpace(input.ChangeReason)
	if len([]rune(input.ChangeReason)) > 1000 {
		return nil, apiError(400, "NETWORK_TEMPLATE_INVALID", "Change reason exceeds 1000 characters")
	}
	head, e := latestNetworkTemplateTx(ctx, tx, p.ID, input.Name)
	if e != nil {
		return nil, e
	}
	if input.ParentVersionID != "" || input.ExpectedParentHash != "" {
		if head == nil || input.ParentVersionID != head.ID || input.ExpectedParentHash != head.ContentHash {
			return nil, apiError(409, "NETWORK_TEMPLATE_HEAD_CHANGED", "Review the latest family version before creating a successor")
		}
	}
	v, e := insertNetworkTemplateTx(ctx, tx, a, p.ID, input, head, "")
	if e != nil {
		return nil, e
	}
	if e = appendAudit(ctx, tx, a.User.ID, p.ID, "network.template.import", v.ID, trace, map[string]any{"version": v.Version, "content_hash": v.ContentHash, "source_ref": v.SourceRef, "parent_version_id": v.ParentVersionID, "change_reason": v.ChangeReason}); e != nil {
		return nil, e
	}
	return v, tx.Commit()
}

func networkTemplateFactsTx(ctx context.Context, tx *sql.Tx, project string, input NetworkPlanInput, lockCatalog bool) (*NetworkTemplateVersion, string, []NetworkPlannedDevice, error) {
	if !bootRouteRef.MatchString(input.BatchRef) || !bomProfileVersionPattern.MatchString(input.BaselineRef) {
		return nil, "", nil, apiError(400, "NETWORK_PLAN_INVALID", "Select an approved baseline and current batch")
	}
	template, e := scanNetworkTemplate(tx.QueryRowContext(ctx, "SELECT "+networkTemplateFields+" FROM network_template_versions WHERE id=$1 AND project_id=$2", input.TemplateID, project))
	if errors.Is(e, sql.ErrNoRows) {
		return nil, "", nil, ErrNotFound
	}
	if e != nil {
		return nil, "", nil, e
	}
	plan, e := currentManagementPlanTx(ctx, tx, project, input.BatchRef)
	if e != nil {
		return nil, "", nil, e
	}
	if plan.BaselineRef != input.BaselineRef {
		return nil, "", nil, apiError(409, "NETWORK_BASELINE_PLAN_MISMATCH", "Management plan must reference this approved baseline")
	}
	var raw []byte
	e = tx.QueryRowContext(ctx, "SELECT engineering_limits FROM network_baseline_versions WHERE id=$1 AND project_id=$2 AND state='approved'", plan.BaselineID, project).Scan(&raw)
	if e != nil {
		return nil, "", nil, e
	}
	var limits NetworkEngineeringLimits
	if e = json.Unmarshal(raw, &limits); e != nil {
		return nil, "", nil, e
	}
	catalog, e := newNetworkVariableCatalogTx(ctx, tx, project, template.Definition, lockCatalog)
	if e != nil {
		return nil, "", nil, e
	}
	devices := []NetworkPlannedDevice{}
	renderedBytes := 0
	for _, binding := range plan.Assignments {
		if binding.Batch != input.BatchRef {
			continue
		}
		variables, exists := limits.ConfigurationVariables[binding.Serial]
		if !exists {
			return nil, "", nil, apiError(409, "NETWORK_CONFIGURATION_BASELINE_REQUIRED", "Approved baseline must include configuration_variables for every target serial")
		}
		var d NetworkPlannedDevice
		d.ID = binding.SwitchID
		d.Serial = binding.Serial
		d.Hostname = binding.Hostname
		d.Address = binding.Address
		if e = tx.QueryRowContext(ctx, `SELECT coalesce(model_version_id,''),coalesce(credential_batch_ref,'') FROM bom_items WHERE item_uid=$1 AND status<>'retired'`, d.ID).Scan(&d.ModelVersionID, &d.CredentialBatchRef); e != nil {
			return nil, "", nil, e
		}
		device := map[string]any{"id": d.ID, "serial": d.Serial, "hostname": d.Hostname, "management_ip": d.Address, "gateway": binding.Gateway, "management_cidr": plan.Rules.CIDR, "model_version_id": d.ModelVersionID}
		approved, resolved, selections, err := catalog.resolve(ctx, tx, project, template.Definition, variables, d.ModelVersionID, lockCatalog)
		if err != nil {
			return nil, "", nil, err
		}
		d.ApprovedVariables = resolved
		d.CatalogSelections = selections
		d.Intended, e = template.Definition.RenderWithCatalog(ctx, variables, device, approved)
		if e != nil {
			return nil, "", nil, apiError(409, "NETWORK_RENDER_FAILED", e.Error())
		}
		if e = netbaseline.ValidateConfiguration(d.Intended); e != nil {
			return nil, "", nil, apiError(409, "NETWORK_CONFIGURATION_INVALID", e.Error())
		}
		var config struct {
			Hostname   string `json:"hostname"`
			Management struct {
				Address string `json:"address"`
				Gateway string `json:"gateway"`
			} `json:"management"`
		}
		if json.Unmarshal(d.Intended, &config) != nil || config.Hostname != d.Hostname || config.Management.Address != d.Address || config.Management.Gateway != binding.Gateway {
			return nil, "", nil, apiError(409, "NETWORK_INTENT_SOT_MISMATCH", "Rendered hostname and management address/gateway must match approved planning")
		}
		renderedBytes += len(d.Intended)
		if renderedBytes > 32<<20 {
			return nil, "", nil, apiError(409, "NETWORK_PLAN_RENDER_LIMIT", "Rendered configurations exceed 32MiB; use a smaller approved batch")
		}
		d.IntendedHash = netbaseline.ContentHash(d.Intended)
		d.IntendedID = netbaseline.ContentHash([]string{template.ID, d.ID, d.IntendedHash})[:32]
		devices = append(devices, d)
	}
	if len(devices) == 0 || len(devices) > 500 {
		return nil, "", nil, apiError(409, "NETWORK_PLAN_TARGET_LIMIT", "Network batch must contain 1 through 500 switches")
	}
	return template, plan.BaselineID, devices, nil
}
func (s *Store) TestNetworkTemplate(ctx context.Context, a *AuthSession, p *ProjectSummary, input NetworkPlanInput, trace string) (*NetworkTemplateVersion, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	if e = requireFabricPermission(ctx, tx, a, p.ID, "fabric:template:edit"); e != nil {
		tx.Rollback()
		return nil, e
	}
	template, baseline, devices, e := networkTemplateFactsTx(ctx, tx, p.ID, input, false)
	tx.Rollback()
	if e != nil {
		return nil, e
	}
	if template.State != "draft" {
		return nil, apiError(409, "NETWORK_TEMPLATE_NOT_DRAFT", "Create a new immutable template version before testing")
	}
	client, e := s.FabricProbes.NetworkClient()
	if e != nil {
		return nil, apiError(409, "NETWORK_ADAPTER_NOT_CONFIGURED", e.Error())
	}
	report := &NetworkTemplateReport{ID: RandomID(), BaselineID: baseline, BaselineRef: input.BaselineRef, BatchRef: input.BatchRef, FactsHash: netbaseline.ContentHash(devices), ConfigHash: s.FabricProbes.Fingerprint, CheckedAt: time.Now().UTC(), Devices: devices, Checks: []netbaseline.OperationResult{}}
	report.Checks = make([]netbaseline.OperationResult, len(devices))
	work, cancel := context.WithCancel(ctx)
	defer cancel()
	slots := make(chan struct{}, 16)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var first error
	for i, d := range devices {
		if work.Err() != nil {
			break
		}
		select {
		case slots <- struct{}{}:
		case <-work.Done():
			break
		}
		if work.Err() != nil {
			break
		}
		wg.Add(1)
		go func(i int, d NetworkPlannedDevice) {
			defer wg.Done()
			defer func() { <-slots }()
			result, err := client.Do(work, netbaseline.Operation{ID: RandomID(), Phase: "template-test", ProjectID: p.ID, DeviceID: d.ID, Serial: d.Serial, BaselineID: baseline, TemplateHash: template.ContentHash, Intended: d.Intended, ApprovedVariables: d.ApprovedVariables})
			if err == nil {
				err = netbaseline.RequireGates(result, "syntax", "semantic", "consistency", "rehearsal", "golden")
			}
			if err == nil && (len(result.Running) == 0 || result.RunningHash != d.IntendedHash) {
				err = errors.New("Golden test readback must match the rendered configuration")
			}
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if first == nil {
					first = err
					cancel()
				}
				return
			}
			if report.EvidenceSource != "" && report.EvidenceSource != result.EvidenceSource {
				if first == nil {
					first = errors.New("A test report cannot mix fixture and device evidence")
					cancel()
				}
				return
			}
			report.EvidenceSource = result.EvidenceSource
			result.Running = nil
			report.Checks[i] = *result
		}(i, d)
	}
	wg.Wait()
	if first != nil {
		return nil, apiError(409, "NETWORK_TEMPLATE_TEST_BLOCKED", first.Error())
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	tx, e = s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireFabricPermission(ctx, tx, a, p.ID, "fabric:template:edit"); e != nil {
		return nil, e
	}
	current, base, latest, e := networkTemplateFactsTx(ctx, tx, p.ID, input, true)
	if e != nil {
		return nil, e
	}
	if current.State != "draft" || current.ContentHash != template.ContentHash || base != baseline || netbaseline.ContentHash(latest) != report.FactsHash {
		return nil, apiError(409, "NETWORK_TEMPLATE_FACTS_CHANGED", "Template or approved device facts changed during testing")
	}
	for _, d := range devices {
		if _, e = tx.ExecContext(ctx, `INSERT INTO network_intended_configs(id,project_id,template_id,switch_id,content_hash,configuration,created_at) VALUES($1,$2,$3,$4,$5,$6,now()) ON CONFLICT(id) DO NOTHING`, d.IntendedID, p.ID, template.ID, d.ID, d.IntendedHash, []byte(d.Intended)); e != nil {
			return nil, e
		}
	}
	encoded, _ := json.Marshal(report)
	_, e = tx.ExecContext(ctx, "UPDATE network_template_versions SET state='tested',test_report=$2,updated_at=now() WHERE id=$1 AND state='draft'", template.ID, encoded)
	if e != nil {
		return nil, e
	}
	template.State = "tested"
	template.TestReport = report
	if e = appendAudit(ctx, tx, a.User.ID, p.ID, "network.template.test", template.ID, trace, map[string]any{"report_id": report.ID, "facts_hash": report.FactsHash, "source": report.EvidenceSource}); e != nil {
		return nil, e
	}
	return template, tx.Commit()
}
func (s *Store) ReleaseNetworkTemplate(ctx context.Context, a *AuthSession, p *ProjectSummary, id, trace string) (*NetworkTemplateVersion, error) {
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	role, e := networkRoleTx(ctx, tx, a, p.ID)
	if e != nil {
		return nil, e
	}
	if role != "Admin" && role != "NH" {
		return nil, ErrForbidden
	}
	template, e := scanNetworkTemplate(tx.QueryRowContext(ctx, "SELECT "+networkTemplateFields+" FROM network_template_versions WHERE id=$1 AND project_id=$2 FOR UPDATE", id, p.ID))
	if errors.Is(e, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if e != nil {
		return nil, e
	}
	if template.CreatedBy == a.User.ID {
		return nil, apiError(409, "NETWORK_TEMPLATE_SELF_APPROVAL", "An independent network reviewer must release this template")
	}
	if template.State != "tested" || template.TestReport == nil {
		return nil, apiError(409, "NETWORK_TEMPLATE_TEST_REQUIRED", "A complete tested template is required")
	}
	report := template.TestReport
	_, baseline, devices, e := networkTemplateFactsTx(ctx, tx, p.ID, NetworkPlanInput{TemplateID: id, BaselineRef: report.BaselineRef, BatchRef: report.BatchRef}, true)
	if e != nil {
		return nil, e
	}
	if baseline != report.BaselineID || netbaseline.ContentHash(devices) != report.FactsHash || s.FabricProbes == nil || report.ConfigHash != s.FabricProbes.Fingerprint {
		return nil, apiError(409, "NETWORK_TEMPLATE_TEST_STALE", "Approved facts or adapter changed; test a new template version")
	}
	_, e = tx.ExecContext(ctx, "UPDATE network_template_versions SET state='released',released_by=$2,updated_at=now() WHERE id=$1", id, a.User.ID)
	if e != nil {
		return nil, e
	}
	queued, e := queueNetworkTemplateChangesTx(ctx, tx, template, a.User.ID)
	if e != nil {
		return nil, e
	}
	if e = appendAudit(ctx, tx, a.User.ID, p.ID, "network.template.release", id, trace, map[string]any{"report_id": report.ID, "content_hash": template.ContentHash, "queued_assessments": queued}); e != nil {
		return nil, e
	}
	template.State = "released"
	return template, tx.Commit()
}
func (s *Store) ListNetworkTemplates(ctx context.Context, a *AuthSession, p *ProjectSummary) ([]NetworkTemplateVersion, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireFabricPermission(ctx, tx, a, p.ID, "fabric:read"); e != nil {
		return nil, e
	}
	rows, e := tx.QueryContext(ctx, "SELECT "+networkTemplateFields+" FROM network_template_versions WHERE project_id=$1 ORDER BY created_at DESC,id DESC LIMIT 50", p.ID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []NetworkTemplateVersion{}
	for rows.Next() {
		v, e := scanNetworkTemplate(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

// DeprecateNetworkTemplate preserves immutable content and all references. Existing
// PushJob admission independently requires a released template before dispatch.
type NetworkTemplateDeprecation struct {
	ExpectedHash string `json:"expected_hash"`
	Reason       string `json:"reason"`
}

func (s *Store) DeprecateNetworkTemplate(ctx context.Context, a *AuthSession, p *ProjectSummary, id string, input NetworkTemplateDeprecation, trace string) (*NetworkTemplateVersion, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	if !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(input.ExpectedHash) || input.Reason == "" || len([]rune(input.Reason)) > 1000 {
		return nil, apiError(400, "NETWORK_TEMPLATE_DEPRECATION_INVALID", "The reviewed content hash and a reason of 1 through 1000 characters are required")
	}
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	role, e := networkRoleTx(ctx, tx, a, p.ID)
	if e != nil {
		return nil, e
	}
	if role != "Admin" && role != "NH" {
		return nil, ErrForbidden
	}
	template, e := scanNetworkTemplate(tx.QueryRowContext(ctx, "SELECT "+networkTemplateFields+" FROM network_template_versions WHERE id=$1 AND project_id=$2 FOR UPDATE", id, p.ID))
	if errors.Is(e, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if e != nil {
		return nil, e
	}
	if input.ExpectedHash != template.ContentHash {
		return nil, apiError(409, "NETWORK_TEMPLATE_FACTS_CHANGED", "The reviewed template hash does not match this version")
	}
	if template.State == "deprecated" {
		return template, tx.Commit()
	}
	if template.State != "released" {
		return nil, apiError(409, "NETWORK_TEMPLATE_NOT_RELEASED", "Only a released template can be deprecated")
	}
	if _, e = tx.ExecContext(ctx, "UPDATE network_template_versions SET state='deprecated',updated_at=now() WHERE id=$1", id); e != nil {
		return nil, e
	}
	if e = appendAudit(ctx, tx, a.User.ID, p.ID, "network.template.deprecate", id, trace, map[string]any{"version": template.Version, "content_hash": template.ContentHash, "reason": input.Reason}); e != nil {
		return nil, e
	}
	template.State = "deprecated"
	return template, tx.Commit()
}
