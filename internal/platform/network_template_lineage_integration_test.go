package platform

import (
	"context"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/enzoOvOyu/keel/internal/netbaseline"
)

// Authored for the P2 milestone. No schema reset, external device or adapter.
func templateLineageFixture(t *testing.T) (*Store, *AuthSession, *AuthSession, *AuthSession, *ProjectSummary) {
	t.Helper()
	dsn := os.Getenv("KEEL_TEST_AGENT_DATABASE_URL")
	if dsn == "" {
		t.Skip("dedicated disposable P2 database absent")
	}
	u, e := url.Parse(dsn)
	if e != nil || u.Hostname() != "127.0.0.1" || u.Port() != "15433" || !strings.HasPrefix(u.Path, "/keel_test_agent_") {
		t.Fatal("refusing non-disposable P2 database")
	}
	ctx := context.Background()
	s, e := Open(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	if e = s.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	var initialized bool
	if e = s.DB.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM users WHERE is_admin)").Scan(&initialized); e != nil {
		t.Fatal(e)
	}
	suffix := RandomID()[:12]
	password := "p2-template-only-test-password"
	for i, name := range []string{"root-", "ne-", "nh-"} {
		if _, e = s.CreateUser(ctx, name+suffix, name+suffix, password, i == 0, i == 0 && !initialized); e != nil {
			t.Fatal(e)
		}
	}
	login := func(name string) *AuthSession {
		t.Helper()
		pre, _, err := s.NewPreSession(ctx)
		if err != nil {
			t.Fatal(err)
		}
		_, a, err := s.Login(ctx, pre, CSRFToken(pre), LoginInput{Username: name + suffix, Password: password, Method: "local"}, "198.51.100.211", "template-lineage")
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	root := login("root-")
	p, e := s.CreateProject(ctx, root, CreateProjectInput{Name: "P2 template " + suffix, Slug: "template-" + suffix}, RandomID(), "test")
	if e != nil {
		t.Fatal(e)
	}
	if e = s.SetMembership(ctx, "ne-"+suffix, p.ID, "NE", false); e != nil {
		t.Fatal(e)
	}
	if e = s.SetMembership(ctx, "nh-"+suffix, p.ID, "NH", false); e != nil {
		t.Fatal(e)
	}
	return s, root, login("ne-"), login("nh-"), p
}
func TestNetworkTemplateRollbackPreservesReleaseAndRequiresFreshApprovalPostgres(t *testing.T) {
	s, root, editor, reviewer, p := templateLineageFixture(t)
	ctx := context.Background()
	input := NetworkTemplateInput{Name: "approved leaf", SourceRef: "fixture:customer-approved", Definition: netbaseline.Template{Source: `{"hostname":"leaf-one","management":{"address":"192.0.2.2","gateway":"192.0.2.1"}}`, Variables: map[string]netbaseline.Variable{}}}
	v1, e := s.CreateNetworkTemplate(ctx, editor, p, input, "test")
	if e != nil {
		t.Fatal(e)
	}
	// Only seed the historical release. The new rollback must go through the real
	// public use case and must not inherit this test report or release authority.
	if _, e = s.DB.ExecContext(ctx, `UPDATE network_template_versions SET state='released',test_report='{"id":"historical-proof"}',released_by=$2 WHERE id=$1`, v1.ID, reviewer.User.ID); e != nil {
		t.Fatal(e)
	}
	input.Definition.Source = `{"hostname":"leaf-two","management":{"address":"192.0.2.2","gateway":"192.0.2.1"}}`
	v2, e := s.CreateNetworkTemplate(ctx, editor, p, input, "test")
	if e != nil {
		t.Fatal(e)
	}
	restore := NetworkTemplateRollbackInput{ExpectedHeadID: v2.ID, ExpectedHeadHash: v2.ContentHash, Reason: "Restore customer-approved previous behavior"}
	_, e = s.RollbackNetworkTemplate(ctx, reviewer, p, v1.ID, restore, "test")
	assertErrorCode(t, e, "FORBIDDEN")
	stale := restore
	stale.ExpectedHeadID = v1.ID
	stale.ExpectedHeadHash = v1.ContentHash
	_, e = s.RollbackNetworkTemplate(ctx, editor, p, v1.ID, stale, "test")
	assertErrorCode(t, e, "NETWORK_TEMPLATE_HEAD_CHANGED")
	foreign, e := s.CreateProject(ctx, root, CreateProjectInput{Name: "other", Slug: "other-" + RandomID()[:12]}, RandomID(), "test")
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.RollbackNetworkTemplate(ctx, root, foreign, v1.ID, restore, "test")
	if e != ErrNotFound {
		t.Fatalf("foreign template: %v", e)
	}
	v3, e := s.RollbackNetworkTemplate(ctx, editor, p, v1.ID, restore, "test")
	if e != nil {
		t.Fatal(e)
	}
	if v3.Version != 3 || v3.State != "draft" || v3.TestReport != nil || v3.ParentVersionID != v2.ID || v3.RollbackFromID != v1.ID || v3.ContentHash != v1.ContentHash {
		t.Fatalf("rollback inherited approval or lost lineage: %+v", v3)
	}
	_, e = s.ReleaseNetworkTemplate(ctx, reviewer, p, v3.ID, "test")
	assertErrorCode(t, e, "NETWORK_TEMPLATE_TEST_REQUIRED")
	_, e = s.RollbackNetworkTemplate(ctx, editor, p, v1.ID, restore, "test")
	assertErrorCode(t, e, "NETWORK_TEMPLATE_HEAD_CHANGED")
	var state, proof, authority string
	if e = s.DB.QueryRowContext(ctx, `SELECT state,test_report->>'id',released_by FROM network_template_versions WHERE id=$1`, v1.ID).Scan(&state, &proof, &authority); e != nil {
		t.Fatal(e)
	}
	if state != "released" || proof != "historical-proof" || authority != reviewer.User.ID {
		t.Fatal("historical release was changed")
	}
	if _, e = s.DB.ExecContext(ctx, `UPDATE network_template_versions SET rollback_from_id=NULL WHERE id=$1`, v3.ID); e == nil {
		t.Fatal("lineage mutation accepted")
	}
	var audits int
	if e = s.DB.QueryRowContext(ctx, `SELECT count(*) FROM audits WHERE project_id=$1 AND action='network.template.rollback' AND target_id=$2`, p.ID, v3.ID).Scan(&audits); e != nil {
		t.Fatal(e)
	}
	if audits != 1 {
		t.Fatalf("rollback audits: %d", audits)
	}
	impact, e := s.NetworkTemplateImpact(ctx, editor, p, v3.ID, "", 1, 20)
	if e != nil {
		t.Fatal(e)
	}
	if impact.BaseTemplateID != v2.ID || impact.CurrentDevices != 0 || impact.ReferencingJobs != 0 || len(impact.Items) != 0 {
		t.Fatalf("impact fabricated references: %+v", impact)
	}
}
func TestNetworkTemplateConcurrentRollbackAllocatesOneSuccessorPostgres(t *testing.T) {
	s, _, editor, reviewer, p := templateLineageFixture(t)
	ctx := context.Background()
	input := NetworkTemplateInput{Name: "rollback family", SourceRef: "fixture:customer-approved", Definition: netbaseline.Template{Source: `{"description":"approved"}`, Variables: map[string]netbaseline.Variable{}}}
	old, e := s.CreateNetworkTemplate(ctx, editor, p, input, "test")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.DB.ExecContext(ctx, `UPDATE network_template_versions SET state='released',released_by=$2 WHERE id=$1`, old.ID, reviewer.User.ID); e != nil {
		t.Fatal(e)
	}
	head, e := s.CreateNetworkTemplate(ctx, editor, p, input, "test")
	if e != nil {
		t.Fatal(e)
	}
	in := NetworkTemplateRollbackInput{ExpectedHeadID: head.ID, ExpectedHeadHash: head.ContentHash, Reason: "Concurrent reviewed rollback"}
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.RollbackNetworkTemplate(ctx, editor, p, old.ID, in, "test")
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	accepted, rejected := 0, 0
	for err := range results {
		if err == nil {
			accepted++
		} else {
			assertErrorCode(t, err, "NETWORK_TEMPLATE_HEAD_CHANGED")
			rejected++
		}
	}
	if accepted != 1 || rejected != 1 {
		t.Fatalf("successors accepted=%d rejected=%d", accepted, rejected)
	}
}
