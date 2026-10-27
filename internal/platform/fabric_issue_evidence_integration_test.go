package platform

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/fabricmon"
)

// Authored for the P2 milestone batch; not an invitation to rerun P1 tests.
func TestFabricIssueEvidencePostgresIdentityProvenanceAndHistory(t *testing.T) {
	s := integrationStore(t)
	ctx := context.Background()
	password := "isolated-network-issue-password"
	if _, e := s.CreateUser(ctx, "network-issue-root", "Root", password, true, true); e != nil {
		t.Fatal(e)
	}
	_, actor := testLogin(t, s, "network-issue-root", password, "198.51.100.71")
	project, e := s.CreateProject(ctx, actor, CreateProjectInput{Name: "Network issues", Slug: "network-issues"}, "network-issues", "test")
	if e != nil {
		t.Fatal(e)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := s.DB.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	pkg, profile := RandomID(), RandomID()
	swA, swB, portA, portB, cableID := RandomID(), RandomID(), RandomID(), RandomID(), RandomID()
	if portA > portB {
		portA, portB = portB, portA
	}
	exec(`INSERT INTO bom_mapping_profiles(id,project_id,version_ref,state,created_by,created_at) VALUES($1,$2,'network-issues','approved',$3,now())`, profile, project.ID, actor.User.ID)
	exec(`INSERT INTO bom_import_packages(id,project_id,bom_import_batch_id,schema_version,profile_version_ref,content_hash,declared_by,manifest,row_count,state,uploaded_by,committed_at) VALUES($1,$2,'network-issues','bom-import/v1','network-issues','sha256:'||repeat('a',64),'test','{}',5,'committed',$3,now())`, pkg, project.ID, actor.User.ID)
	for _, device := range []struct{ id, serial string }{{swA, "NETWORK-ISSUE-A"}, {swB, "NETWORK-ISSUE-B"}} {
		exec(`INSERT INTO bom_items(item_uid,category,serial,source_of_record,first_project_id,created_at,updated_at) VALUES($1,'switch',$2,'design_bom',$3,now(),now())`, device.id, device.serial, project.ID)
		exec(`INSERT INTO switches(id,serial_number,hostname,created_at,updated_at) VALUES($1,$2,$2,now(),now())`, device.id, device.serial)
	}
	for _, port := range []struct{ id, owner string }{{portA, swA}, {portB, swB}} {
		exec(`INSERT INTO bom_items(item_uid,category,parent_uid,port_index,port_name,source_of_record,first_project_id,created_at,updated_at) VALUES($1,'port',$2,'1','swp1','design_bom',$3,now(),now())`, port.id, port.owner, project.ID)
		exec(`INSERT INTO physical_ports(id,switch_id,parent_uid,port_index,port_name,created_at) VALUES($1,$2,$2,'1','swp1',now())`, port.id, port.owner)
	}
	exec(`INSERT INTO bom_items(item_uid,category,endpoint_a_uid,endpoint_b_uid,source_of_record,first_project_id,created_at,updated_at) VALUES($1,'cable',$2,$3,'design_bom',$4,now(),now())`, cableID, portA, portB, project.ID)
	for i, id := range []string{swA, swB, portA, portB, cableID} {
		exec(`INSERT INTO bom_item_revisions(package_id,item_uid,bom_line_no,project_id,row_snapshot,recorded_at) VALUES($1,$2,$3,$4,'{}',now())`, pkg, id, i+1, project.ID)
	}
	cable := fabricmon.Cable{UID: cableID, A: fabricmon.Endpoint{UID: portA, Switch: true, Serial: "NETWORK-ISSUE-A", Port: "swp1"}, B: fabricmon.Endpoint{UID: portB, Switch: true, Serial: "NETWORK-ISSUE-B", Port: "swp1"}}
	observed := time.Now().UTC().Add(-time.Second)
	down, up := false, true
	samples := map[string]fabricmon.Snapshot{}
	for _, endpoint := range []fabricmon.Endpoint{cable.A, cable.B} {
		link := &up
		if endpoint.UID == portA {
			link = &down
		}
		samples[endpoint.Serial] = fabricmon.Snapshot{Version: "fabric-snapshot/v1", Source: "switch", Provider: "milestone-test", Serial: endpoint.Serial, CollectedAt: observed, Capabilities: map[string]bool{"interfaces": true, "lldp": true}, Ports: []fabricmon.Port{{Name: "swp1", LinkUp: link, LLDPReadable: true}}}
	}
	persist := func(origin, verdict string, stale, rollback bool) *FabricCollectionReport {
		t.Helper()
		finding := fabricmon.EvaluateCable(cable, samples)
		if finding.Verdict != "anomaly" || finding.IssueType != "T1" {
			t.Fatalf("fixture classifier: %+v", finding)
		}
		if verdict != "" {
			finding.Verdict = verdict
			if verdict != "anomaly" {
				finding.IssueType = ""
			}
		}
		expiry := observed.Add(5 * time.Minute)
		if stale {
			expiry = time.Now().Add(-time.Second)
		}
		report := &FabricCollectionReport{ID: RandomID(), PackageID: pkg, SourceHash: digest("fixture-run"), EvidenceSource: origin, Stage: "N-S5", NP3: "BLOCKED", CollectedAt: time.Now().UTC(), ValidUntil: expiry, Devices: []FabricDeviceSummary{}, Findings: []fabricmon.Finding{finding}, IssueLinks: []FabricIssueLink{}}
		tx, err := s.beginControl(ctx, actor)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		raw, err := json.Marshal(report)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO fabric_collection_runs(id,project_id,package_id,report,source_hash,collected_at,valid_until,created_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, report.ID, project.ID, pkg, raw, report.SourceHash, report.CollectedAt, expiry, actor.User.ID); err != nil {
			t.Fatal(err)
		}
		for id, sample := range map[string]fabricmon.Snapshot{swA: samples[cable.A.Serial], swB: samples[cable.B.Serial]} {
			raw, _ = json.Marshal(sample)
			if _, err = tx.ExecContext(ctx, `INSERT INTO fabric_switch_samples(run_id,switch_id,content_hash,sample) VALUES($1,$2,$3,$4)`, report.ID, id, digest(string(raw)), raw); err != nil {
				t.Fatal(err)
			}
		}
		if err = recordFabricIssueEvidenceTx(ctx, tx, actor, project.ID, report, []fabricmon.Cable{cable}, samples, "test"); err != nil {
			t.Fatal(err)
		}
		// A retried producer in the same transaction does not duplicate observations.
		if err = recordFabricIssueEvidenceTx(ctx, tx, actor, project.ID, report, []fabricmon.Cable{cable}, samples, "test"); err != nil {
			t.Fatal(err)
		}
		report.IssueLinks, err = fabricIssueLinksTx(ctx, tx, project.ID, report.ID)
		if err != nil {
			t.Fatal(err)
		}
		if rollback {
			if err = tx.Rollback(); err != nil {
				t.Fatal(err)
			}
		} else if err = tx.Commit(); err != nil {
			t.Fatal(err)
		}
		return report
	}
	first := persist("protocol_fixture", "", false, false)
	if len(first.IssueLinks) != 1 {
		t.Fatalf("automatic issue link: %+v", first.IssueLinks)
	}
	id := first.IssueLinks[0].IssueID
	detail, e := s.GetIssue(ctx, actor, id)
	if e != nil || detail.Issue.Source != "fabric" || detail.Issue.Version != 1 || len(detail.Events) != 1 {
		t.Fatalf("initial discovery: %+v %v", detail, e)
	}
	originalObserved := detail.Issue.Evidence.ObservedAt
	repeated := persist("protocol_fixture", "", false, false)
	if len(repeated.IssueLinks) != 1 || repeated.IssueLinks[0].IssueID != id {
		t.Fatalf("duplicate issue: %+v", repeated.IssueLinks)
	}
	detail, e = s.GetIssue(ctx, actor, id)
	if e != nil || detail.Issue.Version != 1 || len(detail.Events) != 1 {
		t.Fatalf("unchanged observation changed version: %+v %v", detail, e)
	}
	history, e := s.FabricIssueObservations(ctx, actor, id, Pagination{Page: 1, PageSize: 20})
	if e != nil || history.Total != 2 || history.Items[0].EvidenceOrigin != "protocol_fixture" || history.Items[0].Stale {
		t.Fatalf("immutable run history: %+v %v", history, e)
	}
	sample := samples[cable.A.Serial]
	sample.CollectedAt = observed.Add(time.Millisecond)
	samples[cable.A.Serial] = sample
	persist("protocol_fixture", "", false, false)
	detail, e = s.GetIssue(ctx, actor, id)
	if e != nil || detail.Issue.Version != 2 || !detail.Issue.Evidence.ObservedAt.Equal(originalObserved) || len(detail.Events) != 2 {
		t.Fatalf("append observation without rewriting original: %+v %v", detail, e)
	}
	device := persist("device", "", false, false)
	if len(device.IssueLinks) != 1 || device.IssueLinks[0].IssueID == id {
		t.Fatalf("mode isolation: %+v", device.IssueLinks)
	}
	persist("protocol_fixture", "pending", false, false)
	persist("protocol_fixture", "precheck_pass", false, false)
	persist("protocol_fixture", "", true, false)
	detail, e = s.GetIssue(ctx, actor, id)
	if e != nil || detail.Issue.Version != 2 || detail.Issue.State != "open" {
		t.Fatalf("pending/pass/stale must not close issue: %+v %v", detail, e)
	}
	before := detail.Issue.Version
	sample = samples[cable.A.Serial]
	sample.CollectedAt = observed.Add(2 * time.Millisecond)
	samples[cable.A.Serial] = sample
	persist("protocol_fixture", "", false, true)
	detail, e = s.GetIssue(ctx, actor, id)
	if e != nil || detail.Issue.Version != before {
		t.Fatalf("rolled-back producer leaked issue mutation: %+v %v", detail, e)
	}
	for _, q := range []string{`DELETE FROM fabric_issue_bindings WHERE issue_id=$1`, `UPDATE fabric_issue_observations SET finding='{}' WHERE issue_id=$1`} {
		if _, e = s.DB.ExecContext(ctx, q, id); e == nil {
			t.Fatal("discovery evidence history was mutable")
		}
	}
	// New BOM identity/version invalidates old observation usability, not history.
	nextPackage := RandomID()
	exec(`INSERT INTO bom_import_packages(id,project_id,bom_import_batch_id,schema_version,profile_version_ref,content_hash,declared_by,manifest,row_count,state,uploaded_by,committed_at) VALUES($1,$2,'network-issues-next','bom-import/v1','network-issues','sha256:'||repeat('b',64),'test','{}',1,'committed',$3,now()+interval '1 second')`, nextPackage, project.ID, actor.User.ID)
	// A package without a network item does not replace the active network BOM.
	history, e = s.FabricIssueObservations(ctx, actor, id, Pagination{Page: 1, PageSize: 20})
	if e != nil || history.Total != 3 || history.Items[0].Stale {
		t.Fatalf("unrelated package must not invalidate network scope: %+v %v", history, e)
	}
	exec(`INSERT INTO bom_item_revisions(package_id,item_uid,bom_line_no,project_id,row_snapshot,recorded_at) VALUES($1,$2,1,$3,'{}',now())`, nextPackage, swA, project.ID)
	history, e = s.FabricIssueObservations(ctx, actor, id, Pagination{Page: 1, PageSize: 20})
	if e != nil || !history.Items[0].Stale {
		t.Fatalf("changed network BOM must invalidate old usability: %+v %v", history, e)
	}
	reader, e := s.CreateUser(ctx, "network-issue-reader", "Reader", password, false, false)
	if e != nil {
		t.Fatal(e)
	}
	_, outsider := testLogin(t, s, reader.Username, password, "198.51.100.72")
	if _, e = s.FabricIssueObservations(ctx, outsider, id, Pagination{Page: 1, PageSize: 20}); e == nil {
		t.Fatal("observation history leaked across project boundary")
	}
}
