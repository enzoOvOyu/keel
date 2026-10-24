package platform

import (
	"context"
	"github.com/enzoOvOyu/keel/internal/fabricboot"
	"strings"
	"testing"
	"time"
)

func TestZTPManagementPlanTransactions(t *testing.T) {
	s := integrationStore(t)
	ctx := context.Background()
	password := "isolated-management-password"
	root, e := s.CreateUser(ctx, "management-root", "Root", password, true, true)
	if e != nil {
		t.Fatal(e)
	}
	_, actor := testLogin(t, s, root.Username, password, "198.51.100.91")
	p, e := s.CreateProject(ctx, actor, CreateProjectInput{Name: "Management", Slug: "management"}, "management", "test")
	if e != nil {
		t.Fatal(e)
	}
	rules := &fabricboot.ManagementRules{CIDR: "192.0.2.0/24", FirstAddress: "192.0.2.10", Gateway: "192.0.2.1", BatchPrefix: "approved", BatchBy: "rack"}
	_, e = s.CreateNetworkBaseline(ctx, actor, p, CreateNetworkBaselineInput{VersionRef: "agreed-v1", SourceRef: "customer-agreed-plan", EngineeringLimits: NetworkEngineeringLimits{SwitchBandwidthRatioLimits: map[string]float64{"role:leaf": 2}, Management: rules}}, "test")
	if e != nil {
		t.Fatal(e)
	}
	input := ZTPManagementPlanInput{BaselineRef: "agreed-v1"}
	_, e = s.PreviewZTPManagementPlan(ctx, actor, p, input)
	assertErrorCode(t, e, "ZTP_BASELINE_NOT_APPROVED")
	_, e = s.ApproveNetworkBaseline(ctx, actor, p, "agreed-v1", ApproveNetworkBaselineInput{CustomerApprovalRef: "customer-signature-ref", NetworkApprovalRef: "engineer-signature-ref"}, "test")
	if e != nil {
		t.Fatal(e)
	}
	profile, pkg := RandomID(), RandomID()
	now := time.Now().UTC()
	_, e = s.DB.ExecContext(ctx, `INSERT INTO bom_mapping_profiles(id,project_id,version_ref,state,created_by,created_at) VALUES($1,$2,'mapping-v1','approved',$3,$4)`, profile, p.ID, root.ID, now)
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.DB.ExecContext(ctx, `INSERT INTO bom_import_packages(id,project_id,bom_import_batch_id,schema_version,profile_version_ref,content_hash,declared_by,manifest,row_count,state,uploaded_by,committed_at) VALUES($1,$2,'management-fixture','bom-import/v1','mapping-v1',$3,'design_institute','{}',0,'committed',$4,$5)`, pkg, p.ID, "sha256:"+strings.Repeat("a", 64), root.ID, now)
	if e != nil {
		t.Fatal(e)
	}
	add := func(serial, index string, line int) {
		t.Helper()
		id := RandomID()
		_, err := s.DB.ExecContext(ctx, `INSERT INTO bom_items(item_uid,category,serial,position,pod,role,source_of_record,first_project_id,created_at,updated_at) VALUES($1,'switch',$2,jsonb_build_object('dc','dc1','rack','r1','idx',$3::text),'p1','leaf','design_bom',$4,$5,$5)`, id, serial, index, p.ID, now)
		if err != nil {
			t.Fatal(err)
		}
		_, err = s.DB.ExecContext(ctx, `INSERT INTO switches(id,serial_number,hostname,created_at,updated_at) VALUES($1,$2,$2,$3,$3)`, id, serial, now)
		if err != nil {
			t.Fatal(err)
		}
		_, err = s.DB.ExecContext(ctx, `INSERT INTO bom_item_revisions(package_id,item_uid,bom_line_no,project_id,row_snapshot,recorded_at) VALUES($1,$2,$3,$4,'{}',$5)`, pkg, id, line, p.ID, now)
		if err != nil {
			t.Fatal(err)
		}
	}
	add("SN-B", "2", 1)
	var auditsBefore int
	s.DB.QueryRowContext(ctx, "SELECT count(*) FROM audits").Scan(&auditsBefore)
	preview, e := s.PreviewZTPManagementPlan(ctx, actor, p, input)
	if e != nil {
		t.Fatal(e)
	}
	var auditsAfter, plans int
	s.DB.QueryRowContext(ctx, "SELECT count(*) FROM audits").Scan(&auditsAfter)
	s.DB.QueryRowContext(ctx, "SELECT count(*) FROM ztp_management_plans").Scan(&plans)
	if auditsAfter != auditsBefore || plans != 0 {
		t.Fatal("preview wrote state")
	}
	add("SN-C", "3", 2)
	input.ExpectedHash = preview.ContentHash
	_, e = s.CommitZTPManagementPlan(ctx, actor, p, input, "test")
	assertErrorCode(t, e, "ZTP_MANAGEMENT_PREVIEW_STALE")
	preview, e = s.PreviewZTPManagementPlan(ctx, actor, p, input)
	if e != nil {
		t.Fatal(e)
	}
	input.ExpectedHash = preview.ContentHash
	saved, e := s.CommitZTPManagementPlan(ctx, actor, p, input, "test")
	if e != nil {
		t.Fatal(e)
	}
	repeat, e := s.CommitZTPManagementPlan(ctx, actor, p, input, "test")
	if e != nil || repeat.ID != saved.ID {
		t.Fatal("commit not idempotent", e)
	}
	report, e := s.RunZTPEnvironmentCheck(ctx, actor, p, ZTPEnvironmentInput{BatchRef: saved.Assignments[0].Batch, BootChannel: "239"}, "test")
	if e != nil || report.Ready || len(report.Checks) != 4 {
		t.Fatal("unconfigured environment must persist four unknown checks", e)
	}
	loaded, e := s.LatestZTPEnvironmentCheck(ctx, actor, p, ZTPEnvironmentInput{BatchRef: report.BatchRef, BootChannel: "239"})
	if e != nil || loaded.ID != report.ID || loaded.Ready {
		t.Fatal("environment report persistence failed", e)
	}
	add("SN-A", "1", 3)
	next, e := s.PreviewZTPManagementPlan(ctx, actor, p, input)
	if e != nil {
		t.Fatal(e)
	}
	old := map[string]string{}
	for _, a := range saved.Assignments {
		old[a.Serial] = a.Address
	}
	for _, a := range next.Assignments {
		if ip, ok := old[a.Serial]; ok && ip != a.Address {
			t.Fatal("existing address changed")
		}
	}
	_, e = s.RunZTPEnvironmentCheck(ctx, actor, p, ZTPEnvironmentInput{BatchRef: report.BatchRef, BootChannel: "239"}, "test")
	assertErrorCode(t, e, "ZTP_MANAGEMENT_PREVIEW_STALE")
	latest, e := s.LatestZTPManagementPlan(ctx, actor, p)
	if e != nil || latest.ID != saved.ID {
		t.Fatal("latest plan not persisted", e)
	}
	user, e := s.CreateUser(ctx, "management-reader", "Reader", password, false, false)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.SetMembership(ctx, user.Username, p.ID, "Viewer", false); e != nil {
		t.Fatal(e)
	}
	_, reader := testLogin(t, s, user.Username, password, "198.51.100.92")
	_, e = s.PreviewZTPManagementPlan(ctx, reader, p, input)
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.RunZTPEnvironmentCheck(ctx, reader, p, ZTPEnvironmentInput{BatchRef: report.BatchRef, BootChannel: "239"}, "test")
	assertErrorCode(t, e, "FORBIDDEN")
	input.ExpectedHash = next.ContentHash
	_, e = s.CommitZTPManagementPlan(ctx, reader, p, input, "test")
	assertErrorCode(t, e, "FORBIDDEN")
	global, e := s.UpsertParamKey(ctx, actor, nil, UpsertParamKeyInput{Key: "fabric.batch_size", ValueType: "string", DefaultValue: "invalid-global"}, "test")
	if e != nil {
		t.Fatal(e)
	}
	local, e := s.UpsertParamKey(ctx, actor, p, UpsertParamKeyInput{Key: "fabric.batch_size", ValueType: "enum", Options: []string{"100", "200"}, DefaultValue: "100"}, "test")
	if e != nil {
		t.Fatal(e)
	}
	policyPreview, e := s.PreviewZTPManagementPlan(ctx, actor, p, input)
	if e != nil || policyPreview.BatchSize != 100 {
		t.Fatalf("scope-aware batch default: %+v %v", policyPreview, e)
	}
	if _, e = s.RetireParamKey(ctx, actor, p, local.ID, "test"); e != nil {
		t.Fatal(e)
	}
	_, e = s.PreviewZTPManagementPlan(ctx, actor, p, input)
	assertErrorCode(t, e, "PARAM_KEY_RETIRED")
	_, e = s.CommitZTPManagementPlan(ctx, actor, p, ZTPManagementPlanInput{BaselineRef: input.BaselineRef, ExpectedHash: policyPreview.ContentHash}, "test")
	assertErrorCode(t, e, "PARAM_KEY_RETIRED")
	if _, e = s.RetireParamKey(ctx, actor, nil, global.ID, "test"); e != nil {
		t.Fatal(e)
	}
	latestAfterRetirement, e := s.LatestZTPManagementPlan(ctx, actor, p)
	if e != nil || latestAfterRetirement.ID != saved.ID {
		t.Fatal("retirement rewrote historical management plan", e)
	}
}
