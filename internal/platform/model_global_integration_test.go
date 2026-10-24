package platform

import (
	"context"
	"testing"
)

func TestModelGlobalScope(t *testing.T) {
	s := integrationStore(t)
	ctx := context.Background()
	const password = "test-only-global-model-password"
	if _, e := s.CreateUser(ctx, "global-model-admin", "Model admin", password, true, true); e != nil {
		t.Fatal(e)
	}
	_, admin := testLogin(t, s, "global-model-admin", password, "198.51.100.94")
	project, e := s.CreateProject(ctx, admin, CreateProjectInput{Name: "Global models", Slug: "global-models"}, "global-model-project", "test")
	if e != nil {
		t.Fatal(e)
	}
	scan := ImportModelScanInput{ModelName: "Shared fixture", SourceHost: "independent-fixture", HwSpec: map[string]any{"kind": "server"}}
	draft, e := s.ImportModelScan(ctx, admin, nil, scan, "test")
	if e != nil {
		t.Fatal(e)
	}
	same, e := s.ImportModelScan(ctx, admin, nil, scan, "test")
	if e != nil || same.ID != draft.ID {
		t.Fatalf("global draft reuse: %+v %v", same, e)
	}
	expected := int64(0)
	payload := scan.payload()
	payload.ExpectedVersion = &expected
	payload.SourceDraftID = draft.ID
	published, e := s.PublishModelVersion(ctx, admin, nil, draft.ModelID, payload, "test")
	if e != nil {
		t.Fatal(e)
	}
	private, e := s.CreateDeviceModel(ctx, admin, project, CreateDeviceModelInput{Name: scan.ModelName, Kind: "server"}, "test")
	if e != nil {
		t.Fatal(e)
	}
	global, e := s.ListDeviceModels(ctx, admin, nil, Pagination{Page: 1, PageSize: 20})
	if e != nil || global.Total != 1 || global.Items[0].ScopeKey != "global" {
		t.Fatalf("global list: %+v %v", global, e)
	}
	if _, e = s.ListModelVersions(ctx, admin, nil, private.ID, Pagination{Page: 1, PageSize: 20}); e == nil {
		t.Fatal("global endpoint read project version")
	}
	if _, e = s.PublishModelVersion(ctx, admin, project, draft.ModelID, payload, "test"); e == nil {
		t.Fatal("project endpoint wrote global version")
	}
	if e = s.DeleteModelDraft(ctx, admin, project, draft.ModelID, draft.ID, "test"); e == nil {
		t.Fatal("project endpoint discarded global draft")
	}
	if _, e = s.PublishModelVersion(ctx, admin, nil, private.ID, PublishModelVersionInput{}, "test"); e == nil {
		t.Fatal("global endpoint wrote project model")
	}
	for _, role := range []string{"Admin", "SRE", "Viewer"} {
		username := "scoped-global-model-" + role
		if _, e = s.CreateUser(ctx, username, username, password, false, false); e != nil {
			t.Fatal(e)
		}
		if _, e = s.ControlCreateMember(ctx, admin, project.ID, CreateMemberInput{Username: username, Role: role}, "test"); e != nil {
			t.Fatal(e)
		}
		_, member := testLogin(t, s, username, password, "198.51.100.95")
		if _, e = s.ListDeviceModels(ctx, member, nil, Pagination{Page: 1, PageSize: 20}); e == nil {
			t.Fatal("member read global management list")
		}
		if _, e = s.ListModelVersions(ctx, member, nil, draft.ModelID, Pagination{Page: 1, PageSize: 20}); e == nil {
			t.Fatal("member read global management versions")
		}
		if _, e = s.ListModelDrafts(ctx, member, nil, draft.ModelID); e == nil {
			t.Fatal("member read global management drafts")
		}
		versions, e := s.ListModelVersions(ctx, member, project, draft.ModelID, Pagination{Page: 1, PageSize: 20})
		if e != nil || versions.Items[0].ID != published.ID {
			t.Fatalf("member lost shared version read: %+v %v", versions, e)
		}
		drafts, e := s.ListModelDrafts(ctx, member, project, draft.ModelID)
		if e != nil || len(drafts) != 0 {
			t.Fatalf("shared private draft leaked: %+v %v", drafts, e)
		}
		if _, e = s.ImportModelScan(ctx, member, nil, scan, "test"); e == nil {
			t.Fatal("member imported global draft")
		}
		if _, e = s.RetireDeviceModel(ctx, member, nil, draft.ModelID, "test"); e == nil {
			t.Fatal("member retired global model")
		}
	}
	profile := testBOMProfile()
	if _, e = s.CreateBOMMappingProfile(ctx, admin, project, CreateBOMMappingProfileInput{VersionRef: "profile-v1", ServerColumns: profile.ServerColumns, FabricColumns: profile.FabricColumns}, "test"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ApproveBOMMappingProfile(ctx, admin, project, "profile-v1", "test"); e != nil {
		t.Fatal(e)
	}
	csv := []byte("SN,hostname,model_name,MAC,port_name,rail\nSHARED-SN,shared-node,Shared fixture,aa:bb:cc:dd:ee:09,p1,0\n")
	batch := "BOM-BATCH-global-models-20261024-1"
	preview, e := s.PreviewLegacyBOM(ctx, admin, project, batch, csv)
	if e != nil || !preview.CanCommit {
		t.Fatalf("shared BOM preview: %+v %v", preview, e)
	}
	if _, e = s.CommitLegacyBOM(ctx, admin, project, batch, csv, preview.PreviewToken, "test"); e != nil {
		t.Fatal(e)
	}
	next := int64(1)
	if _, e = s.PublishModelVersion(ctx, admin, nil, draft.ModelID, PublishModelVersionInput{ExpectedVersion: &next, HwSpec: map[string]any{"gpu_count": 2}}, "test"); e != nil {
		t.Fatal(e)
	}
	var pinned string
	if e = s.DB.QueryRowContext(ctx, `SELECT model_version_id FROM project_assets WHERE project_id=$1`, project.ID).Scan(&pinned); e != nil || pinned != published.ID {
		t.Fatalf("global revision changed existing asset: %s %v", pinned, e)
	}
	if _, e = s.RetireDeviceModel(ctx, admin, nil, draft.ModelID, "test"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.PublishModelVersion(ctx, admin, nil, draft.ModelID, PublishModelVersionInput{}, "test"); e == nil {
		t.Fatal("retired model published")
	}
	versions, e := s.ListModelVersions(ctx, admin, project, draft.ModelID, Pagination{Page: 1, PageSize: 20})
	if e != nil || len(versions.Items) != 2 || versions.Items[1].ID != published.ID {
		t.Fatalf("retired history lost: %+v %v", versions, e)
	}
}
