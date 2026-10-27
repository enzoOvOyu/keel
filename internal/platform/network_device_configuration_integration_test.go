package platform

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/netbaseline"
)

// Source-authored for one milestone run. The synthetic records below assert
// read ordering and scope, never hardware behavior or real Golden readiness.
func TestDeviceConfigurationKeepsLatestProductionAttemptAndReadonlyHistoryPostgres(t *testing.T) {
	s, root, editor, _, p := templateLineageFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, e := s.DB.ExecContext(ctx, query, args...); e != nil {
			t.Fatal(e)
		}
	}
	pkg, profile, device := RandomID(), RandomID(), RandomID()
	serial := "CONFIG-" + device[:12]
	exec(`INSERT INTO bom_mapping_profiles(id,project_id,version_ref,state,created_by,created_at) VALUES($1,$2,'configuration','approved',$3,now())`, profile, p.ID, root.User.ID)
	exec(`INSERT INTO bom_import_packages(id,project_id,bom_import_batch_id,schema_version,profile_version_ref,content_hash,declared_by,manifest,row_count,state,uploaded_by,committed_at) VALUES($1,$2,'configuration','bom-import/v1','configuration','sha256:'||repeat('a',64),'test','{}',1,'committed',$3,now())`, pkg, p.ID, root.User.ID)
	exec(`INSERT INTO bom_items(item_uid,category,serial,source_of_record,first_project_id,created_at,updated_at) VALUES($1,'switch',$2,'design_bom',$3,now(),now())`, device, serial, p.ID)
	exec(`INSERT INTO switches(id,serial_number,hostname,created_at,updated_at) VALUES($1,$2,$2,now(),now())`, device, serial)
	exec(`INSERT INTO bom_item_revisions(package_id,item_uid,bom_line_no,project_id,row_snapshot,recorded_at) VALUES($1,$2,1,$3,'{}',now())`, pkg, device, p.ID)
	template, e := s.CreateNetworkTemplate(ctx, editor, p, NetworkTemplateInput{Name: "device history", SourceRef: "fixture:read-history", Definition: netbaseline.Template{Source: `{"hostname":"leaf-one"}`, Variables: map[string]netbaseline.Variable{}}}, "test")
	if e != nil {
		t.Fatal(e)
	}
	exec(`UPDATE network_template_versions SET state='released',released_by=$2 WHERE id=$1`, template.ID, root.User.ID)
	view, e := s.NetworkDeviceConfiguration(ctx, root, p, device)
	if e != nil || view.LatestProduction != nil || view.LatestPlan != nil {
		t.Fatal("empty history fabricated records", e)
	}
	intendedID := RandomID()
	intended := []byte(`{"hostname":"leaf-one"}`)
	hash := netbaseline.ContentHash(json.RawMessage(intended))
	// Hash the same raw JSON type used by production rendering, not a byte slice.
	facts := NetworkPlannedDevice{ID: device, Serial: serial, Hostname: "leaf-one", IntendedID: intendedID, IntendedHash: hash}
	exec(`INSERT INTO network_intended_configs(id,project_id,template_id,switch_id,content_hash,configuration,created_at) VALUES($1,$2,$3,$4,$5,$6,now())`, intendedID, p.ID, template.ID, device, hash, intended)
	flow, version := RandomID(), RandomID()
	exec(`INSERT INTO exec_flows(id,project_id,name,created_by,created_at,updated_at) VALUES($1,$2,'configuration history',$3,now(),now())`, flow, p.ID, root.User.ID)
	exec(`INSERT INTO exec_flow_versions(id,flow_id,project_id,version,definition,content_hash,created_by,created_at) VALUES($1,$2,$3,1,'{}',repeat('a',64),$4,now())`, version, flow, p.ID, root.User.ID)
	seed := func(jobAt, executionAt time.Time, state, executionState string) string {
		t.Helper()
		job, x := RandomID(), RandomID()
		exec(`INSERT INTO exec_executions(id,project_id,flow_version_id,mode,state,parameters,snapshot,target_set_hash,concurrency,created_by,created_at) VALUES($1,$2,$3,'fabric',$4,'{}','{}',repeat('a',64),1,$5,$6)`, x, p.ID, version, executionState, root.User.ID, executionAt)
		exec(`INSERT INTO network_push_jobs(id,project_id,template_id,baseline_id,input,facts_hash,config_hash,state,canary_min_size,push_concurrency,push_execution_id,created_by,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,repeat('a',64),'PARTIAL',5,1,$7,$8,$9,$9)`, job, p.ID, template.ID, RandomID(), networkJSON(NetworkPlanInput{TemplateID: template.ID, BaselineRef: "history-v1", BatchRef: "history-batch"}), netbaseline.ContentHash([]NetworkPlannedDevice{facts}), x, root.User.ID, jobAt)
		proof := netbaseline.OperationResult{Version: "network-operation/v1", ProjectID: p.ID, DeviceID: device, Serial: serial, RunningHash: hash, State: "completed", FencingToken: 1, Provider: "synthetic-history-test", EvidenceSource: "protocol_fixture", ObservedAt: executionAt}
		for _, id := range networkVerifyGates {
			proof.Gates = append(proof.Gates, netbaseline.Gate{ID: id, State: "PASS"})
		}
		exec(`INSERT INTO network_push_devices(job_id,switch_id,ordinal,intended_id,facts,state,diff,verify_evidence,updated_at) VALUES($1,$2,0,$3,$4,$5,'[]',$6,now())`, job, device, intendedID, networkJSON(facts), state, networkJSON(proof))
		exec(`INSERT INTO network_push_events(job_id,state,metadata,created_at) VALUES($1,'CREATED','{}',$2)`, job, jobAt)
		return job
	}
	olderPlanLaterExecution := seed(now.Add(-2*time.Hour), now.Add(-10*time.Minute), "VERIFIED", "success")
	newestPlanOlderExecution := seed(now.Add(-time.Hour), now.Add(-20*time.Minute), "VERIFIED", "success")
	view, e = s.NetworkDeviceConfiguration(ctx, root, p, device)
	if e != nil {
		t.Fatal(e)
	}
	if view.LatestProduction.JobID != olderPlanLaterExecution || view.LatestPlan.JobID != newestPlanOlderExecution {
		t.Fatal("job creation incorrectly chosen as production ordering")
	}
	unknown := seed(now.Add(-3*time.Hour), now.Add(-time.Minute), "UNKNOWN", "reconciling")
	var before, after int
	if e = s.DB.QueryRowContext(ctx, "SELECT count(*) FROM audits WHERE project_id=$1", p.ID).Scan(&before); e != nil {
		t.Fatal(e)
	}
	view, e = s.NetworkDeviceConfiguration(ctx, editor, p, device)
	if e != nil {
		t.Fatal(e)
	}
	if view.LatestProduction.JobID != unknown || view.LatestProduction.DeviceState != "UNKNOWN" || view.LatestProduction.Golden.State == "PASS" || view.LatestPlan.JobID != newestPlanOlderExecution {
		t.Fatal("unknown latest attempt hidden by older success")
	}
	// Batch state events are visible but not misrepresented as device success;
	// events explicitly belonging to another target never appear in this drawer.
	exec(`INSERT INTO network_push_events(job_id,state,metadata,created_at) VALUES($1,'VERIFYING',jsonb_build_object('device_id',$2::text),now())`, unknown, strings.Repeat("f", 32))
	page, e := s.NetworkDeviceConfigurationEvents(ctx, editor, p, device, 1, 2)
	if e != nil || page.Total != 3 || len(page.Items) != 2 {
		t.Fatal("event scope/page mismatch", e)
	}
	for _, event := range page.Items {
		if event.Scope != "batch" {
			t.Fatal("batch event relabeled as a device outcome")
		}
	}
	if e = s.DB.QueryRowContext(ctx, "SELECT count(*) FROM audits WHERE project_id=$1", p.ID).Scan(&after); e != nil || before != after {
		t.Fatal("read-only drawer wrote audit/business state", e)
	}
	foreign, e := s.CreateProject(ctx, root, CreateProjectInput{Name: "Foreign configuration", Slug: "other-config-" + RandomID()[:12]}, RandomID(), "test")
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.NetworkDeviceConfiguration(ctx, root, foreign, device)
	if e != ErrNotFound {
		t.Fatal("foreign project saw device configuration", e)
	}
	_, e = s.NetworkDeviceConfigurationEvents(ctx, root, foreign, device, 1, 20)
	if e != ErrNotFound {
		t.Fatal("foreign project saw device events", e)
	}
	_, e = s.NetworkDeviceConfigurationEvents(ctx, root, p, device, 0, 20)
	assertErrorCode(t, e, "FABRIC_PAGE_INVALID")
}
