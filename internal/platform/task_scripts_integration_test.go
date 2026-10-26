package platform

import (
	"bytes"
	"context"
	"testing"
)

func TestApprovedTaskScriptArtifactsPostgres(t *testing.T) {
	s := integrationStore(t)
	ctx := context.Background()
	objects := &testMediaObjects{}
	s.MediaObjects = objects
	const password = "test-only-script-artifact-password"
	for _, name := range []string{"script-owner", "script-reviewer"} {
		if _, err := s.CreateUser(ctx, name, name, password, true, true); err != nil {
			t.Fatal(err)
		}
	}
	_, owner := testLogin(t, s, "script-owner", password, "198.51.100.180")
	_, reviewer := testLogin(t, s, "script-reviewer", password, "198.51.100.181")
	project, err := s.CreateProject(ctx, owner, CreateProjectInput{Name: "Script project", Slug: "script-project"}, "script-project", "test")
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.CreateProject(ctx, owner, CreateProjectInput{Name: "Other script project", Slug: "other-script-project"}, "other-script-project", "test")
	if err != nil {
		t.Fatal(err)
	}
	upload := func(scope *ProjectSummary, name, arch, osScope, manifest string, approve bool) *MediaItemSummary {
		t.Helper()
		deps := map[string]any{}
		if arch == "noarch" && osScope == "linux" && (manifest == "keel.task-script/shell/v1" || manifest == "keel.task-script/python/v1") {
			interpreter := "shell"
			if manifest == "keel.task-script/python/v1" {
				interpreter = "python"
			}
			deps["task_script"] = MediaTaskScriptProfile{Version: "keel.task-script/v1", Interpreter: interpreter}
		}
		item, err := s.IngestMedia(ctx, owner, scope, RegisterMediaInput{Type: "tool", Name: name, Version: "v1", Arch: arch, OsScope: osScope, ManifestRef: manifest, Deps: deps}, bytes.NewBufferString("printf fixture\n"), "test")
		if err != nil {
			t.Fatal(err)
		}
		if len(deps) > 0 && item.ManifestRef != "artifact:sha256:"+item.Sha256 {
			t.Fatal("script profile replaced upload provenance")
		}
		if approve {
			for _, state := range []string{"verified", "approved"} {
				item, err = s.TransitionMedia(ctx, reviewer, scope, item.ID, state, "test")
				if err != nil {
					t.Fatal(err)
				}
			}
		}
		return item
	}
	local := upload(project, "local-script", "noarch", "linux", "keel.task-script/shell/v1", true)
	global := upload(nil, "global-script", "noarch", "linux", "keel.task-script/python/v1", true)
	unapproved := upload(project, "unapproved-script", "noarch", "linux", "keel.task-script/shell/v1", false)
	_ = upload(project, "architecture-specific", "amd64", "linux", "keel.task-script/shell/v1", true)
	_ = upload(project, "wrong-os", "noarch", "other", "keel.task-script/shell/v1", true)
	_ = upload(project, "generic-tool", "noarch", "linux", "tool-generic/v1", true)
	page, enabled, err := s.ListTaskScripts(ctx, owner, project, Pagination{Page: 1, PageSize: 1})
	if err != nil || enabled || len(page.Items) != 0 {
		t.Fatal("disabled script catalog", err)
	}
	s.SSHScriptsEnabled = true
	page, enabled, err = s.ListTaskScripts(ctx, owner, project, Pagination{Page: 1, PageSize: 1})
	if err != nil || !enabled || page.Total != 2 || len(page.Items) != 1 || page.Items[0].MediaID != global.ID {
		t.Fatal("script catalog filtering or pagination", page, err)
	}
	if _, _, err = s.ListTaskScripts(ctx, nil, project, Pagination{Page: 1, PageSize: 20}); err == nil {
		t.Fatal("anonymous script catalog read")
	}
	resolve := func(project, media string, frozen *TaskScriptArtifact, withBytes bool) (*TaskScriptArtifact, []byte, error) {
		t.Helper()
		tx, err := s.DB.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		return s.ResolveTaskScriptTx(ctx, tx, project, media, frozen, withBytes)
	}
	frozen, body, err := resolve(project.ID, local.ID, nil, true)
	if err != nil || string(body) != "printf fixture\n" || frozen.SHA256 != local.Sha256 || frozen.Version != "v1" || frozen.Interpreter != "shell" {
		t.Fatal("exact approved script artifact", err)
	}
	s.MediaObjects = nil
	if _, body, err = resolve(project.ID, local.ID, frozen, false); err != nil || body != nil {
		t.Fatal("metadata read fetched objects", err)
	}
	if _, _, err = resolve(project.ID, local.ID, frozen, true); err == nil {
		t.Fatal("script bytes without object storage")
	}
	s.MediaObjects = objects
	changed := *frozen
	changed.ObjectVersion = "different-object"
	if _, _, err = resolve(project.ID, local.ID, &changed, false); err == nil {
		t.Fatal("frozen artifact identity changed")
	}
	if _, _, err = resolve(other.ID, local.ID, nil, false); err == nil {
		t.Fatal("project script crossed scope")
	}
	if _, _, err = resolve(other.ID, global.ID, nil, false); err != nil {
		t.Fatal("global approved script unavailable", err)
	}
	if _, _, err = resolve(project.ID, unapproved.ID, nil, false); err == nil {
		t.Fatal("unapproved script admitted")
	}
	if _, err = s.TransitionMedia(ctx, reviewer, project, local.ID, "retired", "test"); err != nil {
		t.Fatal(err)
	}
	if _, _, err = resolve(project.ID, local.ID, frozen, false); err == nil {
		t.Fatal("retired frozen script still admissible")
	}
}
