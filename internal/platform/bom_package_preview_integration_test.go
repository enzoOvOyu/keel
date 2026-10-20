package platform

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestBOMPackagePreviewIsReadOnlyAndDetectsBatchConflict(t *testing.T) {
	s := integrationStore(t)
	ctx := context.Background()
	const password = "test-only-bom-preview-password"
	if _, err := s.CreateUser(ctx, "bom-preview-admin", "BOM Preview Admin", password, true, true); err != nil {
		t.Fatal(err)
	}
	_, actor := testLogin(t, s, "bom-preview-admin", password, "198.51.100.91")
	project, err := s.CreateProject(ctx, actor, CreateProjectInput{Name: "BOM preview", Slug: "p1"}, "new-bom-preview-project", "test")
	if err != nil {
		t.Fatal(err)
	}
	profile := testBOMProfile()
	input := CreateBOMMappingProfileInput{VersionRef: "profile-v1", ServerColumns: profile.ServerColumns, FabricColumns: profile.FabricColumns}
	if _, err = s.CreateBOMMappingProfile(ctx, actor, project, input, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ApproveBOMMappingProfile(ctx, actor, project, input.VersionRef, "test"); err != nil {
		t.Fatal(err)
	}
	files := bomFixture(t)
	var manifest BOMPackageManifest
	if err = json.Unmarshal(files["manifest.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.ProjectID = project.ID
	files["manifest.json"], _ = json.Marshal(manifest)
	preview, err := s.PreviewBOMPackage(ctx, actor, project, files)
	if err != nil {
		t.Fatal(err)
	}
	if preview.CanCommit || !issueFound(&BOMPackage{Issues: preview.Issues}, "model_version_not_found") {
		t.Fatalf("missing models did not block preview: %+v", preview.Issues)
	}
	var count int
	if err = s.DB.QueryRowContext(ctx, `SELECT count(*) FROM bom_import_packages`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("preview wrote package row: %d %v", count, err)
	}
	id := RandomID()
	_, err = s.DB.ExecContext(ctx, `INSERT INTO bom_import_packages(id,project_id,bom_import_batch_id,schema_version,profile_version_ref,content_hash,declared_by,manifest,row_count,state,uploaded_by,committed_at) VALUES($1,$2,$3,'bom-import/v1',$4,$5,'design_institute','{}',0,'committed',$6,$7)`, id, project.ID, manifest.BatchID, manifest.ProfileVersionRef, "sha256:"+string(make([]byte, 64)), actor.User.ID, time.Now())
	if err == nil {
		t.Fatal("test accepted invalid hash")
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO bom_import_packages(id,project_id,bom_import_batch_id,schema_version,profile_version_ref,content_hash,declared_by,manifest,row_count,state,uploaded_by,committed_at) VALUES($1,$2,$3,'bom-import/v1',$4,$5,'design_institute','{}',0,'committed',$6,$7)`, id, project.ID, manifest.BatchID, manifest.ProfileVersionRef, "sha256:"+"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", actor.User.ID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	preview, err = s.PreviewBOMPackage(ctx, actor, project, files)
	if err != nil {
		t.Fatal(err)
	}
	if !issueFound(&BOMPackage{Issues: preview.Issues}, "batch_hash_conflict") {
		t.Fatal("same batch with different hash was accepted")
	}
}
