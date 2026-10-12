package platform

import (
	"context"
	"errors"
	"github.com/enzoOvOyu/keel/ent/migrate"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func integrationStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("KEEL_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("KEEL_TEST_DATABASE_URL not configured; real PostgreSQL test skipped")
	}
	ctx := context.Background()
	s, e := Open(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	var name string
	if e = s.DB.QueryRowContext(ctx, "SELECT current_database()").Scan(&name); e != nil {
		t.Fatal(e)
	}
	if !strings.HasPrefix(name, "keel_test_") {
		t.Fatal("refusing schema reset outside an explicitly named keel_test_ database")
	}
	if _, e = s.DB.ExecContext(ctx, "DROP SCHEMA public CASCADE; CREATE SCHEMA public"); e != nil {
		t.Fatal(e)
	}
	if e = s.CheckMigrations(ctx); e == nil {
		t.Fatal("unmigrated database accepted")
	}
	if e = s.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	if e = s.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	if e = s.CheckMigrations(ctx); e != nil {
		t.Fatal(e)
	}
	return s
}
func assertErrorCode(t *testing.T, e error, code string) {
	t.Helper()
	var pe *Error
	if !errors.As(e, &pe) || pe.Code != code {
		t.Fatalf("wanted %s, got %v", code, e)
	}
}
func testLogin(t *testing.T, s *Store, username, password, ip string) (string, *AuthSession) {
	t.Helper()
	ctx := context.Background()
	pre, _, e := s.NewPreSession(ctx)
	if e != nil {
		t.Fatal(e)
	}
	token, a, e := s.Login(ctx, pre, CSRFToken(pre), LoginInput{Username: username, Password: password, Method: "local"}, ip, "test")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.CSRFSession(ctx, pre); e == nil {
		t.Fatal("old pre-session survived login")
	}
	return token, a
}
func TestPostgresAuthenticationAndProjectTransactions(t *testing.T) {
	s := integrationStore(t)
	ctx := context.Background()
	password := "test-only-password-12-plus"
	t.Run("migration_matches_ent_columns", func(t *testing.T) {
		for _, table := range migrate.Tables {
			for _, col := range table.Columns {
				var exists bool
				e := s.DB.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name=$1 AND column_name=$2)", table.Name, col.Name).Scan(&exists)
				if e != nil || !exists {
					t.Fatalf("missing ent column %s.%s: %v", table.Name, col.Name, e)
				}
			}
		}
	})
	t.Run("first_admin_is_exclusive", func(t *testing.T) {
		var success atomic.Int32
		var wg sync.WaitGroup
		for _, name := range []string{"first-admin", "second-admin"} {
			wg.Add(1)
			go func(name string) {
				defer wg.Done()
				if _, e := s.CreateUser(ctx, name, name, password, true, true); e == nil {
					success.Add(1)
				}
			}(name)
		}
		wg.Wait()
		if success.Load() != 1 {
			t.Fatalf("initialized %d admins", success.Load())
		}
	})
	var adminName string
	if e := s.DB.QueryRowContext(ctx, "SELECT username FROM users WHERE is_admin").Scan(&adminName); e != nil {
		t.Fatal(e)
	}
	token, admin := testLogin(t, s, adminName, password, "198.51.100.1")
	_, e := s.CreateUser(ctx, "reader", "Reader", password, false, false)
	if e != nil {
		t.Fatal(e)
	}
	input := CreateProjectInput{Name: "Integration project", Slug: "integration-project", Description: "isolated test only"}
	var project *ProjectSummary
	t.Run("atomic_create_and_concurrent_idempotency", func(t *testing.T) {
		var wg sync.WaitGroup
		results := make(chan *ProjectSummary, 4)
		errs := make(chan error, 4)
		for range 4 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				p, e := s.CreateProject(ctx, admin, input, "same-request", "trace-create")
				if e != nil {
					errs <- e
				} else {
					results <- p
				}
			}()
		}
		wg.Wait()
		close(results)
		close(errs)
		for e := range errs {
			t.Error(e)
		}
		for p := range results {
			if project == nil {
				project = p
			} else if project.ID != p.ID {
				t.Fatal("idempotent requests created multiple projects")
			}
		}
		if project == nil {
			t.Fatal("no project created")
		}
		var count int
		for _, q := range []string{"SELECT count(*) FROM projects", "SELECT count(*) FROM memberships", "SELECT count(*) FROM audits WHERE action='project.create'", "SELECT count(*) FROM idempotencies"} {
			if e := s.DB.QueryRowContext(ctx, q).Scan(&count); e != nil || count != 1 {
				t.Fatalf("transaction invariant: count=%d err=%v", count, e)
			}
		}
		other := input
		other.Name = "different"
		_, e := s.CreateProject(ctx, admin, other, "same-request", "test")
		assertErrorCode(t, e, "IDEMPOTENCY_CONFLICT")
	})
	if project == nil {
		t.Fatal("project prerequisite failed")
	}
	t.Run("logout_before_transaction_and_exact_payload", func(t *testing.T) {
		_, a := testLogin(t, s, adminName, password, "198.51.100.1")
		if e := s.Logout(ctx, a, "test"); e != nil {
			t.Fatal(e)
		}
		_, e := s.CreateProject(ctx, a, CreateProjectInput{Name: "After logout", Slug: "after-logout"}, "logged-out-write", "test")
		assertErrorCode(t, e, "UNAUTHENTICATED")
		padded := input
		padded.Name = " " + input.Name + " "
		_, e = s.CreateProject(ctx, admin, padded, "same-request", "test")
		assertErrorCode(t, e, "IDEMPOTENCY_CONFLICT")
	})
	t.Run("membership_scope_and_revoke", func(t *testing.T) {
		readerToken, reader := testLogin(t, s, "reader", password, "198.51.100.2")
		rows, e := s.ListProjects(ctx, reader)
		if e != nil || len(rows) != 0 {
			t.Fatal("new user saw projects")
		}
		_, e = s.GetProject(ctx, reader, project.ID)
		assertErrorCode(t, e, "NOT_FOUND")
		_, e = s.CreateProject(ctx, reader, CreateProjectInput{Name: "Forbidden", Slug: "forbidden"}, "viewer-write", "test")
		assertErrorCode(t, e, "FORBIDDEN")
		if e = s.SetMembership(ctx, "reader", project.ID, "Viewer", false); e != nil {
			t.Fatal(e)
		}
		_, e = s.Authenticate(ctx, readerToken)
		assertErrorCode(t, e, "UNAUTHENTICATED")
		readerToken, reader = testLogin(t, s, "reader", password, "198.51.100.2")
		p, e := s.GetProject(ctx, reader, project.ID)
		if e != nil || p.Role != "Viewer" {
			t.Fatal("membership access unavailable", e)
		}
		if e = s.SetMembership(ctx, "reader", project.ID, "", true); e != nil {
			t.Fatal(e)
		}
		_, e = s.Authenticate(ctx, readerToken)
		assertErrorCode(t, e, "UNAUTHENTICATED")
	})
	t.Run("timeouts_rotation_csrf_and_logout", func(t *testing.T) {
		pre, _, e := s.NewPreSession(ctx)
		if e != nil {
			t.Fatal(e)
		}
		_, e = s.DB.ExecContext(ctx, "UPDATE sessions SET expires_at=$2 WHERE id=$1", TokenHash(pre), time.Now().Add(-time.Second))
		if e != nil {
			t.Fatal(e)
		}
		_, e = s.CSRFSession(ctx, pre)
		assertErrorCode(t, e, "UNAUTHENTICATED")
		_, _, e = s.Login(ctx, pre, CSRFToken(pre), LoginInput{Username: adminName, Password: password, Method: "local"}, "198.51.100.1", "test")
		assertErrorCode(t, e, "CSRF_INVALID")
		aToken, a := testLogin(t, s, adminName, password, "198.51.100.1")
		if e = s.CheckCSRF(ctx, aToken, CSRFToken(token)); e == nil {
			t.Fatal("cross-session CSRF accepted")
		}
		if _, e = s.DB.ExecContext(ctx, "UPDATE sessions SET last_seen_at=$2 WHERE id=$1", a.ID, time.Now().Add(-IdleTimeout-time.Second)); e != nil {
			t.Fatal(e)
		}
		_, e = s.Authenticate(ctx, aToken)
		assertErrorCode(t, e, "UNAUTHENTICATED")
		aToken, a = testLogin(t, s, adminName, password, "198.51.100.1")
		if _, e = s.DB.ExecContext(ctx, "UPDATE sessions SET expires_at=$2 WHERE id=$1", a.ID, time.Now().Add(-time.Second)); e != nil {
			t.Fatal(e)
		}
		_, e = s.Authenticate(ctx, aToken)
		assertErrorCode(t, e, "UNAUTHENTICATED")
		aToken, a = testLogin(t, s, adminName, password, "198.51.100.1")
		if e = s.Logout(ctx, a, "test"); e != nil {
			t.Fatal(e)
		}
		_, e = s.Authenticate(ctx, aToken)
		assertErrorCode(t, e, "UNAUTHENTICATED")
	})
	t.Run("disabled_and_password_changed_sessions_revoke", func(t *testing.T) {
		readerToken, _ := testLogin(t, s, "reader", password, "198.51.100.2")
		if e := s.ResetPassword(ctx, "reader", "replacement-test-password"); e != nil {
			t.Fatal(e)
		}
		_, e := s.Authenticate(ctx, readerToken)
		assertErrorCode(t, e, "UNAUTHENTICATED")
		readerToken, _ = testLogin(t, s, "reader", "replacement-test-password", "198.51.100.2")
		if e = s.DisableUser(ctx, "reader"); e != nil {
			t.Fatal(e)
		}
		_, e = s.Authenticate(ctx, readerToken)
		assertErrorCode(t, e, "UNAUTHENTICATED")
		if e = s.DisableUser(ctx, adminName); e == nil {
			t.Fatal("disabled the last administrator")
		}
	})
	t.Run("persistent_account_and_ip_throttles", func(t *testing.T) {
		pre, _, e := s.NewPreSession(ctx)
		if e != nil {
			t.Fatal(e)
		}
		for range MaxLoginFailures {
			_, _, e = s.Login(ctx, pre, CSRFToken(pre), LoginInput{Username: "missing-account", Password: password, Method: "local"}, "198.51.100.99", "test")
			assertErrorCode(t, e, "INVALID_CREDENTIALS")
		}
		for _, tc := range []struct{ name, ip string }{{"missing-account", "198.51.100.100"}, {"different-account", "198.51.100.99"}} {
			_, _, e = s.Login(ctx, pre, CSRFToken(pre), LoginInput{Username: tc.name, Password: password, Method: "local"}, tc.ip, "test")
			assertErrorCode(t, e, "RATE_LIMITED")
		}
		if e = s.UnlockUser(ctx, "missing-account"); e != nil {
			t.Fatal(e)
		}
		if e = s.UnlockIP(ctx, "198.51.100.99"); e != nil {
			t.Fatal(e)
		}
	})
	t.Run("audit_is_append_only", func(t *testing.T) {
		if _, e := s.DB.ExecContext(ctx, "UPDATE audits SET action='tampered'"); e == nil {
			t.Fatal("audit update accepted")
		}
		if _, e := s.DB.ExecContext(ctx, "DELETE FROM audits"); e == nil {
			t.Fatal("audit delete accepted")
		}
	})
	t.Run("migration_tamper_rejected", func(t *testing.T) {
		if _, e := s.DB.ExecContext(ctx, "UPDATE keel_schema_migrations SET checksum='tampered'"); e != nil {
			t.Fatal(e)
		}
		if e := s.CheckMigrations(ctx); e == nil {
			t.Fatal("tampered migration accepted")
		}
		if e := s.Migrate(ctx); e == nil {
			t.Fatal("tampered migration replay accepted")
		}
	})
}
