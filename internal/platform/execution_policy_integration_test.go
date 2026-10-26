package platform

import (
	"context"
	"testing"
)

func TestExecutionCircuitBreakerCatalogScopePostgres(t *testing.T) {
	s := integrationStore(t)
	ctx := context.Background()
	const password = "isolated-execution-policy-password"
	_, err := s.CreateUser(ctx, "execution-policy", "Policy fixture", password, true, true)
	if err != nil {
		t.Fatal(err)
	}
	_, actor := testLogin(t, s, "execution-policy", password, "198.51.100.195")
	project, err := s.CreateProject(ctx, actor, CreateProjectInput{Name: "Policy", Slug: "execution-policy"}, "policy-project", "test")
	if err != nil {
		t.Fatal(err)
	}
	read := func() (*ExecutionCircuitBreakerPolicy, error) {
		tx, e := s.DB.BeginTx(ctx, nil)
		if e != nil {
			return nil, e
		}
		defer tx.Rollback()
		return ResolveExecutionCircuitBreakerTx(ctx, tx, project.ID)
	}
	put := func(scope *ProjectSummary, value string) *ParamKeySummary {
		t.Helper()
		out, e := s.UpsertParamKey(ctx, actor, scope, UpsertParamKeyInput{Key: "exec.circuit_breaker_threshold", ValueType: "string", DefaultValue: value}, "test")
		if e != nil {
			t.Fatal(e)
		}
		return out
	}
	policy, err := read()
	if err != nil || policy.DefaultPercent != 5 || policy.Source != "built_in" {
		t.Fatal(policy, err)
	}
	global := put(nil, "12")
	policy, err = read()
	if err != nil || policy.DefaultPercent != 12 || policy.Source != "global" || policy.ParamKeyID != global.ID {
		t.Fatal(policy, err)
	}
	local := put(project, "7")
	policy, err = read()
	if err != nil || policy.DefaultPercent != 7 || policy.Source != "project" || policy.UpdatedAt == nil || !policy.UpdatedAt.Equal(local.UpdatedAt) {
		t.Fatal(policy, err)
	}
	// The project tombstone must block, never silently resurrect global/default.
	if _, err = s.RetireParamKeyChecked(ctx, actor, project, local.ID, &local.UpdatedAt, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err = read(); err == nil {
		t.Fatal("retired project policy fell back")
	}
	// A different project sees the global policy, never another project's key.
	other, err := s.CreateProject(ctx, actor, CreateProjectInput{Name: "Other policy", Slug: "other-policy"}, "other-policy", "test")
	if err != nil {
		t.Fatal(err)
	}
	put(other, "not-a-percentage")
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = ResolveExecutionCircuitBreakerTx(ctx, tx, other.ID); err == nil {
		t.Fatal("malformed project policy admitted")
	}
}
