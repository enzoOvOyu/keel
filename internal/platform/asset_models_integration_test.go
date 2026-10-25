package platform

import (
	"context"
	"testing"
)

func TestAssetModelBinding(t *testing.T) {
	s := integrationStore(t)
	ctx := context.Background()
	const password = "test-only-asset-model-password"
	for _, name := range []string{"asset-model-admin", "asset-model-viewer"} {
		if _, e := s.CreateUser(ctx, name, name, password, name == "asset-model-admin", name == "asset-model-admin"); e != nil {
			t.Fatal(e)
		}
	}
	_, admin := testLogin(t, s, "asset-model-admin", password, "198.51.100.96")
	project, e := s.CreateProject(ctx, admin, CreateProjectInput{Name: "Pinned assets", Slug: "pinned-assets"}, "asset-model-project", "test")
	if e != nil {
		t.Fatal(e)
	}
	foreign, e := s.CreateProject(ctx, admin, CreateProjectInput{Name: "Other assets", Slug: "other-assets"}, "other-asset-model-project", "test")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ControlCreateMember(ctx, admin, project.ID, CreateMemberInput{Username: "asset-model-viewer", Role: "Viewer"}, "test"); e != nil {
		t.Fatal(e)
	}
	_, viewer := testLogin(t, s, "asset-model-viewer", password, "198.51.100.97")
	for _, scope := range []*ProjectSummary{project, nil} {
		name := "Private pinned"
		if scope == nil {
			name = "Shared pinned"
		}
		m, err := s.CreateDeviceModel(ctx, admin, scope, CreateDeviceModelInput{Name: name, Kind: "compute-tray"}, "test")
		if err != nil {
			t.Fatal(err)
		}
		input := PublishModelVersionInput{HwSpec: map[string]any{"source": "independent-asset-fixture"}, Slots: []ModelSlot{{SlotNo: 1, PcieSlot: "PCIe-1", ExpectNicModel: "Fixture-NIC", PanelPort: "p1", DefaultRole: "unused"}}, PanelLayout: map[string]any{"layout_v1": map[string]any{"rows": 1, "columns": 1, "ports": []any{map[string]any{"id": "p1", "label": "Bound-v1", "row": 1, "column": 1, "slot_no": 1}}}}}
		v1, err := s.PublishModelVersion(ctx, admin, scope, m.ID, input, "test")
		if err != nil {
			t.Fatal(err)
		}
		machine, asset := RandomID(), RandomID()
		if _, err = s.DB.ExecContext(ctx, `INSERT INTO machines(id,serial_number,created_at,updated_at) VALUES($1,$2,now(),now())`, machine, "PINNED-"+m.ID); err != nil {
			t.Fatal(err)
		}
		if _, err = s.DB.ExecContext(ctx, `INSERT INTO project_assets(id,project_id,machine_id,model_id,model_version_id,hostname,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,9,now(),now())`, asset, project.ID, machine, m.ID, v1.ID, "pinned-"+m.ID); err != nil {
			t.Fatal(err)
		}
		input.PanelLayout = map[string]any{"layout_v1": map[string]any{"rows": 1, "columns": 1, "ports": []any{map[string]any{"id": "p2", "label": "Unbound-v2", "row": 1, "column": 1, "slot_no": 1}}}}
		if _, err = s.PublishModelVersion(ctx, admin, scope, m.ID, input, "test"); err != nil {
			t.Fatal(err)
		}
		if _, err = s.RetireDeviceModel(ctx, admin, scope, m.ID, "test"); err != nil {
			t.Fatal(err)
		}
		binding, err := s.GetAssetModelBinding(ctx, viewer, project, asset)
		if err != nil || binding.ModelVersion.ID != v1.ID || binding.AssetVersion != 9 || binding.Model.Version != 2 || binding.Model.State != "retired" || len(binding.ModelVersion.Slots) != 1 || binding.ModelVersion.Slots[0].DefaultRole != "unused" {
			t.Fatalf("binding changed: %+v %v", binding, err)
		}
		if binding.ModelVersion.ContentHash != v1.ContentHash {
			t.Fatal("returned latest instead of pinned version")
		}
		list, err := s.ListProjectAssets(ctx, viewer, project, Pagination{Page: 1, PageSize: 20})
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, row := range list.Items {
			if row.ID == asset {
				found = row.ModelVersionID == v1.ID && row.ModelVersion == 1 && row.Version == 9 && row.ModelID == m.ID
			}
		}
		if !found {
			t.Fatalf("list omitted pinned IDs: %+v", list)
		}
		if _, err = s.GetAssetModelBinding(ctx, admin, foreign, asset); err == nil {
			t.Fatal("asset read through wrong project")
		}
		if _, err = s.GetAssetModelBinding(ctx, viewer, foreign, asset); err == nil {
			t.Fatal("outsider read binding")
		}
	}
	if _, err := s.GetAssetModelBinding(ctx, viewer, project, RandomID()); err == nil {
		t.Fatal("missing asset returned binding")
	}
	// Legacy assets without a version stay explicitly unbound, never latest.
	machine, asset := RandomID(), RandomID()
	if _, e = s.DB.ExecContext(ctx, `INSERT INTO machines(id,serial_number,created_at,updated_at) VALUES($1,'UNBOUND-ASSET',now(),now())`, machine); e != nil {
		t.Fatal(e)
	}
	if _, e = s.DB.ExecContext(ctx, `INSERT INTO project_assets(id,project_id,machine_id,hostname,created_at,updated_at) VALUES($1,$2,$3,'unbound',now(),now())`, asset, project.ID, machine); e != nil {
		t.Fatal(e)
	}
	if _, e = s.GetAssetModelBinding(ctx, viewer, project, asset); e == nil {
		t.Fatal("unbound asset acquired a model implicitly")
	}
	list, e := s.ListProjectAssets(ctx, viewer, project, Pagination{Page: 1, PageSize: 20})
	if e != nil {
		t.Fatal(e)
	}
	for _, row := range list.Items {
		if row.ID == asset && (row.ModelID != "" || row.ModelVersionID != "" || row.ModelVersion != 0) {
			t.Fatal("unbound list invented a version")
		}
	}
}
