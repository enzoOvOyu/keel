package platform

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"
)

// These integration cases are authored with the foundation; execution is
// deferred to the next agreed milestone. integrationStore refuses live DBs.
func TestIssueFoundationPostgresLifecycleAndBoundaries(t *testing.T) {
	s := integrationStore(t)
	ctx := context.Background()
	password := "isolated-issue-foundation-password"
	rootUser, e := s.CreateUser(ctx, "issue-root", "Root", password, true, true)
	if e != nil {
		t.Fatal(e)
	}
	_, root := testLogin(t, s, rootUser.Username, password, "198.51.100.30")
	project, e := s.CreateProject(ctx, root, CreateProjectInput{Name: "Issue project", Slug: "issue-project"}, "issue-project", "test")
	if e != nil {
		t.Fatal(e)
	}
	other, e := s.CreateProject(ctx, root, CreateProjectInput{Name: "Other project", Slug: "issue-other"}, "issue-other", "test")
	if e != nil {
		t.Fatal(e)
	}
	neUser, e := s.CreateUser(ctx, "issue-ne", "Network engineer", password, false, false)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.SetMembership(ctx, neUser.Username, project.ID, "NE", false); e != nil {
		t.Fatal(e)
	}
	if e = s.SetMembership(ctx, neUser.Username, other.ID, "Viewer", false); e != nil {
		t.Fatal(e)
	}
	_, ne := testLogin(t, s, neUser.Username, password, "198.51.100.31")
	machine, port := RandomID(), RandomID()
	if _, e = s.DB.ExecContext(ctx, `INSERT INTO machines(id,serial_number,created_at,updated_at) VALUES($1,'ISSUE-MACHINE',now(),now())`, machine); e != nil {
		t.Fatal(e)
	}
	if _, e = s.DB.ExecContext(ctx, `INSERT INTO project_assets(id,project_id,machine_id,hostname,created_at,updated_at) VALUES($1,$2,$3,'issue-node',now(),now())`, RandomID(), project.ID, machine); e != nil {
		t.Fatal(e)
	}
	if _, e = s.DB.ExecContext(ctx, `INSERT INTO physical_ports(id,machine_id,mac_address,port_name,created_at) VALUES($1,$2,'02:ab:cd:11:22:33','unmapped-port',now())`, port, machine); e != nil {
		t.Fatal(e)
	}
	input := CreateIssueInput{Title: "Unmapped port observation", Category: "T3", Source: "g5", Phase: "delivery", Anchors: []IssueAnchor{{Kind: "port", ID: port}}, Evidence: IssueEvidence{ObservedAt: time.Now().UTC().Add(-time.Second), SourceVersion: "sample-v1", Expected: "link up", Actual: "link down"}}
	checkCode := func(err error, code string) {
		t.Helper()
		var pe *Error
		if !errors.As(err, &pe) || pe.Code != code {
			t.Fatalf("want %s, got %v", code, err)
		}
	}
	targets, e := s.IssueTargets(ctx, ne, project.ID, "unmapped")
	if e != nil || len(targets) != 1 || targets[0].ID != port {
		t.Fatalf("unmapped target picker: %+v %v", targets, e)
	}
	v, e := s.CreateIssue(ctx, ne, project.ID, input, "same-key", "test")
	if e != nil {
		t.Fatal(e)
	}
	if v.State != "open" || v.ResponsibleDomain != "unknown" || !slices.Contains(v.AllowedTransitions, "assign") {
		t.Fatalf("initial issue: %+v", v)
	}
	again, e := s.CreateIssue(ctx, ne, project.ID, input, "same-key", "test")
	if e != nil || again.ID != v.ID {
		t.Fatalf("idempotency: %+v %v", again, e)
	}
	changed := input
	changed.Title = "different"
	_, e = s.CreateIssue(ctx, ne, project.ID, changed, "same-key", "test")
	checkCode(e, "IDEMPOTENCY_CONFLICT")
	_, e = s.CreateIssue(ctx, root, other.ID, input, "foreign-anchor", "test")
	checkCode(e, "NOT_FOUND")
	_, e = s.CreateIssue(ctx, ne, other.ID, input, "viewer-cross-project", "test")
	checkCode(e, "FORBIDDEN")
	_, e = s.ActOnIssue(ctx, ne, v.ID, "assign", IssueActionInput{Version: 1, Reason: "assign", AssigneeID: root.User.ID}, "test")
	checkCode(e, "ISSUE_ASSIGNEE_UNAVAILABLE")
	v, e = s.ActOnIssue(ctx, ne, v.ID, "assign", IssueActionInput{Version: 1, Reason: "assign on-site work", AssigneeID: ne.User.ID}, "test")
	if e != nil {
		t.Fatal(e)
	}
	// Only one concurrent action can use an observed version. The loser must
	// refresh rather than silently replaying another operator's intent.
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.ActOnIssue(ctx, ne, v.ID, "start", IssueActionInput{Version: v.Version, Reason: "start field work"}, "test")
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	passed, conflicts := 0, 0
	for err := range results {
		if err == nil {
			passed++
		} else {
			checkCode(err, "VERSION_CONFLICT")
			conflicts++
		}
	}
	if passed != 1 || conflicts != 1 {
		t.Fatalf("CAS: %d/%d", passed, conflicts)
	}
	detail, e := s.GetIssue(ctx, ne, v.ID)
	if e != nil {
		t.Fatal(e)
	}
	v = &detail.Issue
	_, e = s.ActOnIssue(ctx, ne, v.ID, "repair", IssueActionInput{Version: v.Version, Reason: "replace optic", RepairAction: "replace_optic"}, "test")
	checkCode(e, "VALIDATION_ERROR")
	v, e = s.ActOnIssue(ctx, ne, v.ID, "repair", IssueActionInput{Version: v.Version, Reason: "replaced defective optic", RepairAction: "replace_optic", OldSerial: "OLD-OPTIC", NewSerial: "NEW-OPTIC"}, "test")
	if e != nil {
		t.Fatal(e)
	}
	for _, action := range []string{"verify", "close"} {
		_, e = s.ActOnIssue(ctx, ne, v.ID, action, IssueActionInput{Version: v.Version, Reason: "cannot fake verification"}, "test")
		checkCode(e, "ISSUE_REVALIDATION_REQUIRED")
	}
	detail, e = s.GetIssue(ctx, ne, v.ID)
	if e != nil || len(detail.Events) != 4 || detail.Issue.State != "in_repair" || detail.Issue.Source != "g5" {
		t.Fatalf("immutable original and timeline: %+v %v", detail, e)
	}
	for _, q := range []string{`UPDATE issues SET source='fabric',version=version+1 WHERE id=$1`, `DELETE FROM issues WHERE id=$1`, `DELETE FROM issue_events WHERE issue_id=$1`} {
		if _, e = s.DB.ExecContext(ctx, q, v.ID); e == nil {
			t.Fatalf("immutable history accepted: %s", q)
		}
	}
	// Access must be recomputed, despite a prior NE capability in the session.
	if e = s.SetMembership(ctx, neUser.Username, project.ID, "Viewer", false); e != nil {
		t.Fatal(e)
	}
	_, ne = testLogin(t, s, neUser.Username, password, "198.51.100.31")
	read, e := s.GetIssue(ctx, ne, v.ID)
	if e != nil || len(read.Issue.AllowedTransitions) != 0 {
		t.Fatalf("Viewer actions: %+v %v", read, e)
	}
	_, e = s.ActOnIssue(ctx, ne, v.ID, "repair", IssueActionInput{Version: v.Version, Reason: "no longer authorized", RepairAction: "reseat"}, "test")
	checkCode(e, "FORBIDDEN")
	if e = s.Logout(ctx, ne, "test"); e != nil {
		t.Fatal(e)
	}
	_, e = s.GetIssue(ctx, ne, v.ID)
	checkCode(e, "UNAUTHENTICATED")
}

func TestIssueFoundationPostgresAggregationAndNetworkOnlyTargets(t *testing.T) {
	s := integrationStore(t)
	ctx := context.Background()
	password := "isolated-issue-scope-password"
	if _, e := s.CreateUser(ctx, "issue-scope-root", "Root", password, true, true); e != nil {
		t.Fatal(e)
	}
	_, root := testLogin(t, s, "issue-scope-root", password, "198.51.100.33")
	one, e := s.CreateProject(ctx, root, CreateProjectInput{Name: "First", Slug: "issue-one"}, "one", "test")
	if e != nil {
		t.Fatal(e)
	}
	two, e := s.CreateProject(ctx, root, CreateProjectInput{Name: "Second", Slug: "issue-two"}, "two", "test")
	if e != nil {
		t.Fatal(e)
	}
	// Network-only objects must not require a Machine or G3 mapping.
	for i, p := range []*ProjectSummary{one, two} {
		profile, pkg, sw := RandomID(), RandomID(), RandomID()
		if _, e = s.DB.ExecContext(ctx, `INSERT INTO bom_mapping_profiles(id,project_id,version_ref,state,created_by,created_at) VALUES($1,$2,'issue-profile','approved',$3,now())`, profile, p.ID, root.User.ID); e != nil {
			t.Fatal(e)
		}
		if _, e = s.DB.ExecContext(ctx, `INSERT INTO bom_import_packages(id,project_id,bom_import_batch_id,schema_version,profile_version_ref,content_hash,declared_by,manifest,row_count,state,uploaded_by,committed_at) VALUES($1,$2,'issue-batch','bom-import/v1','issue-profile','sha256:'||repeat('a',64),'test','{}',1,'committed',$3,now())`, pkg, p.ID, root.User.ID); e != nil {
			t.Fatal(e)
		}
		if _, e = s.DB.ExecContext(ctx, `INSERT INTO bom_items(item_uid,category,serial,source_of_record,first_project_id,created_at,updated_at) VALUES($1,'switch',$2,'design_bom',$3,now(),now())`, sw, "ISSUE-SW-"+p.ID, p.ID); e != nil {
			t.Fatal(e)
		}
		if _, e = s.DB.ExecContext(ctx, `INSERT INTO switches(id,serial_number,hostname,created_at,updated_at) VALUES($1,$2,'issue-switch',now(),now())`, sw, "ISSUE-SW-"+p.ID); e != nil {
			t.Fatal(e)
		}
		if _, e = s.DB.ExecContext(ctx, `INSERT INTO bom_item_revisions(package_id,item_uid,bom_line_no,project_id,row_snapshot,recorded_at) VALUES($1,$2,1,$3,'{}',now())`, pkg, sw, p.ID); e != nil {
			t.Fatal(e)
		}
		input := CreateIssueInput{Title: "Network only", Category: "T6", Source: "fabric", Phase: "delivery", Anchors: []IssueAnchor{{Kind: "switch", ID: sw}}, Evidence: IssueEvidence{ObservedAt: time.Now(), SourceVersion: "network-sample", Actual: "configuration differs"}}
		if i == 1 {
			input.Source = "bmc"
			input.Phase = "post-delivery"
		}
		if _, e = s.CreateIssue(ctx, root, p.ID, input, "network-only", "test"); e != nil {
			t.Fatal(e)
		}
	}
	viewer, e := s.CreateUser(ctx, "issue-scope-reader", "Reader", password, false, false)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.SetMembership(ctx, viewer.Username, one.ID, "Viewer", false); e != nil {
		t.Fatal(e)
	}
	_, actor := testLogin(t, s, viewer.Username, password, "198.51.100.34")
	out, e := s.ListIssues(ctx, actor, Pagination{Page: 1, PageSize: 20}, IssueFilter{})
	if e != nil || out.Total != 1 || out.Items[0].ProjectID != one.ID {
		t.Fatalf("authorized aggregate: %+v %v", out, e)
	}
	all, e := s.ListIssues(ctx, root, Pagination{Page: 1, PageSize: 20}, IssueFilter{Phase: "post-delivery", Source: "bmc"})
	if e != nil || all.Total != 1 || all.Items[0].ProjectID != two.ID {
		t.Fatalf("phase/source filter: %+v %v", all, e)
	}
	if _, e = s.GetIssue(ctx, actor, all.Items[0].ID); e == nil {
		t.Fatal("unauthorized issue detail leaked")
	}
}
