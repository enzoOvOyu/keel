package platform

import (
	"context"
	"net/netip"
	"strings"
	"sync"
	"testing"

	"github.com/enzoOvOyu/keel/internal/fabricboot"
)

func scopeFixture(t *testing.T) (*Store, *AuthSession, *AuthSession, *ProjectSummary) {
	t.Helper()
	s := integrationStore(t)
	ctx := context.Background()
	password := "test-only-provision-network-password"
	for i, name := range []string{"scope-owner", "scope-reviewer"} {
		if _, err := s.CreateUser(ctx, name, name, password, true, i == 0); err != nil {
			t.Fatal(err)
		}
	}
	_, owner := testLogin(t, s, "scope-owner", password, "198.51.100.190")
	_, reviewer := testLogin(t, s, "scope-reviewer", password, "198.51.100.191")
	p, err := s.CreateProject(ctx, owner, CreateProjectInput{Name: "Server boot network", Slug: "server-boot-network"}, "scope-project", "test")
	if err != nil {
		t.Fatal(err)
	}
	return s, owner, reviewer, p
}
func scopeInput() CreateProvisionBootScopeInput {
	return CreateProvisionBootScopeInput{Interface: "boot0", ServerIPv4: "192.0.2.1", ClientCIDR: "192.0.2.0/24", Origin: "https://boot.example.test", KeaNetworks: []string{"198.51.100.0/24"}, SourceRef: "fixture:current-network-ownership"}
}
func scopeApproval(version int64) ProvisionBootScopeActionInput {
	return ProvisionBootScopeActionInput{ExpectedVersion: version, CustomerApprovalRef: "fixture:customer-scope-consent", NetworkApprovalRef: "fixture:network-team-current-scope-consent"}
}
func TestProvisionBootScopeApprovalOwnershipAndRevocation(t *testing.T) {
	s, owner, reviewer, p := scopeFixture(t)
	ctx := context.Background()
	x, err := s.CreateProvisionBootScope(ctx, owner, p.ID, scopeInput(), "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ChangeProvisionBootScope(ctx, owner, p.ID, x.ID, "approve", scopeApproval(1), "test"); err == nil {
		t.Fatal("self approval")
	}
	x, err = s.ChangeProvisionBootScope(ctx, reviewer, p.ID, x.ID, "approve", scopeApproval(1), "test")
	if err != nil || x.State != "approved" {
		t.Fatal("independent approval", err)
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadApprovedProvisionScopeTx(ctx, tx, p.ID, x.ID)
	tx.Rollback()
	if err != nil || loaded.ScopeSHA256 != x.ScopeSHA256 {
		t.Fatal("live scope check", err)
	}
	if _, err = s.CreateProvisionBootScope(ctx, owner, p.ID, scopeInput(), "test"); err == nil {
		t.Fatal("second overlapping boot domain")
	}
	if _, err = s.DB.ExecContext(ctx, `UPDATE provision_boot_scopes SET scope_sha256=repeat('f',64),state_version=state_version+1 WHERE id=$1`, x.ID); err == nil {
		t.Fatal("scope binding mutable")
	}
	rules := &fabricboot.ManagementRules{CIDR: "192.0.2.0/24", FirstAddress: "192.0.2.10", Gateway: "192.0.2.1", BatchPrefix: "scope", BatchBy: "rack"}
	if _, err = s.CreateNetworkBaseline(ctx, owner, p, CreateNetworkBaselineInput{VersionRef: "scope-management-v1", SourceRef: "fixture:later-network-plan", EngineeringLimits: NetworkEngineeringLimits{SwitchBandwidthRatioLimits: map[string]float64{"role:mgmt": 1}, Management: rules}}, "test"); err != nil {
		t.Fatal(err)
	}
	baselineApproval := ApproveNetworkBaselineInput{CustomerApprovalRef: "fixture:customer", NetworkApprovalRef: "fixture:network"}
	if _, err = s.ApproveNetworkBaseline(ctx, owner, p, "scope-management-v1", baselineApproval, "test"); err == nil {
		t.Fatal("network baseline claimed approved server domain")
	}
	if _, err = s.ChangeProvisionBootScope(ctx, owner, p.ID, x.ID, "revoke", ProvisionBootScopeActionInput{ExpectedVersion: 1, Reason: "stale"}, "test"); err == nil {
		t.Fatal("stale revoke")
	}
	if _, err = s.ChangeProvisionBootScope(ctx, owner, p.ID, x.ID, "revoke", ProvisionBootScopeActionInput{ExpectedVersion: x.StateVersion, Reason: "withdraw server boot ownership"}, "test"); err != nil {
		t.Fatal(err)
	}
	tx, err = s.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = LoadApprovedProvisionScopeTx(ctx, tx, p.ID, x.ID)
	tx.Rollback()
	if err == nil {
		t.Fatal("revoked scope authorized")
	}
	if _, err = s.ApproveNetworkBaseline(ctx, owner, p, "scope-management-v1", baselineApproval, "test"); err != nil {
		t.Fatal("explicitly released domain", err)
	}
	if _, err = s.CreateProvisionBootScope(ctx, owner, p.ID, scopeInput(), "test"); err == nil {
		t.Fatal("boot domain overlaps current approved Kea pool")
	}
	var executions int
	if err = s.DB.QueryRowContext(ctx, "SELECT count(*) FROM exec_executions").Scan(&executions); err != nil || executions != 0 {
		t.Fatal("registry started an execution", err)
	}
}
func TestProvisionBootScopeConcurrentApprovalsAndInvalidInventory(t *testing.T) {
	s, owner, reviewer, p := scopeFixture(t)
	ctx := context.Background()
	in := scopeInput()
	in.KeaNetworks = nil
	if _, err := s.CreateProvisionBootScope(ctx, owner, p.ID, in, "test"); err == nil {
		t.Fatal("missing explicit external ownership inventory")
	}
	in = scopeInput()
	in.KeaNetworks = []string{"192.0.2.0/25"}
	if _, err := s.CreateProvisionBootScope(ctx, owner, p.ID, in, "test"); err == nil {
		t.Fatal("declared external Kea conflict")
	}
	first, err := s.CreateProvisionBootScope(ctx, owner, p.ID, scopeInput(), "test")
	if err != nil {
		t.Fatal(err)
	}
	in = scopeInput()
	in.ServerIPv4 = "192.0.2.2"
	second, err := s.CreateProvisionBootScope(ctx, owner, p.ID, in, "test")
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, x := range []*ProvisionBootScope{first, second} {
		wg.Add(1)
		go func(x *ProvisionBootScope) {
			defer wg.Done()
			<-start
			_, err := s.ChangeProvisionBootScope(ctx, reviewer, p.ID, x.ID, "approve", scopeApproval(1), "test")
			results <- err
		}(x)
	}
	close(start)
	wg.Wait()
	close(results)
	accepted := 0
	for err := range results {
		if err == nil {
			accepted++
		}
	}
	if accepted != 1 {
		t.Fatalf("concurrent conflicting domains accepted: %d", accepted)
	}
	bad := first.Scope
	bad.Development = true
	if validateProvisionScope(bad) == nil {
		t.Fatal("production API accepted development scope")
	}
	bad = first.Scope
	bad.Clients = netip.MustParsePrefix("192.0.2.0/32")
	if validateProvisionScope(bad) == nil {
		t.Fatal("unsupported subnet")
	}
}

func TestProvisionBootScopeCurrentNetworkRoleAndProjectIsolation(t *testing.T) {
	s, owner, _, p := scopeFixture(t)
	ctx := context.Background()
	password := "test-only-scope-roles-password"
	actors := map[string]*AuthSession{}
	for i, role := range []string{"NDE", "DE", "Viewer"} {
		name := "scope-role-" + strings.ToLower(role)
		if _, err := s.CreateUser(ctx, name, name, password, false, false); err != nil {
			t.Fatal(err)
		}
		if err := s.SetMembership(ctx, name, p.ID, role, false); err != nil {
			t.Fatal(err)
		}
		_, actors[role] = testLogin(t, s, name, password, "198.51.100."+[]string{"192", "193", "194"}[i])
	}
	x, err := s.CreateProvisionBootScope(ctx, owner, p.ID, scopeInput(), "test")
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"DE", "Viewer"} {
		if _, err = s.CreateProvisionBootScope(ctx, actors[role], p.ID, scopeInput(), "test"); err == nil {
			t.Fatalf("%s created network authorization", role)
		}
		if _, err = s.ChangeProvisionBootScope(ctx, actors[role], p.ID, x.ID, "approve", scopeApproval(1), "test"); err == nil {
			t.Fatalf("%s approved network authorization", role)
		}
		if _, err = s.ReadProvisionBootScope(ctx, actors[role], p.ID, x.ID); err != nil {
			t.Fatalf("project member read %s: %v", role, err)
		}
	}
	x, err = s.ChangeProvisionBootScope(ctx, actors["NDE"], p.ID, x.ID, "approve", scopeApproval(1), "test")
	if err != nil {
		t.Fatal("NDE independent network review", err)
	}
	other, err := s.CreateProject(ctx, owner, CreateProjectInput{Name: "Other scope", Slug: "other-scope"}, "other-scope", "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReadProvisionBootScope(ctx, owner, other.ID, x.ID); err == nil {
		t.Fatal("scope rebound to another project")
	}
	if _, err = s.ReadProvisionBootScope(ctx, actors["DE"], other.ID, x.ID); err == nil {
		t.Fatal("nonmember scope read")
	}
	if err = s.SetMembership(ctx, "scope-role-nde", p.ID, "NDE", true); err != nil {
		t.Fatal(err)
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = LoadApprovedProvisionScopeTx(ctx, tx, p.ID, x.ID)
	tx.Rollback()
	if err == nil {
		t.Fatal("revoked approver authority still admitted boot")
	}
}
