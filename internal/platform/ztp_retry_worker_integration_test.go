package platform

import (
	"context"
	"sync"
	"testing"
)

func TestZTPSimAutomaticRetryIsDurableAndSerialized(t *testing.T) {
	s := integrationStore(t)
	ctx := context.Background()
	const password = "test-only-ztp-retry-password"
	if _, err := s.CreateUser(ctx, "ztp-retry-admin", "ZTP Retry Admin", password, true, true); err != nil {
		t.Fatal(err)
	}
	_, actor := testLogin(t, s, "ztp-retry-admin", password, "198.51.100.23")
	project, err := s.CreateProject(ctx, actor, CreateProjectInput{Name: "ZTP retries", Slug: "ztp-retries"}, "ztp-retry-project", "test")
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.CreateZTPSimSession(ctx, actor, project, ZTPSimSessionInput{BatchRef: "rack-a", BootChannel: "239", Identity: ZTPIdentityInput{Serial: "UNKNOWN-RETRY-SW"}}, "test")
	if err != nil {
		t.Fatal(err)
	}
	failed, err := s.AdvanceZTPSimSession(ctx, actor, project, session.ID, ZTPSessionAction{Action: "fail", ExpectedVersion: session.StateVersion, Phenomenon: "DHCP timeout", Cause: "no offer", NextAction: "check Kea"}, "test")
	if err != nil || failed.NextRetryAt == nil {
		t.Fatalf("failed session: %+v %v", failed, err)
	}
	if count, err := s.RetryDueZTPSimSessions(ctx, 10); err != nil || count != 0 {
		t.Fatalf("early retry count=%d err=%v", count, err)
	}
	if _, err = s.DB.ExecContext(ctx, `UPDATE ztp_sessions SET next_retry_at=now()-interval '1 second' WHERE id=$1`, session.ID); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	counts := make(chan int, 2)
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); n, e := s.RetryDueZTPSimSessions(ctx, 1); counts <- n; errs <- e }()
	}
	wg.Wait()
	close(counts)
	close(errs)
	total := 0
	for n := range counts {
		total += n
	}
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	if total != 1 {
		t.Fatalf("concurrent retry count %d", total)
	}
	resumed, err := s.GetZTPSession(ctx, actor, project, session.ID)
	if err != nil || resumed.State != "powered" || resumed.Attempt != 2 || resumed.StateVersion != 3 || resumed.NextRetryAt != nil || len(resumed.Events) != 3 || resumed.Events[2].EventCode != "sim.retry" {
		t.Fatalf("resumed: %+v %v", resumed, err)
	}
	var cause string
	if err = s.DB.QueryRowContext(ctx, `SELECT metadata->>'cause' FROM audits WHERE project_id=$1 AND action='ztp.sim.fail' AND target_id=$2`, project.ID, session.ID).Scan(&cause); err != nil || cause != "no offer" {
		t.Fatalf("failure audit: %q %v", cause, err)
	}
	if _, err = s.DB.ExecContext(ctx, `UPDATE ztp_sessions SET state='failed',next_retry_at=now()-interval '1 second',retry_max=1 WHERE id=$1`, session.ID); err != nil {
		t.Fatal(err)
	}
	if count, err := s.RetryDueZTPSimSessions(ctx, 10); err != nil || count != 0 {
		t.Fatalf("exhausted retry count=%d err=%v", count, err)
	}
}
