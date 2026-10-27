package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/enzoOvOyu/keel/internal/netbaseline"
)

type NetworkAddressPlanInput struct {
	BaselineRef  string                        `json:"baseline_ref"`
	Overrides    []netbaseline.AddressOverride `json:"overrides,omitempty"`
	Reason       string                        `json:"reason"`
	ExpectedHash string                        `json:"expected_hash,omitempty"`
}
type NetworkAddressPlan struct {
	ID               string                          `json:"id,omitempty"`
	ProjectID        string                          `json:"project_id"`
	BaselineID       string                          `json:"baseline_id"`
	BaselineRef      string                          `json:"baseline_ref"`
	ManagementPlanID string                          `json:"management_plan_id"`
	PackageID        string                          `json:"package_id"`
	State            string                          `json:"state"`
	Input            NetworkAddressPlanInput         `json:"input"`
	SourceHash       string                          `json:"source_hash"`
	ContentHash      string                          `json:"content_hash"`
	Assignments      []netbaseline.AddressAssignment `json:"assignments"`
	CreatedBy        string                          `json:"created_by,omitempty"`
	CreatedAt        *time.Time                      `json:"created_at,omitempty"`
	ApprovedBy       string                          `json:"approved_by,omitempty"`
	ApprovalReason   string                          `json:"approval_reason,omitempty"`
	ApprovedAt       *time.Time                      `json:"approved_at,omitempty"`
	Current          bool                            `json:"current"`
}
type NetworkAddressPlanSummary struct {
	ID              string     `json:"id"`
	BaselineRef     string     `json:"baseline_ref"`
	State           string     `json:"state"`
	Current         bool       `json:"current"`
	AssignmentCount int        `json:"assignment_count"`
	AdjustmentCount int        `json:"adjustment_count"`
	ContentHash     string     `json:"content_hash"`
	CreatedBy       string     `json:"created_by"`
	CreatedAt       *time.Time `json:"created_at"`
	ApprovedBy      string     `json:"approved_by,omitempty"`
	ApprovedAt      *time.Time `json:"approved_at,omitempty"`
}
type NetworkAddressPlanApproval struct {
	ExpectedHash string `json:"expected_hash"`
	Reason       string `json:"reason"`
}

func networkAddressPlanPreviewTx(ctx context.Context, tx *sql.Tx, project string, input NetworkAddressPlanInput) (*NetworkAddressPlan, error) {
	input.Overrides = append([]netbaseline.AddressOverride(nil), input.Overrides...)
	sort.Slice(input.Overrides, func(i, j int) bool {
		a, b := input.Overrides[i], input.Overrides[j]
		if a.PoolRef != b.PoolRef {
			return a.PoolRef < b.PoolRef
		}
		return a.ObjectUID < b.ObjectUID
	})
	input.ExpectedHash = ""
	input.Reason = strings.TrimSpace(input.Reason)
	if !bomProfileVersionPattern.MatchString(input.BaselineRef) || len([]rune(input.Reason)) > 1000 || (len(input.Overrides) > 0 && input.Reason == "") {
		return nil, apiError(400, "NETWORK_ADDRESS_PLAN_INVALID", "Select an approved baseline and provide a reason for manual adjustments")
	}
	var baseline string
	var raw []byte
	e := tx.QueryRowContext(ctx, `SELECT id,engineering_limits FROM network_baseline_versions WHERE project_id=$1 AND version_ref=$2 AND state='approved'`, project, input.BaselineRef).Scan(&baseline, &raw)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, apiError(409, "NETWORK_BASELINE_NOT_APPROVED", "An approved network baseline is required")
	}
	if e != nil {
		return nil, e
	}
	var limits NetworkEngineeringLimits
	if e = json.Unmarshal(raw, &limits); e != nil {
		return nil, e
	}
	if limits.AddressPlanning == nil {
		return nil, apiError(409, "NETWORK_ADDRESS_RULES_REQUIRED", "Approved baseline must include explicit address_planning rules")
	}
	assignments, e := netbaseline.BuildAddressPlan(*limits.AddressPlanning, input.Overrides)
	if e != nil {
		return nil, apiError(409, "NETWORK_ADDRESS_PLAN_BLOCKED", e.Error())
	}
	if e = tx.QueryRowContext(ctx, `SELECT report FROM ztp_management_plans WHERE project_id=$1 ORDER BY created_at DESC,id DESC LIMIT 1`, project).Scan(&raw); errors.Is(e, sql.ErrNoRows) {
		return nil, apiError(409, "ZTP_BATCH_PLAN_REQUIRED", "Save the minimal management plan first")
	}
	if e != nil {
		return nil, e
	}
	var saved ZTPManagementPlan
	if e = json.Unmarshal(raw, &saved); e != nil {
		return nil, e
	}
	if len(saved.Assignments) == 0 {
		return nil, apiError(409, "ZTP_BATCH_PLAN_REQUIRED", "Current management plan has no assignments")
	}
	management, e := currentManagementPlanTx(ctx, tx, project, saved.Assignments[0].Batch)
	if e != nil {
		return nil, e
	}
	if management.BaselineID != baseline {
		return nil, apiError(409, "NETWORK_BASELINE_PLAN_MISMATCH", "Address and management planning must share the approved baseline")
	}
	pkg, _, cables, e := fabricInventoryTx(ctx, tx, project)
	if e != nil {
		return nil, e
	}
	type object struct {
		UID, Kind, Owner, Serial, PortName string
		PortStable                         bool
	}
	objects := map[string]object{}
	rows, e := tx.QueryContext(ctx, `SELECT b.item_uid,b.category,coalesce(b.parent_uid,''),coalesce(b.serial,''),coalesce(nullif(b.port_name,''),b.port_index,''),EXISTS(SELECT 1 FROM physical_ports port WHERE port.id=b.item_uid AND port.switch_id=b.parent_uid) FROM bom_item_revisions r JOIN bom_items b ON b.item_uid=r.item_uid WHERE r.package_id=$1 AND b.category IN('switch','port') AND b.status<>'retired' ORDER BY b.item_uid`, pkg)
	if e != nil {
		return nil, e
	}
	for rows.Next() {
		var o object
		if e = rows.Scan(&o.UID, &o.Kind, &o.Owner, &o.Serial, &o.PortName, &o.PortStable); e != nil {
			rows.Close()
			return nil, e
		}
		if o.Kind == "switch" {
			o.Owner = o.UID
		}
		objects[o.UID] = o
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	batches := map[string]struct{ Serial, Batch string }{}
	managementIPs := map[string]string{}
	for _, a := range management.Assignments {
		batches[a.SwitchID] = struct{ Serial, Batch string }{a.Serial, a.Batch}
		managementIPs[a.Address] = a.SwitchID
	}
	physical := map[string]string{}
	for _, cable := range cables {
		physical[cable.A.UID+"/"+cable.B.UID] = cable.UID
		physical[cable.B.UID+"/"+cable.A.UID] = cable.UID
	}
	for i := range assignments {
		a := &assignments[i]
		o, ok := objects[a.ObjectUID]
		owner, ownerExists := objects[o.Owner]
		bound, boundExists := batches[o.Owner]
		if !ok || !ownerExists || owner.Kind != "switch" || (o.Kind == "port" && !o.PortStable) || !boundExists || bound.Serial != owner.Serial || (a.Scope == "loopback" && o.Kind != "switch") || (a.Scope == "p2p" && (o.Kind != "port" || o.PortName == "" || physical[a.ObjectUID+"/"+a.PeerUID] == "")) {
			return nil, apiError(409, "NETWORK_ADDRESS_OBJECT_UNMODELED", "Allocation target/peer must match current modeled switch or connected switch ports and management identity")
		}
		a.PortName = o.PortName
		a.OwnerSwitchID = o.Owner
		a.Serial = owner.Serial
		a.BatchRef = bound.Batch
		if a.VRF == limits.AddressPlanning.ManagementVRF {
			if device, exists := managementIPs[a.Address]; exists && (device != o.Owner || a.Scope != "bgp_peer") {
				return nil, apiError(409, "NETWORK_ADDRESS_MANAGEMENT_CONFLICT", "Allocation conflicts with the approved management address in this VRF")
			}
			if limits.Management != nil && a.Address == limits.Management.Gateway {
				return nil, apiError(409, "NETWORK_ADDRESS_MANAGEMENT_CONFLICT", "Allocation conflicts with the approved management gateway")
			}
		}
	}
	out := &NetworkAddressPlan{ProjectID: project, BaselineID: baseline, BaselineRef: input.BaselineRef, ManagementPlanID: management.ID, PackageID: pkg, State: "preview", Input: input, Assignments: assignments, Current: true}
	out.SourceHash = netbaseline.ContentHash(struct {
		Baseline, Package, ManagementHash string
		Objects                           map[string]object
		Cables                            map[string]string
	}{baseline, pkg, management.ContentHash, objects, physical})
	out.ContentHash = netbaseline.ContentHash(struct {
		Project, Source string
		Input           NetworkAddressPlanInput
		Assignments     []netbaseline.AddressAssignment
	}{project, out.SourceHash, input, assignments})
	return out, nil
}
func scanNetworkAddressPlan(row scanner) (*NetworkAddressPlan, error) {
	var out NetworkAddressPlan
	var raw []byte
	e := row.Scan(&out.ID, &raw, &out.CreatedBy, &out.CreatedAt, &out.ApprovedBy, &out.ApprovedAt, &out.ApprovalReason)
	if e != nil {
		return nil, e
	}
	id, creator, created, approver, approved, reason := out.ID, out.CreatedBy, out.CreatedAt, out.ApprovedBy, out.ApprovedAt, out.ApprovalReason
	if e = json.Unmarshal(raw, &out); e != nil {
		return nil, e
	}
	out.ID = id
	out.CreatedBy = creator
	out.CreatedAt = created
	out.ApprovedBy = approver
	out.ApprovedAt = approved
	out.ApprovalReason = reason
	out.State = "draft"
	out.Current = false
	if approved != nil {
		out.State = "approved"
	}
	return &out, nil
}

const addressPlanReadFields = `p.id,p.report,p.created_by,p.created_at,coalesce(a.approved_by,''),a.approved_at,coalesce(a.reason,'')`

func loadNetworkAddressPlanTx(ctx context.Context, tx *sql.Tx, project, id string) (*NetworkAddressPlan, error) {
	v, e := scanNetworkAddressPlan(tx.QueryRowContext(ctx, "SELECT "+addressPlanReadFields+" FROM network_address_plans p LEFT JOIN network_address_plan_approvals a ON a.plan_id=p.id WHERE p.project_id=$1 AND p.id=$2", project, id))
	if errors.Is(e, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return v, e
}
func (s *Store) PreviewNetworkAddressPlan(ctx context.Context, a *AuthSession, p *ProjectSummary, input NetworkAddressPlanInput) (*NetworkAddressPlan, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireFabricPermission(ctx, tx, a, p.ID, "fabric:read"); e != nil {
		return nil, e
	}
	return networkAddressPlanPreviewTx(ctx, tx, p.ID, input)
}
func (s *Store) SaveNetworkAddressPlan(ctx context.Context, a *AuthSession, p *ProjectSummary, input NetworkAddressPlanInput, trace string) (*NetworkAddressPlan, error) {
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireFabricPermission(ctx, tx, a, p.ID, "fabric:plan"); e != nil {
		return nil, e
	}
	v, e := networkAddressPlanPreviewTx(ctx, tx, p.ID, input)
	if e != nil {
		return nil, e
	}
	if input.ExpectedHash != v.ContentHash {
		return nil, apiError(409, "NETWORK_ADDRESS_PREVIEW_STALE", "Approved allocation facts changed; preview again")
	}
	var existing string
	e = tx.QueryRowContext(ctx, `SELECT id FROM network_address_plans WHERE project_id=$1 AND content_hash=$2`, p.ID, v.ContentHash).Scan(&existing)
	if e == nil {
		old, err := loadNetworkAddressPlanTx(ctx, tx, p.ID, existing)
		if err != nil {
			return nil, err
		}
		old.Current = true
		return old, tx.Commit()
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return nil, e
	}
	v.ID = RandomID()
	v.State = "draft"
	v.CreatedBy = a.User.ID
	now := time.Now().UTC()
	v.CreatedAt = &now
	_, e = tx.ExecContext(ctx, `INSERT INTO network_address_plans(id,project_id,baseline_id,management_plan_id,package_id,input,report,content_hash,created_by,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, v.ID, p.ID, v.BaselineID, v.ManagementPlanID, v.PackageID, networkJSON(v.Input), networkJSON(v), v.ContentHash, a.User.ID, now)
	if e != nil {
		return nil, e
	}
	if e = appendAudit(ctx, tx, a.User.ID, p.ID, "network.address.plan", v.ID, trace, map[string]any{"content_hash": v.ContentHash, "assignments": len(v.Assignments), "manual_adjustments": len(v.Input.Overrides), "reason": v.Input.Reason}); e != nil {
		return nil, e
	}
	return v, tx.Commit()
}
func (s *Store) ApproveNetworkAddressPlan(ctx context.Context, a *AuthSession, p *ProjectSummary, id string, input NetworkAddressPlanApproval, trace string) (*NetworkAddressPlan, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	if input.Reason == "" || len([]rune(input.Reason)) > 1000 {
		return nil, apiError(400, "NETWORK_ADDRESS_APPROVAL_INVALID", "A review reason is required")
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
	v, e := loadNetworkAddressPlanTx(ctx, tx, p.ID, id)
	if e != nil {
		return nil, e
	}
	if v.CreatedBy == a.User.ID {
		return nil, apiError(409, "NETWORK_ADDRESS_SELF_APPROVAL", "An independent network reviewer must approve the allocation")
	}
	current, e := networkAddressPlanPreviewTx(ctx, tx, p.ID, v.Input)
	if e != nil {
		return nil, e
	}
	if current.ContentHash != v.ContentHash || input.ExpectedHash != v.ContentHash {
		return nil, apiError(409, "NETWORK_ADDRESS_PREVIEW_STALE", "Allocation facts changed; create a fresh plan")
	}
	if v.State == "approved" {
		v.Current = true
		return v, tx.Commit()
	}
	now := time.Now().UTC()
	_, e = tx.ExecContext(ctx, `INSERT INTO network_address_plan_approvals(plan_id,approved_by,authority,content_hash,reason,approved_at) VALUES($1,$2,$3,$4,$5,$6)`, id, a.User.ID, role, v.ContentHash, input.Reason, now)
	if e != nil {
		return nil, e
	}
	if e = appendAudit(ctx, tx, a.User.ID, p.ID, "network.address.approve", id, trace, map[string]any{"content_hash": v.ContentHash, "reason": input.Reason, "authority": role}); e != nil {
		return nil, e
	}
	v.State = "approved"
	v.ApprovedBy = a.User.ID
	v.ApprovedAt = &now
	v.ApprovalReason = input.Reason
	v.Current = true
	return v, tx.Commit()
}
func (s *Store) ListNetworkAddressPlans(ctx context.Context, a *AuthSession, p *ProjectSummary) ([]NetworkAddressPlanSummary, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireFabricPermission(ctx, tx, a, p.ID, "fabric:read"); e != nil {
		return nil, e
	}
	rows, e := tx.QueryContext(ctx, "SELECT "+addressPlanReadFields+" FROM network_address_plans p LEFT JOIN network_address_plan_approvals a ON a.plan_id=p.id WHERE p.project_id=$1 ORDER BY p.created_at DESC,p.id DESC LIMIT 30", p.ID)
	if e != nil {
		return nil, e
	}
	out := []NetworkAddressPlan{}
	for rows.Next() {
		v, err := scanNetworkAddressPlan(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, *v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	for i := range out {
		v := &out[i]
		current, err := networkAddressPlanPreviewTx(ctx, tx, p.ID, v.Input)
		if err != nil {
			var business *Error
			if !errors.As(err, &business) {
				return nil, err
			}
			continue
		}
		v.Current = current.ContentHash == v.ContentHash
	}
	summaries := []NetworkAddressPlanSummary{}
	for _, v := range out {
		summaries = append(summaries, NetworkAddressPlanSummary{ID: v.ID, BaselineRef: v.BaselineRef, State: v.State, Current: v.Current, AssignmentCount: len(v.Assignments), AdjustmentCount: len(v.Input.Overrides), ContentHash: v.ContentHash, CreatedBy: v.CreatedBy, CreatedAt: v.CreatedAt, ApprovedBy: v.ApprovedBy, ApprovedAt: v.ApprovedAt})
	}
	return summaries, nil
}
func currentNetworkAddressPlanTx(ctx context.Context, tx *sql.Tx, project, baseline, management string) (*NetworkAddressPlan, error) {
	v, e := scanNetworkAddressPlan(tx.QueryRowContext(ctx, "SELECT "+addressPlanReadFields+" FROM network_address_plans p JOIN network_address_plan_approvals a ON a.plan_id=p.id WHERE p.project_id=$1 AND p.baseline_id=$2 AND p.management_plan_id=$3 ORDER BY a.approved_at DESC,p.id DESC LIMIT 1", project, baseline, management))
	if errors.Is(e, sql.ErrNoRows) {
		return nil, apiError(409, "NETWORK_ADDRESS_APPROVAL_REQUIRED", "Approve the extended address plan before rendering production templates")
	}
	if e != nil {
		return nil, e
	}
	current, e := networkAddressPlanPreviewTx(ctx, tx, project, v.Input)
	if e != nil {
		return nil, e
	}
	if current.ContentHash != v.ContentHash {
		return nil, apiError(409, "NETWORK_ADDRESS_PREVIEW_STALE", "Approved address planning no longer matches the current BOM and management plan")
	}
	v.Current = true
	return v, nil
}

func (s *Store) GetNetworkAddressPlan(ctx context.Context, a *AuthSession, p *ProjectSummary, id string) (*NetworkAddressPlan, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireFabricPermission(ctx, tx, a, p.ID, "fabric:read"); e != nil {
		return nil, e
	}
	v, e := loadNetworkAddressPlanTx(ctx, tx, p.ID, id)
	if e != nil {
		return nil, e
	}
	current, e := networkAddressPlanPreviewTx(ctx, tx, p.ID, v.Input)
	if e != nil {
		var business *Error
		if !errors.As(e, &business) {
			return nil, e
		}
	} else {
		v.Current = current.ContentHash == v.ContentHash
	}
	return v, nil
}
