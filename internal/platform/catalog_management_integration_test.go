package platform

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
)

func TestCatalogManagementConcurrencyAndRetirement(t *testing.T) {
	s := integrationStore(t)
	ctx := context.Background()
	const password = "test-only-catalog-management-password"
	if _, e := s.CreateUser(ctx, "catalog-manager", "Catalog Manager", password, true, true); e != nil {
		t.Fatal(e)
	}
	_, a := testLogin(t, s, "catalog-manager", password, "198.51.100.113")
	p, e := s.CreateProject(ctx, a, CreateProjectInput{Name: "Catalog management", Slug: "catalog-management"}, "catalog-management", "test")
	if e != nil {
		t.Fatal(e)
	}
	input := UpsertParamKeyInput{Key: "runtime.profile", ValueType: "enum", Options: []string{"tool-a", "tool-b"}, DefaultValue: "tool-a", CreateOnly: true}
	original, e := s.UpsertParamKey(ctx, a, p, input, "test")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.UpsertParamKey(ctx, a, p, input, "test"); e == nil {
		t.Fatal("create-only overwrote existing definition")
	} else {
		assertErrorCode(t, e, "PARAM_KEY_CONFLICT")
	}
	input.CreateOnly = false
	input.ExpectedUpdatedAt = &original.UpdatedAt
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, value := range []string{"tool-a", "tool-b"} {
		wg.Add(1)
		go func(value string) {
			defer wg.Done()
			in := input
			in.DefaultValue = value
			_, err := s.UpsertParamKey(ctx, a, p, in, "test")
			results <- err
		}(value)
	}
	wg.Wait()
	close(results)
	wins, conflicts := 0, 0
	for err := range results {
		if err == nil {
			wins++
		} else {
			assertErrorCode(t, err, "PARAM_KEY_CONFLICT")
			conflicts++
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatalf("concurrent editors: wins=%d conflicts=%d", wins, conflicts)
	}
	listed, e := s.ListParamKeysIncludingRetired(ctx, a, p, Pagination{Page: 1, PageSize: 20}, "", true)
	if e != nil || len(listed.Items) != 1 {
		t.Fatalf("list %+v %v", listed, e)
	}
	current := listed.Items[0]
	if !current.CreatedAt.Equal(original.CreatedAt) || !current.UpdatedAt.After(original.UpdatedAt) {
		t.Fatalf("timestamps not preserved %+v", current)
	}
	if _, e = s.RetireParamKeyChecked(ctx, a, p, current.ID, &original.UpdatedAt, "test"); e == nil {
		t.Fatal("stale retirement accepted")
	} else {
		assertErrorCode(t, e, "PARAM_KEY_CONFLICT")
	}
	retired, e := s.RetireParamKeyChecked(ctx, a, p, current.ID, &current.UpdatedAt, "test")
	if e != nil || retired.State != "retired" || !retired.UpdatedAt.After(current.UpdatedAt) {
		t.Fatalf("retire %+v %v", retired, e)
	}
	input.ExpectedUpdatedAt = nil
	if _, e = s.UpsertParamKey(ctx, a, p, input, "test"); e == nil {
		t.Fatal("retired definition resurrected")
	} else {
		assertErrorCode(t, e, "PARAM_KEY_RETIRED")
	}
	global, e := s.UpsertParamKey(ctx, a, nil, UpsertParamKeyInput{Key: input.Key, ValueType: "enum", Options: []string{"global-value"}}, "test")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.RetireParamKeyChecked(ctx, a, p, global.ID, &global.UpdatedAt, "test"); e == nil {
		t.Fatal("project retired a global definition")
	} else {
		assertErrorCode(t, e, "PARAM_KEY_NOT_FOUND")
	}
	active, e := s.ListParamKeys(ctx, a, p, Pagination{Page: 1, PageSize: 20}, "")
	if e != nil || active.Total != 0 {
		t.Fatalf("retired override fell back to global %+v %v", active, e)
	}
	all, e := s.ListParamKeysIncludingRetired(ctx, a, p, Pagination{Page: 1, PageSize: 20}, "", true)
	if e != nil || all.Total != 1 || all.Items[0].ID != original.ID || all.Items[0].State != "retired" {
		t.Fatalf("management history lost %+v %v", all, e)
	}
	var audits int
	if e = s.DB.QueryRowContext(ctx, `SELECT count(*) FROM audits WHERE action='param.upsert' AND target_id=$1`, original.ID).Scan(&audits); e != nil {
		t.Fatal(e)
	}
	if audits != 2 {
		t.Fatalf("failed writes created audits: %d", audits)
	}
	var options []string
	if e = json.Unmarshal(all.Items[0].Options, &options); e != nil || len(options) != 2 {
		t.Fatal("retirement lost options")
	}
}
