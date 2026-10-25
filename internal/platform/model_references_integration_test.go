package platform

import (
	"context"
	"testing"
)

func TestModelReferenceCounts(t *testing.T) {
	s := integrationStore(t)
	ctx := context.Background()
	const password = "isolated-reference-test-password"
	for _, name := range []string{"reference-admin", "reference-viewer"} {
		if _, err := s.CreateUser(ctx, name, name, password, name == "reference-admin", name == "reference-admin"); err != nil {
			t.Fatal(err)
		}
	}
	_, admin := testLogin(t, s, "reference-admin", password, "198.51.100.98")
	a, err := s.CreateProject(ctx, admin, CreateProjectInput{Name: "Reference A", Slug: "reference-a"}, "reference-a", "test")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.CreateProject(ctx, admin, CreateProjectInput{Name: "Reference B", Slug: "reference-b"}, "reference-b", "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ControlCreateMember(ctx, admin, a.ID, CreateMemberInput{Username: "reference-viewer", Role: "Viewer"}, "test"); err != nil {
		t.Fatal(err)
	}
	_, viewer := testLogin(t, s, "reference-viewer", password, "198.51.100.99")
	global, err := s.CreateDeviceModel(ctx, admin, nil, CreateDeviceModelInput{Name: "Shared reference", Kind: "server"}, "test")
	if err != nil {
		t.Fatal(err)
	}
	private, err := s.CreateDeviceModel(ctx, admin, a, CreateDeviceModelInput{Name: "Private reference", Kind: "server"}, "test")
	if err != nil {
		t.Fatal(err)
	}
	v1, err := s.PublishModelVersion(ctx, admin, nil, global.ID, PublishModelVersionInput{}, "test")
	if err != nil {
		t.Fatal(err)
	}
	v2, err := s.PublishModelVersion(ctx, admin, nil, global.ID, PublishModelVersionInput{HwSpec: map[string]any{"revision": "second"}}, "test")
	if err != nil {
		t.Fatal(err)
	}
	pv, err := s.PublishModelVersion(ctx, admin, a, private.ID, PublishModelVersionInput{}, "test")
	if err != nil {
		t.Fatal(err)
	}
	machine := RandomID()
	if _, err = s.DB.ExecContext(ctx, `INSERT INTO machines(id,serial_number,created_at,updated_at) VALUES($1,'REFERENCE-SHARED-MACHINE',now(),now())`, machine); err != nil {
		t.Fatal(err)
	}
	add := func(project, model, version, machineID string) {
		t.Helper()
		if machineID == "" {
			machineID = RandomID()
			if _, e := s.DB.ExecContext(ctx, `INSERT INTO machines(id,serial_number,created_at,updated_at) VALUES($1,$2,now(),now())`, machineID, "REFERENCE-"+machineID); e != nil {
				t.Fatal(e)
			}
		}
		if _, e := s.DB.ExecContext(ctx, `INSERT INTO project_assets(id,project_id,machine_id,model_id,model_version_id,hostname,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,now(),now())`, RandomID(), project, machineID, model, version, "ref-"+RandomID()); e != nil {
			t.Fatal(e)
		}
	}
	add(a.ID, global.ID, v1.ID, machine)
	add(a.ID, global.ID, v2.ID, "")
	// The same physical machine has a separate project-history binding.
	add(b.ID, global.ID, v1.ID, machine)
	add(a.ID, private.ID, pv.ID, "")
	// Invalid model/version pairs must not inflate an authoritative count.
	add(a.ID, global.ID, pv.ID, "")
	check := func(actor *AuthSession, project *ProjectSummary, want map[string]int64) {
		t.Helper()
		page, e := s.ListDeviceModels(ctx, actor, project, Pagination{Page: 1, PageSize: 20})
		if e != nil {
			t.Fatal(e)
		}
		got := map[string]int64{}
		for _, m := range page.Items {
			if m.ReferenceCount == nil {
				t.Fatal("list omitted count")
			}
			got[m.ID] = *m.ReferenceCount
		}
		if len(got) != len(want) {
			t.Fatalf("scope leak: %v", got)
		}
		for id, n := range want {
			if got[id] != n {
				t.Fatalf("count %s=%d want %d", id, got[id], n)
			}
		}
	}
	check(viewer, a, map[string]int64{global.ID: 2, private.ID: 1})
	check(admin, b, map[string]int64{global.ID: 1})
	check(admin, nil, map[string]int64{global.ID: 3})
	if _, err = s.RetireDeviceModel(ctx, admin, nil, global.ID, "test"); err != nil {
		t.Fatal(err)
	}
	check(viewer, a, map[string]int64{global.ID: 2, private.ID: 1})
	check(admin, nil, map[string]int64{global.ID: 3})
	if _, err = s.ListDeviceModels(ctx, viewer, b, Pagination{Page: 1, PageSize: 20}); err == nil {
		t.Fatal("foreign project count leaked")
	}
	if _, err = s.ListDeviceModels(ctx, viewer, nil, Pagination{Page: 1, PageSize: 20}); err == nil {
		t.Fatal("viewer read cross-project global counts")
	}
}
