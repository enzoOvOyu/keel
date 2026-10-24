package platform

import (
	"bytes"
	"context"
	"os"
	"testing"
)

func TestGlobalCatalogScopeBoundaries(t *testing.T) {
	s := integrationStore(t)
	s.MediaObjects = &testMediaObjects{} // Object transport only; scope/auth/lifecycle use PostgreSQL.
	ctx := context.Background()
	const password = "isolated-global-catalog-password"
	for i, name := range []string{"global-owner", "global-reviewer", "project-admin"} {
		if _, e := s.CreateUser(ctx, name, name, password, i < 2, i == 0); e != nil {
			t.Fatal(e)
		}
	}
	_, owner := testLogin(t, s, "global-owner", password, "198.51.100.130")
	_, reviewer := testLogin(t, s, "global-reviewer", password, "198.51.100.131")
	p, e := s.CreateProject(ctx, owner, CreateProjectInput{Name: "Global isolation", Slug: "global-isolation"}, "global-isolation", "test")
	if e != nil {
		t.Fatal(e)
	}
	if e = s.SetMembership(ctx, "project-admin", p.ID, "Admin", false); e != nil {
		t.Fatal(e)
	}
	_, localAdmin := testLogin(t, s, "project-admin", password, "198.51.100.132")
	page := Pagination{Page: 1, PageSize: 100}
	if _, e = s.ListParamKeys(ctx, localAdmin, nil, page, ""); e == nil {
		t.Fatal("project admin read global operator catalog")
	} else {
		assertErrorCode(t, e, "FORBIDDEN")
	}
	if _, e = s.ListMedia(ctx, localAdmin, nil, page); e == nil {
		t.Fatal("project admin read global operator media")
	} else {
		assertErrorCode(t, e, "FORBIDDEN")
	}
	globalInput := UpsertParamKeyInput{Key: "runtime.shared", ValueType: "enum", Options: []string{"global-a", "global-b"}, DefaultValue: "global-a", CreateOnly: true}
	if _, e = s.UpsertParamKey(ctx, localAdmin, nil, globalInput, "test"); e == nil {
		t.Fatal("project admin changed global catalog")
	} else {
		assertErrorCode(t, e, "FORBIDDEN")
	}
	global, e := s.UpsertParamKey(ctx, owner, nil, globalInput, "test")
	if e != nil {
		t.Fatal(e)
	}
	inherited, e := s.ListParamKeys(ctx, localAdmin, p, page, "")
	if e != nil || len(inherited.Items) != 1 || inherited.Items[0].ID != global.ID {
		t.Fatalf("global inheritance: %+v %v", inherited, e)
	}
	local, e := s.UpsertParamKey(ctx, localAdmin, p, UpsertParamKeyInput{Key: globalInput.Key, ValueType: "enum", Options: []string{"local"}, DefaultValue: "local"}, "test")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.RetireParamKey(ctx, owner, nil, local.ID, "test"); e == nil {
		t.Fatal("global scope retired a project definition")
	} else {
		assertErrorCode(t, e, "PARAM_KEY_NOT_FOUND")
	}
	if _, e = s.RetireParamKey(ctx, localAdmin, nil, global.ID, "test"); e == nil {
		t.Fatal("project admin retired global definition")
	} else {
		assertErrorCode(t, e, "FORBIDDEN")
	}
	globalInput.CreateOnly = false
	globalInput.ExpectedUpdatedAt = &global.UpdatedAt
	globalInput.DefaultValue = "global-b"
	updated, e := s.UpsertParamKey(ctx, owner, nil, globalInput, "test")
	if e != nil || !updated.CreatedAt.Equal(global.CreatedAt) {
		t.Fatalf("global update: %+v %v", updated, e)
	}
	if _, e = s.UpsertParamKey(ctx, reviewer, nil, globalInput, "test"); e == nil {
		t.Fatal("stale global edit accepted")
	} else {
		assertErrorCode(t, e, "PARAM_KEY_CONFLICT")
	}
	if _, e = s.RetireParamKeyChecked(ctx, owner, nil, global.ID, &global.UpdatedAt, "test"); e == nil {
		t.Fatal("stale global retirement accepted")
	} else {
		assertErrorCode(t, e, "PARAM_KEY_CONFLICT")
	}
	if _, e = s.RetireParamKeyChecked(ctx, owner, nil, global.ID, &updated.UpdatedAt, "test"); e != nil {
		t.Fatal(e)
	}
	selected, e := s.ValidateCatalog(ctx, localAdmin, p, map[string]any{globalInput.Key: "local"})
	if e != nil || !selected.Valid || selected.Selections[0].KeyID != local.ID {
		t.Fatalf("global retirement changed project override: %+v %v", selected, e)
	}
	// Global version defaults cannot borrow approved project files.
	for _, scope := range []*ProjectSummary{nil, p} {
		version := "global-file"
		if scope != nil {
			version = "project-file"
		}
		m, err := s.IngestMedia(ctx, owner, scope, RegisterMediaInput{Type: "tool", Name: "scope-tool", Version: version, Arch: "noarch"}, bytes.NewBufferString(version), "test")
		if err != nil {
			t.Fatal(err)
		}
		for _, state := range []string{"verified", "approved"} {
			if _, err = s.TransitionMedia(ctx, reviewer, scope, m.ID, state, "test"); err != nil {
				t.Fatal(err)
			}
		}
	}
	globalMedia, err := s.ListMedia(ctx, owner, nil, page)
	if err != nil || len(globalMedia.Items) != 1 {
		t.Fatalf("global media: %+v %v", globalMedia, err)
	}
	globalID := globalMedia.Items[0].ID
	if _, _, err = s.DownloadMedia(ctx, localAdmin, nil, globalID, "test"); err == nil {
		t.Fatal("project admin downloaded from the global administration boundary")
	} else {
		assertErrorCode(t, err, "FORBIDDEN")
	}
	file, _, err := s.DownloadMedia(ctx, localAdmin, p, globalID, "test")
	if err != nil {
		t.Fatal("project member cannot consume approved global artifact", err)
	}
	file.Close()
	os.Remove(file.Name())
	versionInput := UpsertParamKeyInput{Key: "tool.version", ValueType: "version", SourceMediaType: "tool", DefaultValue: "project-file"}
	if _, e = s.UpsertParamKey(ctx, owner, nil, versionInput, "test"); e == nil {
		t.Fatal("project artifact became a global default")
	} else {
		assertErrorCode(t, e, "VALIDATION_ERROR")
	}
	versionInput.DefaultValue = "global-file"
	if _, e = s.UpsertParamKey(ctx, owner, nil, versionInput, "test"); e != nil {
		t.Fatal(e)
	}
	media, e := s.ListMedia(ctx, owner, nil, page)
	if e != nil || media.Total != 1 || media.Items[0].Version != "global-file" {
		t.Fatalf("global media scope: %+v %v", media, e)
	}
	all, e := s.ListParamKeysIncludingRetired(ctx, owner, nil, page, "", true)
	if e != nil || all.Total != 2 {
		t.Fatalf("global management list: %+v %v", all, e)
	}
	for _, key := range all.Items {
		if key.ScopeKey != "global" {
			t.Fatal("global catalog contains project definition")
		}
	}
}
