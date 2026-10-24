package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
)

func TestCatalogArtifactAdmission(t *testing.T) {
	s := integrationStore(t)
	s.MediaObjects = &testMediaObjects{}
	ctx := context.Background()
	const password = "test-only-catalog-review-password"
	for i, name := range []string{"catalog-owner", "catalog-reviewer"} {
		if _, e := s.CreateUser(ctx, name, name, password, true, i == 0); e != nil {
			t.Fatal(e)
		}
	}
	_, a := testLogin(t, s, "catalog-owner", password, "198.51.100.110")
	_, reviewer := testLogin(t, s, "catalog-reviewer", password, "198.51.100.111")
	p, e := s.CreateProject(ctx, a, CreateProjectInput{Name: "Catalog admission", Slug: "catalog-admission"}, "catalog-project", "test")
	if e != nil {
		t.Fatal(e)
	}
	for _, in := range []UpsertParamKeyInput{{Key: "driver.version", ValueType: "version", SourceMediaType: "driver"}, {Key: "os.kernel", ValueType: "enum", Options: []string{"6.8.0-52", "6.8.0-53"}}} {
		if _, e = s.UpsertParamKey(ctx, a, p, in, "test"); e != nil {
			t.Fatal(e)
		}
	}
	matrix := map[string]any{"compatibility": map[string]any{"source": "fixture-report", "version": "matrix-1", "report_ref": "report:fixture", "requires": map[string][]string{"os.kernel": {"6.8.0-52"}, "cpu.arch": {"amd64"}}}}
	m, e := s.IngestMedia(ctx, a, p, RegisterMediaInput{Type: "driver", Name: "fixture-driver", Version: "reviewed-1", Arch: "amd64", Deps: matrix}, bytes.NewBufferString("driver fixture"), "test")
	if e != nil {
		t.Fatal(e)
	}
	for _, state := range []string{"verified", "approved"} {
		if _, e = s.TransitionMedia(ctx, reviewer, p, m.ID, state, "test"); e != nil {
			t.Fatal(e)
		}
	}
	// Historical approved metadata without bytes cannot re-enter the catalog.
	if _, e = s.DB.ExecContext(ctx, `INSERT INTO media_items(id,scope_key,type,name,version,sha256,size,arch,os_scope,hw_scope,deps,manifest_ref,state,uploaded_by,approved_by,created_at,updated_at) VALUES($1,$2,'driver','historical','legacy',$3,0,'amd64','','','{}','legacy','approved',$4,$5,now(),now())`, RandomID(), "project:"+p.ID, m.Sha256, a.User.ID, reviewer.User.ID); e != nil {
		t.Fatal(e)
	}
	page, e := s.ListParamKeys(ctx, a, p, Pagination{Page: 1, PageSize: 20}, "amd64")
	if e != nil {
		t.Fatal(e)
	}
	var driver ParamKeySummary
	for _, key := range page.Items {
		if key.Key == "driver.version" {
			driver = key
		}
	}
	var options []string
	json.Unmarshal(driver.Options, &options)
	if len(options) != 1 || options[0] != "reviewed-1" || len(driver.MediaOptions) != 1 {
		t.Fatalf("invalid options %+v", driver)
	}
	for _, tc := range []struct {
		values map[string]any
		code   string
	}{
		{map[string]any{"driver.version": "reviewed-1"}, "PARAM_ARCH_REQUIRED"},
		{map[string]any{"driver.version": "reviewed-1", "cpu.arch": "arm64"}, "PARAM_VALUE_REJECTED"},
		{map[string]any{"driver.version": "legacy", "cpu.arch": "amd64"}, "PARAM_VALUE_REJECTED"},
		{map[string]any{"driver.version": "reviewed-1", "cpu.arch": "amd64", "os.kernel": "6.8.0-53"}, "PARAM_DEPENDENCY_REJECTED"},
		{map[string]any{"unknown.version": "v1"}, "PARAM_KEY_NOT_FOUND"},
	} {
		result, e := s.ValidateCatalog(ctx, a, p, tc.values)
		if e != nil {
			t.Fatal(e)
		}
		if result.Valid {
			t.Fatal("invalid combination admitted")
		}
		found := false
		for _, issue := range result.Issues {
			found = found || issue.Code == tc.code
		}
		if !found {
			t.Fatalf("missing %s in %+v", tc.code, result)
		}
	}
	values := map[string]any{"driver.version": "reviewed-1", "cpu.arch": "amd64", "os.kernel": "6.8.0-52"}
	valid, e := s.ValidateCatalog(ctx, a, p, values)
	if e != nil || !valid.Valid {
		t.Fatalf("valid combination %+v %v", valid, e)
	}
	if valid.Selections[0].Media == nil || valid.Selections[0].Media.SHA256 != m.Sha256 || valid.Selections[0].Media.ObjectVersion != "v1" {
		t.Fatal("missing immutable reference")
	}
	// The commit boundary must read the actual pinned object, not just its SQL binding.
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.VerifyCatalogArtifactsTx(ctx, tx, valid.Selections); e != nil {
		tx.Rollback()
		t.Fatal(e)
	}
	tx.Rollback()
	objects := s.MediaObjects.(*testMediaObjects)
	objects.mu.Lock()
	for key := range objects.data {
		objects.data[key] = []byte("corrupt bytes")
	}
	objects.mu.Unlock()
	tx, e = s.DB.BeginTx(ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	e = s.VerifyCatalogArtifactsTx(ctx, tx, valid.Selections)
	tx.Rollback()
	assertErrorCode(t, e, "MEDIA_DIGEST_MISMATCH")
	objects.mu.Lock()
	for key := range objects.data {
		objects.data[key] = []byte("driver fixture")
	}
	objects.mu.Unlock()
	// A project route must never retire a global key; its own retired override is a tombstone.
	global, e := s.UpsertParamKey(ctx, a, nil, UpsertParamKeyInput{Key: "os.kernel", ValueType: "enum", Options: []string{"6.8.0-52"}}, "test")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.RetireParamKey(ctx, a, p, global.ID, "test"); e == nil {
		t.Fatal("project retired global key")
	}
	projectKey, e := s.UpsertParamKey(ctx, a, p, UpsertParamKeyInput{Key: "os.kernel", ValueType: "enum", Options: []string{"6.8.0-52"}}, "test")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.RetireParamKey(ctx, a, p, projectKey.ID, "test"); e != nil {
		t.Fatal(e)
	}
	valid, e = s.ValidateCatalog(ctx, a, p, values)
	if e != nil || valid.Valid {
		t.Fatal("retired project key fell back to global")
	}
	page, e = s.ListParamKeys(ctx, a, p, Pagination{Page: 1, PageSize: 20}, "")
	if e != nil {
		t.Fatal(e)
	}
	for _, k := range page.Items {
		if k.Key == "os.kernel" {
			t.Fatal("retired project override fell back to global list")
		}
	}
	if _, e = s.TransitionMedia(ctx, reviewer, p, m.ID, "retired", "test"); e != nil {
		t.Fatal(e)
	}
	valid, e = s.ValidateCatalog(ctx, a, p, values)
	if e != nil || valid.Valid {
		t.Fatal("retired media admitted")
	}
}

func TestCatalogCorrelatedCombinations(t *testing.T) {
	s := integrationStore(t)
	ctx := context.Background()
	const password = "test-only-matrix-password"
	if _, e := s.CreateUser(ctx, "matrix-owner", "Matrix Owner", password, true, true); e != nil {
		t.Fatal(e)
	}
	_, a := testLogin(t, s, "matrix-owner", password, "198.51.100.112")
	p, e := s.CreateProject(ctx, a, CreateProjectInput{Name: "Correlated combinations", Slug: "correlated"}, "matrix-project", "test")
	if e != nil {
		t.Fatal(e)
	}
	for _, in := range []UpsertParamKeyInput{
		{Key: "os.kernel", ValueType: "enum", Options: []string{"kernel-a", "kernel-b"}},
		{Key: "runtime.profile", ValueType: "enum", Options: []string{"tool-a", "tool-b"}},
	} {
		if _, e = s.UpsertParamKey(ctx, a, p, in, "test"); e != nil {
			t.Fatal(e)
		}
	}
	matrix := map[string]any{"compatibility": map[string]any{"source": "reviewed-fixture", "version": "matrix-2", "report_ref": "report:fixture-2", "requires": map[string][]string{"cpu.arch": {"amd64"}}, "combinations": []map[string]string{
		{"os.kernel": "kernel-a", "runtime.profile": "tool-a"}, {"os.kernel": "kernel-b", "runtime.profile": "tool-b"},
	}}}
	if _, e = s.UpsertParamKey(ctx, a, p, UpsertParamKeyInput{Key: "workload.profile", ValueType: "enum", Options: []string{"reviewed"}, Deps: matrix}, "test"); e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		kernel, profile, arch string
		valid                 bool
	}{
		{"kernel-a", "tool-a", "amd64", true}, {"kernel-b", "tool-b", "amd64", true},
		{"kernel-a", "tool-b", "amd64", false}, {"kernel-b", "tool-a", "amd64", false},
		{"kernel-a", "tool-a", "arm64", false}, {"kernel-a", "", "amd64", false},
	} {
		values := map[string]any{"workload.profile": "reviewed", "os.kernel": tc.kernel, "runtime.profile": tc.profile, "cpu.arch": tc.arch}
		result, err := s.ValidateCatalog(ctx, a, p, values)
		if err != nil || result.Valid != tc.valid {
			t.Fatalf("tuple %+v: %+v %v", tc, result, err)
		}
		if !tc.valid && tc.arch == "amd64" {
			found := false
			for _, issue := range result.Issues {
				if issue.Code == "PARAM_COMBINATION_REJECTED" && issue.Matrix != nil && len(issue.Matrix.Combinations) == 2 && issue.Matrix.ReportRef == "report:fixture-2" {
					found = true
				}
			}
			if !found {
				t.Fatalf("missing complete matrix provenance %+v", result)
			}
		}
	}
	// A valid row cannot bypass the underlying dependency's own option list.
	if _, e = s.UpsertParamKey(ctx, a, p, UpsertParamKeyInput{Key: "runtime.profile", ValueType: "enum", Options: []string{"tool-b"}}, "test"); e != nil {
		t.Fatal(e)
	}
	result, e := s.ValidateCatalog(ctx, a, p, map[string]any{"workload.profile": "reviewed", "os.kernel": "kernel-a", "runtime.profile": "tool-a", "cpu.arch": "amd64"})
	if e != nil || result.Valid {
		t.Fatalf("matrix bypassed dependent key admission %+v %v", result, e)
	}
}
