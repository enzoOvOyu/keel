package platform

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestControlPlanePostgresBoundaries(t *testing.T) {
	s := integrationStore(t)
	ctx := context.Background()
	password := "isolated-control-password"
	root, e := s.CreateUser(ctx, "control-root", "Root", password, true, true)
	if e != nil {
		t.Fatal(e)
	}
	_, a := testLogin(t, s, root.Username, password, "198.51.100.10")
	createUser := func(name string, admin bool) *UserAccount {
		t.Helper()
		u, e := s.ControlCreateUser(ctx, a, CreateUserInput{Username: name, DisplayName: name, Password: password, IsAdmin: admin}, "control-test")
		if e != nil {
			t.Fatal(e)
		}
		return u
	}
	account := func(name string) *UserAccount {
		t.Helper()
		p, e := s.ListUsers(ctx, a, Pagination{1, 100}, name)
		if e != nil {
			t.Fatal(e)
		}
		for _, u := range p.Items {
			if u.Username == name {
				return &u
			}
		}
		t.Fatal("account not found")
		return nil
	}
	project := func(slug string) *ProjectSummary {
		t.Helper()
		p, e := s.CreateProject(ctx, a, CreateProjectInput{Name: slug, Slug: slug}, slug, "control-test")
		if e != nil {
			t.Fatal(e)
		}
		return p
	}
	reader := createUser("control-reader", false)
	lead := createUser("control-lead", false)
	one, two, three := project("control-one"), project("control-two"), project("control-three")
	readerToken, readerSession := testLogin(t, s, reader.Username, password, "198.51.100.11")
	t.Run("deny_platform_privilege_and_paginate", func(t *testing.T) {
		_, e := s.ListUsers(ctx, readerSession, Pagination{1, 20}, "")
		assertErrorCode(t, e, "FORBIDDEN")
		_, e = s.ControlCreateUser(ctx, readerSession, CreateUserInput{Username: "intruder", DisplayName: "Intruder", Password: password, IsAdmin: true}, "test")
		assertErrorCode(t, e, "FORBIDDEN")
		_, e = s.ListAudits(ctx, readerSession, "", Pagination{1, 20}, "")
		assertErrorCode(t, e, "FORBIDDEN")
		p1, e := s.ListProjectsPage(ctx, a, Pagination{1, 2})
		if e != nil || len(p1.Items) != 2 || p1.Total != 3 {
			t.Fatalf("page1: %+v %v", p1, e)
		}
		p2, e := s.ListProjectsPage(ctx, a, Pagination{2, 2})
		if e != nil || len(p2.Items) != 1 || p2.Total != 3 {
			t.Fatalf("page2: %+v %v", p2, e)
		}
		for _, p := range p1.Items {
			if p.ID == p2.Items[0].ID {
				t.Fatal("pagination duplicated a project")
			}
		}
		empty, e := s.ListProjectsPage(ctx, readerSession, Pagination{1, 20})
		if e != nil || len(empty.Items) != 0 || empty.Total != 0 {
			t.Fatal("unscoped project data")
		}
		_, e = s.ListUsers(ctx, a, Pagination{0, 20}, "")
		assertErrorCode(t, e, "VALIDATION_ERROR")
		literal, e := s.ListUsers(ctx, a, Pagination{1, 20}, "%")
		if e != nil || literal.Total != 0 {
			t.Fatal("search wildcards were not literal")
		}
	})
	var leadMember *Member
	t.Run("project_admin_scope_and_member_cas", func(t *testing.T) {
		leadMember, e = s.ControlCreateMember(ctx, a, one.ID, CreateMemberInput{Username: lead.Username, Role: "Admin"}, "test")
		if e != nil {
			t.Fatal(e)
		}
		m, e := s.ControlCreateMember(ctx, a, one.ID, CreateMemberInput{Username: reader.Username, Role: "Viewer"}, "test")
		if e != nil {
			t.Fatal(e)
		}
		_, e = s.Authenticate(ctx, readerToken)
		assertErrorCode(t, e, "UNAUTHENTICATED")
		readerToken, readerSession = testLogin(t, s, reader.Username, password, "198.51.100.11")
		_, e = s.ListMembers(ctx, readerSession, one.ID, Pagination{1, 20})
		assertErrorCode(t, e, "FORBIDDEN")
		_, e = s.ListAudits(ctx, readerSession, one.ID, Pagination{1, 20}, "")
		assertErrorCode(t, e, "FORBIDDEN")
		_, leadSession := testLogin(t, s, lead.Username, password, "198.51.100.12")
		_, e = s.ListMembers(ctx, leadSession, two.ID, Pagination{1, 20})
		assertErrorCode(t, e, "NOT_FOUND")
		_, e = s.ListUsers(ctx, leadSession, Pagination{1, 20}, "")
		assertErrorCode(t, e, "FORBIDDEN")
		m, e = s.ControlChangeMember(ctx, leadSession, one.ID, reader.ID, UpdateMemberInput{Role: "DE", Version: m.Version}, false, "test")
		if e != nil {
			t.Fatal(e)
		}
		_, e = s.ControlChangeMember(ctx, leadSession, one.ID, reader.ID, UpdateMemberInput{Role: "Admin", Version: 1}, false, "test")
		assertErrorCode(t, e, "VERSION_CONFLICT")
		_, e = s.Authenticate(ctx, readerToken)
		assertErrorCode(t, e, "UNAUTHENTICATED")
		m, e = s.ControlChangeMember(ctx, leadSession, one.ID, reader.ID, UpdateMemberInput{Role: "Admin", Version: m.Version}, false, "test")
		if e != nil {
			t.Fatal(e)
		}
		readerToken, readerSession = testLogin(t, s, reader.Username, password, "198.51.100.11")
		if readerSession.User.IsAdmin {
			t.Fatal("project Admin became platform Admin")
		}
		_, e = s.ListUsers(ctx, readerSession, Pagination{1, 20}, "")
		assertErrorCode(t, e, "FORBIDDEN")
		_, e = s.ControlChangeMember(ctx, leadSession, one.ID, reader.ID, UpdateMemberInput{Version: m.Version}, true, "test")
		if e != nil {
			t.Fatal(e)
		}
		_, e = s.Authenticate(ctx, readerToken)
		assertErrorCode(t, e, "UNAUTHENTICATED")
		replacement, e := s.ControlCreateMember(ctx, a, one.ID, CreateMemberInput{Username: reader.Username, Role: "Viewer"}, "test")
		if e != nil {
			t.Fatal(e)
		}
		if replacement.Version <= m.Version {
			t.Fatal("recreated membership reset its version")
		}
		_, e = s.ControlChangeMember(ctx, a, one.ID, reader.ID, UpdateMemberInput{Role: "Admin", Version: m.Version}, false, "stale-patch")
		assertErrorCode(t, e, "VERSION_CONFLICT")
		_, e = s.ControlChangeMember(ctx, a, one.ID, reader.ID, UpdateMemberInput{Version: m.Version}, true, "stale-delete")
		assertErrorCode(t, e, "VERSION_CONFLICT")
		_, e = s.ControlChangeMember(ctx, a, one.ID, reader.ID, UpdateMemberInput{Version: replacement.Version}, true, "cleanup-test-member")
		if e != nil {
			t.Fatal(e)
		}
		if e = s.SetMembership(ctx, reader.Username, one.ID, "Viewer", false); e != nil {
			t.Fatal(e)
		}
		members, e := s.ListMembers(ctx, a, one.ID, Pagination{1, 100})
		if e != nil {
			t.Fatal(e)
		}
		var cliVersion int
		for _, entry := range members.Items {
			if entry.User.ID == reader.ID {
				cliVersion = entry.Version
			}
		}
		if cliVersion <= replacement.Version {
			t.Fatal("CLI membership recreation reset version")
		}
		if e = s.SetMembership(ctx, reader.Username, one.ID, "DE", false); e != nil {
			t.Fatal(e)
		}
		_, e = s.ControlChangeMember(ctx, a, one.ID, reader.ID, UpdateMemberInput{Version: cliVersion}, true, "stale-after-cli")
		assertErrorCode(t, e, "VERSION_CONFLICT")
		if e = s.SetMembership(ctx, reader.Username, one.ID, "", true); e != nil {
			t.Fatal(e)
		}
	})
	t.Run("last_platform_and_project_administrator", func(t *testing.T) {
		disabled := false
		_, e = s.ControlUpdateUser(ctx, a, root.ID, UpdateUserInput{Enabled: &disabled, Version: account(root.Username).Version}, "test")
		assertErrorCode(t, e, "LAST_PLATFORM_ADMIN")
		_, e = s.ControlChangeMember(ctx, a, three.ID, root.ID, UpdateMemberInput{Role: "Viewer", Version: 1}, false, "test")
		assertErrorCode(t, e, "LAST_PROJECT_ADMIN")
		_, e = s.ControlChangeMember(ctx, a, one.ID, root.ID, UpdateMemberInput{Version: 1}, true, "test")
		if e != nil {
			t.Fatal(e)
		}
		_, a = testLogin(t, s, root.Username, password, "198.51.100.10")
		_, e = s.ControlUpdateUser(ctx, a, lead.ID, UpdateUserInput{Enabled: &disabled, Version: account(lead.Username).Version}, "test")
		assertErrorCode(t, e, "LAST_PROJECT_ADMIN")
		if e = s.DisableUser(ctx, lead.Username); e == nil {
			t.Fatal("CLI disabled last project administrator")
		}
		if e = s.SetMembership(ctx, lead.Username, one.ID, "Viewer", false); e == nil {
			t.Fatal("CLI demoted last project administrator")
		}
		_, leadSession := testLogin(t, s, lead.Username, password, "198.51.100.12")
		_, e = s.ControlChangeMember(ctx, leadSession, one.ID, lead.ID, UpdateMemberInput{Version: leadMember.Version}, true, "test")
		assertErrorCode(t, e, "LAST_PROJECT_ADMIN")
	})
	t.Run("concurrent_removal_cannot_empty_project_administration", func(t *testing.T) {
		x := createUser("control-next-one", false)
		y := createUser("control-next-two", false)
		xm, e := s.ControlCreateMember(ctx, a, three.ID, CreateMemberInput{Username: x.Username, Role: "Admin"}, "test")
		if e != nil {
			t.Fatal(e)
		}
		ym, e := s.ControlCreateMember(ctx, a, three.ID, CreateMemberInput{Username: y.Username, Role: "Admin"}, "test")
		if e != nil {
			t.Fatal(e)
		}
		versions := map[string]int{x.ID: xm.Version, y.ID: ym.Version}
		if _, e = s.ControlChangeMember(ctx, a, three.ID, root.ID, UpdateMemberInput{Version: 1}, true, "test"); e != nil {
			t.Fatal(e)
		}
		_, a = testLogin(t, s, root.Username, password, "198.51.100.10")
		var successes, protected atomic.Int32
		var wg sync.WaitGroup
		for _, id := range []string{x.ID, y.ID} {
			wg.Add(1)
			go func(id string) {
				defer wg.Done()
				_, e := s.ControlChangeMember(ctx, a, three.ID, id, UpdateMemberInput{Version: versions[id]}, true, "test")
				if e == nil {
					successes.Add(1)
				} else {
					var pe *Error
					if errors.As(e, &pe) && pe.Code == "LAST_PROJECT_ADMIN" {
						protected.Add(1)
					} else {
						t.Error(e)
					}
				}
			}(id)
		}
		wg.Wait()
		if successes.Load() != 1 || protected.Load() != 1 {
			t.Fatal("last-project-admin race protection failed")
		}
	})
	t.Run("status_password_unlock_cas_and_revocation", func(t *testing.T) {
		enabled, disabled := true, false
		readerToken, _ = testLogin(t, s, reader.Username, password, "198.51.100.11")
		u := account(reader.Username)
		u, e = s.ControlUpdateUser(ctx, a, reader.ID, UpdateUserInput{Enabled: &disabled, Version: u.Version}, "test")
		if e != nil {
			t.Fatal(e)
		}
		_, e = s.Authenticate(ctx, readerToken)
		assertErrorCode(t, e, "UNAUTHENTICATED")
		_, e = s.ControlUpdateUser(ctx, a, reader.ID, UpdateUserInput{Enabled: &enabled, Version: u.Version - 1}, "test")
		assertErrorCode(t, e, "VERSION_CONFLICT")
		u, e = s.ControlUpdateUser(ctx, a, reader.ID, UpdateUserInput{Enabled: &enabled, Version: u.Version}, "test")
		if e != nil {
			t.Fatal(e)
		}
		readerToken, _ = testLogin(t, s, reader.Username, password, "198.51.100.11")
		u, e = s.ControlResetPassword(ctx, a, reader.ID, ResetPasswordInput{Password: "replaced-control-password", Version: u.Version}, "test")
		if e != nil {
			t.Fatal(e)
		}
		_, e = s.Authenticate(ctx, readerToken)
		assertErrorCode(t, e, "UNAUTHENTICATED")
		readerToken, _ = testLogin(t, s, reader.Username, "replaced-control-password", "198.51.100.11")
		key := digest("account:" + reader.Username)
		ipKey := digest("ip:198.51.100.90")
		for _, k := range []string{key, ipKey} {
			if _, e = s.DB.ExecContext(ctx, "INSERT INTO login_throttles(id,failures,window_started_at,locked_until) VALUES($1,5,$2,$3) ON CONFLICT(id) DO UPDATE SET locked_until=EXCLUDED.locked_until", k, time.Now(), time.Now().Add(time.Minute)); e != nil {
				t.Fatal(e)
			}
		}
		u, e = s.ControlUnlockUser(ctx, a, reader.ID, UnlockUserInput{Version: u.Version}, "test")
		if e != nil {
			t.Fatal(e)
		}
		var ipRemains bool
		if e = s.DB.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM login_throttles WHERE id=$1)", ipKey).Scan(&ipRemains); e != nil || !ipRemains {
			t.Fatal("account unlock erased unrelated IP lock")
		}
		_, e = s.Authenticate(ctx, readerToken)
		assertErrorCode(t, e, "UNAUTHENTICATED")
		_, e = s.ControlUnlockUser(ctx, a, reader.ID, UnlockUserInput{Version: u.Version, IP: "198.51.100.90"}, "test")
		if e != nil {
			t.Fatal(e)
		}
	})
	t.Run("redacted_audit_filter_scope_and_no_mutation_after_logout", func(t *testing.T) {
		tx, e := s.DB.BeginTx(ctx, nil)
		if e != nil {
			t.Fatal(e)
		}
		sentinel := "SECRET-AUDIT-REDACTION-PROBE"
		if e = appendAudit(ctx, tx, root.ID, one.ID, "redaction.test", one.ID, "trace-probe", map[string]any{"password": sentinel, "password_hash": sentinel, "csrf_hash": sentinel, "session": sentinel, "source_ip": "198.51.100.20", "role": "Admin", "source": "server_cli", "result": sentinel}); e != nil {
			t.Fatal(e)
		}
		if e = tx.Commit(); e != nil {
			t.Fatal(e)
		}
		page, e := s.ListAudits(ctx, a, one.ID, Pagination{1, 1}, "redaction.test")
		if e != nil || page.Total != 1 || len(page.Items) != 1 {
			t.Fatalf("audit page %+v: %v", page, e)
		}
		encoded, _ := json.Marshal(page)
		if strings.Contains(string(encoded), sentinel) || strings.Contains(string(encoded), "198.51.100.20") {
			t.Fatal("audit secrets exposed")
		}
		if page.Items[0].Metadata["role"] != "Admin" {
			t.Fatal("safe audit context lost")
		}
		_, fresh := testLogin(t, s, root.Username, password, "198.51.100.10")
		if e = s.Logout(ctx, fresh, "test"); e != nil {
			t.Fatal(e)
		}
		_, e = s.ControlCreateUser(ctx, fresh, CreateUserInput{Username: "after-logout", DisplayName: "Denied", Password: password}, "test")
		assertErrorCode(t, e, "UNAUTHENTICATED")
	})
	t.Run("own_password_old_secret_and_throttle", func(t *testing.T) {
		readerToken, readerSession = testLogin(t, s, reader.Username, "replaced-control-password", "198.51.100.11")
		e = s.ChangeOwnPassword(ctx, readerSession, ChangePasswordInput{CurrentPassword: "wrong-password", NewPassword: "own-new-control-password"}, "198.51.100.11", "test")
		assertErrorCode(t, e, "CURRENT_PASSWORD_INVALID")
		if _, e = s.Authenticate(ctx, readerToken); e != nil {
			t.Fatal("one wrong old password destroyed valid session")
		}
		if e = s.ChangeOwnPassword(ctx, readerSession, ChangePasswordInput{CurrentPassword: "replaced-control-password", NewPassword: "own-new-control-password"}, "198.51.100.11", "test"); e != nil {
			t.Fatal(e)
		}
		_, e = s.Authenticate(ctx, readerToken)
		assertErrorCode(t, e, "UNAUTHENTICATED")
		_, readerSession = testLogin(t, s, reader.Username, "own-new-control-password", "198.51.100.11")
		for range MaxLoginFailures {
			e = s.ChangeOwnPassword(ctx, readerSession, ChangePasswordInput{CurrentPassword: "wrong-again", NewPassword: "own-next-control-password"}, "198.51.100.111", "test")
			assertErrorCode(t, e, "CURRENT_PASSWORD_INVALID")
		}
		e = s.ChangeOwnPassword(ctx, readerSession, ChangePasswordInput{CurrentPassword: "own-new-control-password", NewPassword: "own-next-control-password"}, "198.51.100.111", "test")
		assertErrorCode(t, e, "RATE_LIMITED")
	})
}
