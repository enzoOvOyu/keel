package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func TestMediaArtifactDependencyInput(t *testing.T) {
	ref := MediaArtifactDependency{MediaID: strings.Repeat("a", 32), SHA256: strings.Repeat("b", 64)}
	raw, _ := json.Marshal(map[string]any{"artifacts": []MediaArtifactDependency{ref}, "compatibility": map[string]any{"source": "fixture", "version": "1", "report_ref": "fixture:report", "requires": map[string][]string{"cpu.arch": {"amd64"}}}})
	if _, err := decodeCompatibility(raw); err != nil {
		t.Fatal(err)
	}
	duplicate, _ := json.Marshal(map[string]any{"artifacts": []MediaArtifactDependency{ref, ref}})
	oversized := make([]MediaArtifactDependency, 33)
	for i := range oversized {
		oversized[i] = MediaArtifactDependency{MediaID: fmt.Sprintf("%032x", i), SHA256: ref.SHA256}
	}
	large, _ := json.Marshal(map[string]any{"artifacts": oversized})
	for _, raw := range []string{string(duplicate), string(large), `{"artifacts":[{"media_id":"latest","sha256":"auto"}]}`, `{"artifacts":[{"media_id":"` + ref.MediaID + `","sha256":"` + ref.SHA256 + `","unknown":true}]}`, `{"artifacts":"auto"}`} {
		if _, err := decodeCompatibility(json.RawMessage(raw)); err == nil {
			t.Fatalf("accepted invalid dependencies %s", raw)
		}
	}
	if err := validateParamInput(UpsertParamKeyInput{Key: "test.key", ValueType: "string", Deps: map[string]any{"artifacts": []any{}}}); err == nil {
		t.Fatal("parameter accepted artifact dependencies")
	}
}

func TestMediaArtifactDependencies(t *testing.T) {
	s := integrationStore(t)
	ctx := context.Background()
	objects := &testMediaObjects{}
	s.MediaObjects = objects
	const password = "test-only-dependencies-password"
	for i, name := range []string{"dependency-owner", "dependency-reviewer"} {
		if _, err := s.CreateUser(ctx, name, name, password, true, i == 0); err != nil {
			t.Fatal(err)
		}
	}
	_, owner := testLogin(t, s, "dependency-owner", password, "198.51.100.123")
	_, reviewer := testLogin(t, s, "dependency-reviewer", password, "198.51.100.124")
	project, err := s.CreateProject(ctx, owner, CreateProjectInput{Name: "Dependency project", Slug: "dependency-project"}, "deps-project", "test")
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.CreateProject(ctx, owner, CreateProjectInput{Name: "Other project", Slug: "dependency-other"}, "deps-other", "test")
	if err != nil {
		t.Fatal(err)
	}
	deps := func(items ...*MediaItemSummary) map[string]any {
		refs := []MediaArtifactDependency{}
		for _, m := range items {
			refs = append(refs, MediaArtifactDependency{MediaID: m.ID, SHA256: m.Sha256, Name: "forged display name", Version: "latest"})
		}
		return map[string]any{"artifacts": refs}
	}
	upload := func(p *ProjectSummary, name, arch string, dependencies map[string]any, approve bool) *MediaItemSummary {
		t.Helper()
		m, e := s.IngestMedia(ctx, owner, p, RegisterMediaInput{Type: "driver", Name: name, Version: "v1", Arch: arch, Deps: dependencies}, bytes.NewBufferString(name+" bytes"), "test")
		if e != nil {
			t.Fatal(e)
		}
		if approve {
			for _, state := range []string{"verified", "approved"} {
				if _, e = s.TransitionMedia(ctx, reviewer, p, m.ID, state, "test"); e != nil {
					t.Fatal(e)
				}
			}
		}
		return m
	}
	global := upload(nil, "global-runtime", "noarch", nil, true)
	leaf := upload(project, "leaf-runtime", "noarch", deps(global), true)
	middle := upload(project, "middle-driver", "amd64", deps(leaf), true)
	parent := upload(project, "parent-driver", "amd64", deps(middle), true)
	refs, e := mediaArtifactDependencies(parent.Deps)
	if e != nil || len(refs) != 1 || refs[0].MediaID != middle.ID || refs[0].Version != "v1" || refs[0].Name != middle.Name {
		t.Fatalf("untrusted display metadata %+v %v", refs, e)
	}
	download := func(code string) {
		t.Helper()
		f, _, e := s.DownloadMedia(ctx, reviewer, project, parent.ID, "test")
		if code != "" {
			if f != nil {
				f.Close()
				os.Remove(f.Name())
			}
			assertErrorCode(t, e, code)
			return
		}
		if e != nil {
			t.Fatal(e)
		}
		defer os.Remove(f.Name())
		defer f.Close()
		b, e := io.ReadAll(f)
		if e != nil || string(b) != parent.Name+" bytes" {
			t.Fatalf("download %q %v", b, e)
		}
	}
	download("")
	for _, in := range []UpsertParamKeyInput{{Key: "driver.version", ValueType: "version", SourceMediaType: "driver"}} {
		if _, e = s.UpsertParamKey(ctx, owner, project, in, "test"); e != nil {
			t.Fatal(e)
		}
	}
	// Use a distinct version so the catalog has exactly one matching immutable identity.
	catalogParent, e := s.IngestMedia(ctx, owner, project, RegisterMediaInput{Type: "driver", Name: "catalog-parent", Version: "catalog-v1", Arch: "amd64", Deps: deps(middle)}, bytes.NewBufferString("catalog parent bytes"), "test")
	if e != nil {
		t.Fatal(e)
	}
	for _, state := range []string{"verified", "approved"} {
		if _, e = s.TransitionMedia(ctx, reviewer, project, catalogParent.ID, state, "test"); e != nil {
			t.Fatal(e)
		}
	}
	values := map[string]any{"driver.version": "catalog-v1", "cpu.arch": "amd64"}
	selection, e := s.ValidateCatalog(ctx, owner, project, values)
	if e != nil || !selection.Valid {
		t.Fatalf("valid closure rejected %+v %v", selection, e)
	}
	verify := func(code string) {
		t.Helper()
		tx, e := s.DB.BeginTx(ctx, nil)
		if e != nil {
			t.Fatal(e)
		}
		defer tx.Rollback()
		e = s.VerifyCatalogArtifactsTx(ctx, tx, selection.Selections)
		if code == "" {
			if e != nil {
				t.Fatal(e)
			}
		} else {
			assertErrorCode(t, e, code)
		}
	}
	verify("")

	// Admission holds every transitive dependency through the enclosing commit,
	// including a global leaf: concurrent retirement cannot slip into that window.
	admission, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.VerifyCatalogArtifactsTx(ctx, admission, selection.Selections); e != nil {
		admission.Rollback()
		t.Fatal(e)
	}
	retireCtx, cancelRetire := context.WithTimeout(ctx, time.Second)
	_, retireErr := s.TransitionMedia(retireCtx, reviewer, nil, global.ID, "retired", "test-concurrent-retire")
	deadline := retireCtx.Err()
	cancelRetire()
	admission.Rollback()
	if retireErr == nil || deadline != context.DeadlineExceeded {
		t.Fatalf("retirement was not held behind admission: error=%v context=%v", retireErr, deadline)
	}
	var globalState string
	if e = s.DB.QueryRowContext(ctx, "SELECT state FROM media_items WHERE id=$1", global.ID).Scan(&globalState); e != nil || globalState != "approved" {
		t.Fatalf("blocked retirement changed dependency: %s %v", globalState, e)
	}
	var retireAudits int
	if e = s.DB.QueryRowContext(ctx, "SELECT count(*) FROM audits WHERE target_id=$1 AND action='media.retired'", global.ID).Scan(&retireAudits); e != nil || retireAudits != 0 {
		t.Fatalf("blocked retirement wrote success audit: %d %v", retireAudits, e)
	}
	// Pinned bytes of a transitive global dependency, not only the parent, are rechecked.
	var objectKey, objectVersion string
	if e = s.DB.QueryRowContext(ctx, "SELECT object_key,object_version FROM media_artifacts WHERE media_id=$1", global.ID).Scan(&objectKey, &objectVersion); e != nil {
		t.Fatal(e)
	}
	objects.mu.Lock()
	saved := append([]byte{}, objects.data[objectKey+":"+objectVersion]...)
	objects.data[objectKey+":"+objectVersion] = []byte("tampered transitive bytes")
	objects.mu.Unlock()
	download("MEDIA_DIGEST_MISMATCH")
	verify("MEDIA_DIGEST_MISMATCH")
	unapproved := upload(project, "blocked-parent", "amd64", deps(middle), false)
	if _, e = s.TransitionMedia(ctx, reviewer, project, unapproved.ID, "verified", "test"); e == nil {
		t.Fatal("verification ignored corrupted transitive dependency")
	} else {
		assertErrorCode(t, e, "MEDIA_DIGEST_MISMATCH")
	}
	objects.mu.Lock()
	objects.data[objectKey+":"+objectVersion] = saved
	objects.mu.Unlock()
	download("")
	verify("")
	// Cross-project/global-to-private, approval, arch and digest failures never create a parent.
	foreign := upload(other, "foreign-leaf", "amd64", nil, true)
	pending := upload(project, "pending-leaf", "amd64", nil, false)
	arm := upload(project, "arm-leaf", "arm64", nil, true)
	mismatch := deps(middle)
	mismatch["artifacts"] = []MediaArtifactDependency{{MediaID: middle.ID, SHA256: strings.Repeat("0", 64)}}
	missing := map[string]any{"artifacts": []MediaArtifactDependency{{MediaID: RandomID(), SHA256: middle.Sha256}}}
	for i, tc := range []struct {
		scope        *ProjectSummary
		arch         string
		dependencies map[string]any
		code         string
	}{{project, "amd64", deps(foreign), "MEDIA_DEPENDENCY_UNAVAILABLE"}, {nil, "amd64", deps(middle), "MEDIA_DEPENDENCY_UNAVAILABLE"}, {project, "amd64", deps(pending), "MEDIA_DEPENDENCY_UNAVAILABLE"}, {project, "amd64", deps(arm), "MEDIA_DEPENDENCY_ARCH_MISMATCH"}, {project, "noarch", deps(middle), "MEDIA_DEPENDENCY_ARCH_MISMATCH"}, {project, "amd64", mismatch, "MEDIA_DEPENDENCY_IDENTITY_MISMATCH"}, {project, "amd64", missing, "MEDIA_DEPENDENCY_UNAVAILABLE"}} {
		_, e = s.RegisterMedia(ctx, owner, tc.scope, RegisterMediaInput{Type: "tool", Name: fmt.Sprintf("rejected-%d", i), Version: "v1", Arch: tc.arch, Sha256: middle.Sha256, Size: 1, Deps: tc.dependencies}, "test")
		assertErrorCode(t, e, tc.code)
	}
	var rejected int
	if e = s.DB.QueryRowContext(ctx, "SELECT count(*) FROM media_items WHERE name LIKE 'rejected-%'").Scan(&rejected); e != nil || rejected != 0 {
		t.Fatalf("failed registration persisted %d %v", rejected, e)
	}

	// Corrupt historical metadata must fail closed even if normal registration cannot create cycles.
	cycleTx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	cycleDeps, _ := json.Marshal(deps(parent))
	if _, e = cycleTx.ExecContext(ctx, "UPDATE media_items SET deps=$2 WHERE id=$1", middle.ID, cycleDeps); e != nil {
		cycleTx.Rollback()
		t.Fatal(e)
	}
	e = checkPinnedMediaDependencies(ctx, cycleTx, parent.ID, nil)
	cycleTx.Rollback()
	assertErrorCode(t, e, "MEDIA_DEPENDENCY_CYCLE")
	// A shared leaf visited first cannot hide an excessively deep alternate path.
	shared := upload(nil, "bounded-shared", "noarch", deps(global), true)
	chain := shared
	for i := 0; i < 15; i++ {
		chain = upload(nil, fmt.Sprintf("bounded-chain-%d", i), "noarch", deps(chain), true)
	}
	_, e = s.RegisterMedia(ctx, owner, nil, RegisterMediaInput{Type: "tool", Name: "too-deep-shared", Version: "v1", Arch: "noarch", Sha256: chain.Sha256, Size: 1, Deps: deps(shared, chain)}, "test")
	assertErrorCode(t, e, "MEDIA_DEPENDENCY_LIMIT")
	// A later registered version does not silently replace any pinned edge.
	v2, e := s.IngestMedia(ctx, owner, project, RegisterMediaInput{Type: "driver", Name: middle.Name, Version: "v2", Arch: "amd64"}, bytes.NewBufferString("new version bytes"), "test")
	if e != nil {
		t.Fatal(e)
	}
	for _, state := range []string{"verified", "approved"} {
		if _, e = s.TransitionMedia(ctx, reviewer, project, v2.ID, state, "test"); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = s.TransitionMedia(ctx, reviewer, project, leaf.ID, "retired", "test"); e != nil {
		t.Fatal(e)
	}
	download("MEDIA_DEPENDENCY_UNAVAILABLE")
	verify("MEDIA_DEPENDENCY_UNAVAILABLE")
	result, e := s.ValidateCatalog(ctx, owner, project, values)
	if e != nil || result.Valid || len(result.Issues) != 1 || result.Issues[0].Code != "MEDIA_DEPENDENCY_UNAVAILABLE" {
		t.Fatalf("retired transitive dependency admitted %+v %v", result, e)
	}
	if _, e = s.TransitionMedia(ctx, reviewer, project, unapproved.ID, "verified", "test"); e == nil {
		t.Fatal("retired closure verified")
	} else {
		assertErrorCode(t, e, "MEDIA_DEPENDENCY_UNAVAILABLE")
	}
	// Retirement of the parent itself must remain possible even when dependencies fail.
	if _, e = s.TransitionMedia(ctx, reviewer, project, parent.ID, "retired", "test"); e != nil {
		t.Fatal(e)
	}
}
