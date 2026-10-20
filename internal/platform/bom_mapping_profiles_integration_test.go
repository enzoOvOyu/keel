package platform

import (
	"context"
	"testing"
)

func TestBOMMappingProfileApprovalAndVersionConflict(t *testing.T) {
	s := integrationStore(t)
	ctx := context.Background()
	const password = "test-only-bom-mapping-password"
	if _, e := s.CreateUser(ctx, "bom-admin", "BOM Admin", password, true, true); e != nil {
		t.Fatal(e)
	}
	_, actor := testLogin(t, s, "bom-admin", password, "198.51.100.90")
	project, e := s.CreateProject(ctx, actor, CreateProjectInput{Name: "BOM mapping", Slug: "bom-mapping"}, "new-bom-project", "test")
	if e != nil {
		t.Fatal(e)
	}
	profile := testBOMProfile()
	input := CreateBOMMappingProfileInput{VersionRef: "profile-v1", ServerColumns: profile.ServerColumns, FabricColumns: profile.FabricColumns}
	draft, e := s.CreateBOMMappingProfile(ctx, actor, project, input, "test")
	if e != nil {
		t.Fatal(e)
	}
	if draft.State != "draft" {
		t.Fatal("new mapping was auto-approved")
	}
	if _, e = s.GetApprovedBOMMappingProfile(ctx, actor, project, input.VersionRef); e == nil {
		t.Fatal("draft mapping accepted for import")
	}
	approved, e := s.ApproveBOMMappingProfile(ctx, actor, project, input.VersionRef, "test")
	if e != nil {
		t.Fatal(e)
	}
	if approved.State != "approved" || approved.ApprovedAt == nil {
		t.Fatal("mapping approval not recorded")
	}
	got, e := s.GetApprovedBOMMappingProfile(ctx, actor, project, input.VersionRef)
	if e != nil {
		t.Fatal(e)
	}
	if got.ID != draft.ID {
		t.Fatal("approval changed mapping identity")
	}
	if _, e = s.ApproveBOMMappingProfile(ctx, actor, project, input.VersionRef, "test"); e == nil {
		t.Fatal("second approval accepted")
	}
	if _, e = s.CreateBOMMappingProfile(ctx, actor, project, input, "test"); e == nil {
		t.Fatal("duplicate version accepted")
	}
}
