package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/enzoOvOyu/keel/internal/mediamanifest"
	"path/filepath"
	"testing"
)

func TestSignedMediaManifest(t *testing.T) {
	s := integrationStore(t)
	ctx := context.Background()
	objects := &testMediaObjects{}
	s.MediaObjects = objects
	dir := t.TempDir()
	private, public := filepath.Join(dir, "private.pem"), filepath.Join(dir, "public.pem")
	if _, err := mediamanifest.GenerateKeyFiles(private, public); err != nil {
		t.Fatal(err)
	}
	signer, err := mediamanifest.LoadSigner(private)
	if err != nil {
		t.Fatal(err)
	}
	trusted, err := mediamanifest.LoadPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	const password = "manifest-integration-password"
	for i, name := range []string{"manifest-owner", "manifest-reviewer"} {
		if _, err = s.CreateUser(ctx, name, name, password, true, i == 0); err != nil {
			t.Fatal(err)
		}
	}
	_, owner := testLogin(t, s, "manifest-owner", password, "198.51.100.130")
	_, reviewer := testLogin(t, s, "manifest-reviewer", password, "198.51.100.131")
	project, err := s.CreateProject(ctx, owner, CreateProjectInput{Name: "Manifest project", Slug: "manifest-project"}, "manifest-project", "test")
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.CreateProject(ctx, owner, CreateProjectInput{Name: "Other project", Slug: "manifest-other"}, "manifest-other", "test")
	if err != nil {
		t.Fatal(err)
	}
	upload := func(scope *ProjectSummary, name, version string, dependencies ...*MediaItemSummary) *MediaItemSummary {
		t.Helper()
		refs := []MediaArtifactDependency{}
		for _, dep := range dependencies {
			refs = append(refs, MediaArtifactDependency{MediaID: dep.ID, SHA256: dep.Sha256})
		}
		item, e := s.IngestMedia(ctx, owner, scope, RegisterMediaInput{Type: "tool", Name: name, Version: version, Arch: "noarch", Deps: map[string]any{"artifacts": refs}}, bytes.NewBufferString(name+version+" payload"), "test")
		if e != nil {
			t.Fatal(e)
		}
		for _, state := range []string{"verified", "approved"} {
			if _, e = s.TransitionMedia(ctx, reviewer, scope, item.ID, state, "test"); e != nil {
				t.Fatal(e)
			}
		}
		return item
	}
	leaf := upload(nil, "global-leaf", "v1")
	middle := upload(project, "private-middle", "v1", leaf)
	root := upload(project, "manifest-root", "v1", middle)
	_, err = s.ExportMediaManifest(ctx, owner, project, root.ID, "test-disabled")
	assertErrorCode(t, err, "MEDIA_SIGNING_UNAVAILABLE")
	s.MediaManifestSigner = signer
	manifest, err := s.ExportMediaManifest(ctx, owner, project, root.ID, "test-manifest")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	verified, err := mediamanifest.Verify(raw, trusted)
	if err != nil {
		t.Fatal(err)
	}
	if verified.Payload.RootID != root.ID || len(verified.Payload.Artifacts) != 3 {
		t.Fatalf("incomplete closure: %+v", verified.Payload)
	}
	for _, node := range verified.Payload.Artifacts {
		if node.ObjectVersion != "v1" || node.ApprovedBy != reviewer.User.ID || node.ApprovedAt.IsZero() {
			t.Fatalf("missing immutable provenance: %+v", node)
		}
	}
	if bytes.Contains(raw, []byte("object_key")) || bytes.Contains(raw, []byte("private.pem")) {
		t.Fatal("manifest leaked storage or key locations")
	}
	// A later version cannot silently upgrade this signed dependency identity.
	upload(nil, "global-leaf", "v2")
	again, err := s.ExportMediaManifest(ctx, owner, project, root.ID, "test-v2")
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range again.Payload.Artifacts {
		if node.Name == leaf.Name && (node.MediaID != leaf.ID || node.Version != "v1") {
			t.Fatal("dependency silently upgraded")
		}
	}
	_, err = s.ExportMediaManifest(ctx, owner, other, root.ID, "test-wrong-scope")
	if err == nil {
		t.Fatal("cross-project manifest exported")
	}
	_, err = s.ExportMediaManifest(ctx, owner, nil, root.ID, "test-wrong-global")
	if err == nil {
		t.Fatal("private artifact exported globally")
	}
	if _, err = s.ExportMediaManifest(ctx, owner, project, leaf.ID, "test-inherited"); err != nil {
		t.Fatal(err)
	}
	auditCount := func() int {
		t.Helper()
		var n int
		if e := s.DB.QueryRowContext(ctx, "SELECT count(*) FROM audits WHERE action='media.manifest.export'").Scan(&n); e != nil {
			t.Fatal(e)
		}
		return n
	}
	before := auditCount()
	var key, version string
	if err = s.DB.QueryRowContext(ctx, "SELECT object_key,object_version FROM media_artifacts WHERE media_id=$1", leaf.ID).Scan(&key, &version); err != nil {
		t.Fatal(err)
	}
	objects.mu.Lock()
	saved := append([]byte{}, objects.data[key+":"+version]...)
	objects.data[key+":"+version] = []byte("corrupted")
	objects.mu.Unlock()
	_, err = s.ExportMediaManifest(ctx, owner, project, root.ID, "test-corrupt")
	assertErrorCode(t, err, "MEDIA_DIGEST_MISMATCH")
	objects.mu.Lock()
	objects.data[key+":"+version] = saved
	objects.mu.Unlock()
	if auditCount() != before {
		t.Fatal("failed export wrote success audit")
	}
	if _, err = s.TransitionMedia(ctx, reviewer, nil, leaf.ID, "retired", "test"); err != nil {
		t.Fatal(err)
	}
	_, err = s.ExportMediaManifest(ctx, owner, project, root.ID, "test-retired-dependency")
	assertErrorCode(t, err, "MEDIA_DEPENDENCY_UNAVAILABLE")
	if auditCount() != before {
		t.Fatal("retired dependency export wrote success audit")
	}
	// Existing signed receipts still verify offline and do not authorize a new export.
	if _, err = mediamanifest.Verify(raw, trusted); err != nil {
		t.Fatal(err)
	}
	if _, err = s.TransitionMedia(ctx, reviewer, project, root.ID, "retired", "test"); err != nil {
		t.Fatal(err)
	}
	_, err = s.ExportMediaManifest(ctx, owner, project, root.ID, "test-retired-root")
	assertErrorCode(t, err, "MEDIA_NOT_APPROVED")
}
