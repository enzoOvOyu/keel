package platform

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/enzoOvOyu/keel/internal/fabricboot"
	"github.com/enzoOvOyu/keel/internal/netbaseline"
)

// Authored for one P2 milestone run in the disposable database. No hardware,
// schema reset or production adapter is used by this case.
func TestExtendedAddressPlanningApprovalAndFrozenTemplateFactsPostgres(t *testing.T) {
	s, root, editor, reviewer, p := templateLineageFixture(t)
	ctx := context.Background()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, e := s.DB.ExecContext(ctx, query, args...); e != nil {
			t.Fatal(e)
		}
	}
	pkg, profile, a, b, pa, pb, cable := RandomID(), RandomID(), RandomID(), RandomID(), RandomID(), RandomID(), RandomID()
	if pa > pb {
		pa, pb = pb, pa
	}
	suffix := p.ID[:12]
	serialA, serialB := "ADDRESS-A-"+suffix, "ADDRESS-B-"+suffix
	exec(`INSERT INTO bom_mapping_profiles(id,project_id,version_ref,state,created_by,created_at) VALUES($1,$2,'address-v1','approved',$3,now())`, profile, p.ID, root.User.ID)
	exec(`INSERT INTO bom_import_packages(id,project_id,bom_import_batch_id,schema_version,profile_version_ref,content_hash,declared_by,manifest,row_count,state,uploaded_by,committed_at) VALUES($1,$2,'address-fixture','bom-import/v1','address-v1','sha256:'||repeat('a',64),'design_institute','{}',5,'committed',$3,now())`, pkg, p.ID, root.User.ID)
	for i, d := range []struct{ id, serial string }{{a, serialA}, {b, serialB}} {
		exec(`INSERT INTO bom_items(item_uid,category,serial,position,pod,role,source_of_record,first_project_id,created_at,updated_at) VALUES($1,'switch',$2,jsonb_build_object('dc','dc1','rack','r1','idx',$3::text),'p1','leaf','design_bom',$4,now(),now())`, d.id, d.serial, fmt.Sprint(i+1), p.ID)
		exec(`INSERT INTO switches(id,serial_number,hostname,created_at,updated_at) VALUES($1,$2,$2,now(),now())`, d.id, d.serial)
	}
	for _, port := range []struct{ id, owner string }{{pa, a}, {pb, b}} {
		exec(`INSERT INTO bom_items(item_uid,category,parent_uid,port_index,port_name,source_of_record,first_project_id,created_at,updated_at) VALUES($1,'port',$2,'1','swp1','design_bom',$3,now(),now())`, port.id, port.owner, p.ID)
		exec(`INSERT INTO physical_ports(id,switch_id,parent_uid,port_index,port_name,created_at) VALUES($1,$2,$2,'1','swp1',now())`, port.id, port.owner)
	}
	exec(`INSERT INTO bom_items(item_uid,category,endpoint_a_uid,endpoint_b_uid,source_of_record,first_project_id,created_at,updated_at) VALUES($1,'cable',$2,$3,'design_bom',$4,now(),now())`, cable, pa, pb, p.ID)
	for i, id := range []string{a, b, pa, pb, cable} {
		exec(`INSERT INTO bom_item_revisions(package_id,item_uid,bom_line_no,project_id,row_snapshot,recorded_at) VALUES($1,$2,$3,$4,'{}',now())`, pkg, id, i+1, p.ID)
	}
	limits := NetworkEngineeringLimits{
		SwitchBandwidthRatioLimits: map[string]float64{"role:leaf": 2},
		Management:                 &fabricboot.ManagementRules{CIDR: "192.0.2.0/24", FirstAddress: "192.0.2.10", Gateway: "192.0.2.1", BatchPrefix: "address", BatchBy: "rack"},
		ConfigurationVariables:     map[string]map[string]any{serialA: {}, serialB: {}},
		AddressPlanning:            &netbaseline.AddressPlanning{ManagementVRF: "mgmt", Pools: []netbaseline.AddressPool{{ID: "lo", Scope: "loopback", CIDR: "198.51.100.0/24", UnitPrefix: 32, VRF: "default"}, {ID: "links", Scope: "p2p", CIDR: "203.0.113.0/24", UnitPrefix: 31, VRF: "default"}}, Rules: []netbaseline.AddressRule{{ObjectUID: a, PoolRef: "lo", UnitIndex: 10}, {ObjectUID: b, PoolRef: "lo", UnitIndex: 11}, {ObjectUID: pa, PoolRef: "links", UnitIndex: 1, PeerUID: pb}, {ObjectUID: pb, PoolRef: "links", UnitIndex: 1, HostOffset: 1, PeerUID: pa}}},
	}
	if _, e := s.CreateNetworkBaseline(ctx, root, p, CreateNetworkBaselineInput{VersionRef: "address-v1", SourceRef: "fixture:approved-addresses", EngineeringLimits: limits}, "test"); e != nil {
		t.Fatal(e)
	}
	if _, e := s.ApproveNetworkBaseline(ctx, root, p, "address-v1", ApproveNetworkBaselineInput{CustomerApprovalRef: "fixture:customer", NetworkApprovalRef: "fixture:engineer"}, "test"); e != nil {
		t.Fatal(e)
	}
	mgmt, e := s.PreviewZTPManagementPlan(ctx, root, p, ZTPManagementPlanInput{BaselineRef: "address-v1"})
	if e != nil {
		t.Fatal(e)
	}
	mgmt, e = s.CommitZTPManagementPlan(ctx, root, p, ZTPManagementPlanInput{BaselineRef: "address-v1", ExpectedHash: mgmt.ContentHash}, "test")
	if e != nil {
		t.Fatal(e)
	}
	template, e := s.CreateNetworkTemplate(ctx, editor, p, NetworkTemplateInput{Name: "approved addresses", SourceRef: "fixture:address-aware", Definition: netbaseline.Template{Source: `{"hostname":{{device.hostname|keeljson}},"management":{"address":{{device.management_ip|keeljson}},"gateway":{{device.gateway|keeljson}}},"network_addresses":{{device.address_plan|keeljson}}}`, Variables: map[string]netbaseline.Variable{}}}, "test")
	if e != nil {
		t.Fatal(e)
	}
	facts := func() ([]NetworkPlannedDevice, error) {
		t.Helper()
		tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		_, _, devices, err := networkTemplateFactsTx(ctx, tx, p.ID, NetworkPlanInput{TemplateID: template.ID, BaselineRef: "address-v1", BatchRef: mgmt.Assignments[0].Batch}, false)
		return devices, err
	}
	_, e = facts()
	assertErrorCode(t, e, "NETWORK_ADDRESS_APPROVAL_REQUIRED")
	input := NetworkAddressPlanInput{BaselineRef: "address-v1", Overrides: []netbaseline.AddressOverride{{ObjectUID: a, PoolRef: "lo", Address: "198.51.100.30"}}, Reason: "Customer-approved loopback adjustment"}
	preview, e := s.PreviewNetworkAddressPlan(ctx, root, p, input)
	if e != nil {
		t.Fatal(e)
	}
	if len(preview.Assignments) != 4 || preview.ID != "" {
		t.Fatalf("bad preview: %+v", preview)
	}
	var count int
	if e = s.DB.QueryRowContext(ctx, "SELECT count(*) FROM network_address_plans WHERE project_id=$1", p.ID).Scan(&count); e != nil || count != 0 {
		t.Fatal("preview wrote a plan", e)
	}
	input.ExpectedHash = strings.Repeat("0", 64)
	_, e = s.SaveNetworkAddressPlan(ctx, root, p, input, "test")
	assertErrorCode(t, e, "NETWORK_ADDRESS_PREVIEW_STALE")
	input.ExpectedHash = preview.ContentHash
	plan, e := s.SaveNetworkAddressPlan(ctx, root, p, input, "test")
	if e != nil {
		t.Fatal(e)
	}
	repeated, e := s.SaveNetworkAddressPlan(ctx, root, p, input, "test")
	if e != nil || repeated.ID != plan.ID {
		t.Fatal("save lost idempotency", e)
	}
	approval := NetworkAddressPlanApproval{ExpectedHash: plan.ContentHash, Reason: "Reviewed endpoints, pool and adjustment"}
	_, e = s.ApproveNetworkAddressPlan(ctx, root, p, plan.ID, approval, "test")
	assertErrorCode(t, e, "NETWORK_ADDRESS_SELF_APPROVAL")
	_, e = s.ApproveNetworkAddressPlan(ctx, editor, p, plan.ID, approval, "test")
	assertErrorCode(t, e, "FORBIDDEN")
	stale := approval
	stale.ExpectedHash = strings.Repeat("0", 64)
	_, e = s.ApproveNetworkAddressPlan(ctx, reviewer, p, plan.ID, stale, "test")
	assertErrorCode(t, e, "NETWORK_ADDRESS_PREVIEW_STALE")
	approved, e := s.ApproveNetworkAddressPlan(ctx, reviewer, p, plan.ID, approval, "test")
	if e != nil {
		t.Fatal(e)
	}
	if approved.State != "approved" || approved.ApprovedBy != reviewer.User.ID || !approved.Current {
		t.Fatal("approval identity lost")
	}
	first, e := facts()
	if e != nil {
		t.Fatal(e)
	}
	if len(first) != 2 {
		t.Fatal("wrong batch size")
	}
	for _, d := range first {
		if d.AddressPlanID != plan.ID || len(d.Addresses) != 2 {
			t.Fatal("unfrozen addresses")
		}
		for _, address := range d.Addresses {
			if address.OwnerSwitchID != d.ID || address.Serial != d.Serial {
				t.Fatal("address bound to wrong switch")
			}
			if address.Scope == "p2p" && address.PortName != "swp1" {
				t.Fatal("lost stable port name")
			}
		}
	}
	input.Overrides[0].Address = "198.51.100.40"
	input.Reason = "Second approved adjustment"
	preview, e = s.PreviewNetworkAddressPlan(ctx, root, p, input)
	if e != nil {
		t.Fatal(e)
	}
	input.ExpectedHash = preview.ContentHash
	second, e := s.SaveNetworkAddressPlan(ctx, root, p, input, "test")
	if e != nil {
		t.Fatal(e)
	}
	// An unapproved draft cannot silently replace approved production facts.
	pending, e := facts()
	if e != nil || netbaseline.ContentHash(pending) != netbaseline.ContentHash(first) {
		t.Fatal("draft changed approved facts", e)
	}
	approval.ExpectedHash = second.ContentHash
	if _, e = s.ApproveNetworkAddressPlan(ctx, reviewer, p, second.ID, approval, "test"); e != nil {
		t.Fatal(e)
	}
	latest, e := facts()
	if e != nil {
		t.Fatal(e)
	}
	if netbaseline.ContentHash(first) == netbaseline.ContentHash(latest) {
		t.Fatal("new approval did not invalidate previous template/job facts")
	}
	for _, table := range []string{"network_address_plans", "network_address_plan_approvals"} {
		column := "id"
		if table == "network_address_plan_approvals" {
			column = "plan_id"
		}
		if _, e = s.DB.ExecContext(ctx, "DELETE FROM "+table+" WHERE "+column+"=$1", plan.ID); e == nil {
			t.Fatal("immutable planning evidence deleted")
		}
	}
	foreign, e := s.CreateProject(ctx, root, CreateProjectInput{Name: "Other address scope", Slug: "other-address-" + RandomID()[:12]}, RandomID(), "test")
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.GetNetworkAddressPlan(ctx, root, foreign, plan.ID)
	if e != ErrNotFound {
		t.Fatal("cross-project plan visible", e)
	}
	// Mutating a live endpoint invalidates approval without changing historical proof.
	exec(`UPDATE bom_items SET port_name='swp2',updated_at=now() WHERE item_uid=$1`, pa)
	_, e = facts()
	assertErrorCode(t, e, "NETWORK_ADDRESS_PREVIEW_STALE")
	history, e := s.GetNetworkAddressPlan(ctx, root, p, plan.ID)
	if e != nil || history.Current || history.ContentHash != plan.ContentHash {
		t.Fatal("historical proof changed or stale approval stayed current", e)
	}
}
