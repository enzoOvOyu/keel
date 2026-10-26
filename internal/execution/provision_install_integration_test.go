package execution

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/platform"
)

// Fixtures carry seven explicitly approved role records but an intentionally
// invalid initrd. They must support scheduling/locking and fail before boot;
// none is accepted as a runnable image or hardware evidence.
func installationKernel(t *testing.T, installable bool) (*provisionKernelFixture, *platform.ProvisionRequest, *platform.ProvisionBootScope) {
	return installationKernelSource(t, installable, false)
}
func installationKernelSource(t *testing.T, installable, signedImage bool, writer ...bool) (*provisionKernelFixture, *platform.ProvisionRequest, *platform.ProvisionBootScope) {
	t.Helper()
	f := provisionKernelWithImage(t, installable, signedImage, writer...)
	ctx := context.Background()
	prep := f.prepare(t)
	claim := f.claim(t, prep.ID)
	if claim == nil {
		t.Fatal("preparation claim missing")
	}
	mustAgent(t, f.service.RunClaim(ctx, claim))
	request, err := f.store.ReadProvisionRequest(ctx, f.actor, f.project, f.request.ID)
	mustAgent(t, err)
	if request.State != "prepared" {
		t.Fatal("preparation failed", request.State)
	}
	scope, err := f.store.CreateProvisionBootScope(ctx, f.actor, f.project, platform.CreateProvisionBootScopeInput{Interface: "boot0", ServerIPv4: "192.0.2.1", ClientCIDR: "192.0.2.0/24", Origin: "https://boot.example.test", KeaNetworks: []string{}, SourceRef: "fixture:independent-network-ownership"}, "test")
	mustAgent(t, err)
	scope, err = f.store.ChangeProvisionBootScope(ctx, f.reviewer, f.project, scope.ID, "approve", platform.ProvisionBootScopeActionInput{ExpectedVersion: scope.StateVersion, CustomerApprovalRef: "fixture:customer-consent", NetworkApprovalRef: "fixture:network-team-consent"}, "test")
	mustAgent(t, err)
	// This suite shares one disposable DB across fixtures. Retire only this
	// fixture's domain through the real API so a later fixture can use its CIDR.
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		current, e := f.store.ReadProvisionBootScope(cleanup, f.actor, f.project, scope.ID)
		if e != nil {
			t.Error("read fixture scope for retirement", e)
			return
		}
		if current.State != "revoked" {
			_, e = f.store.ChangeProvisionBootScope(cleanup, f.actor, f.project, current.ID, "revoke", platform.ProvisionBootScopeActionInput{ExpectedVersion: current.StateVersion, Reason: "retire this isolated software fixture domain"}, "test")
			if e != nil {
				t.Error("retire fixture scope", e)
			}
		}
	})
	public, _, err := ed25519.GenerateKey(rand.Reader)
	mustAgent(t, err)
	if !signedImage {
		f.store.ProvisionImagePublicKey = public
	}
	f.store.ProvisionInstallEnabled = true
	return f, request, scope
}

func TestProvisionInstallationKernelPostgres(t *testing.T) {
	ctx := context.Background()
	t.Run("disabled_and_missing_recipe", func(t *testing.T) {
		f, request, scope := installationKernel(t, false)
		in := CreateProvisionInstallationInput{ExpectedVersion: request.StateVersion, ScopeID: scope.ID, Reason: "isolated native installation admission"}
		if _, err := f.service.CreateProvisionInstallation(ctx, f.actor, f.project, request.ID, in, platform.RandomID(), "test"); err == nil {
			t.Fatal("generic initrd without recipe admitted")
		}
		f.store.ProvisionInstallEnabled = false
		if _, err := f.service.CreateProvisionInstallation(ctx, f.actor, f.project, request.ID, in, platform.RandomID(), "test"); err == nil {
			t.Fatal("disabled installer admitted")
		}
	})
	t.Run("exclusive_lock_then_invalid_archive_stops_before_boot", func(t *testing.T) {
		f, request, scope := installationKernel(t, true)
		in := CreateProvisionInstallationInput{ExpectedVersion: request.StateVersion, ScopeID: scope.ID, Reason: "isolated pre-write failure evidence"}
		key := platform.RandomID()
		x, err := f.service.CreateProvisionInstallation(ctx, f.actor, f.project, request.ID, in, key, "test")
		mustAgent(t, err)
		again, err := f.service.CreateProvisionInstallation(ctx, f.actor, f.project, request.ID, in, key, "test")
		mustAgent(t, err)
		if again.ID != x.ID {
			t.Fatal("idempotency duplicated execution")
		}
		if _, err = f.service.CreateProvisionInstallation(ctx, f.actor, f.project, request.ID, in, platform.RandomID(), "test"); err == nil {
			t.Fatal("prepared source consumed twice")
		}
		task := x.Snapshot.Flow.Definition.Tasks[0]
		if task.Type != "provision.install" || task.Retry != 0 || task.Idempotent {
			t.Fatal("installation automatically retryable")
		}
		agent := f.create(t)
		agentClaim := f.claim(t, agent.ID)
		if agentClaim == nil {
			t.Fatal("agent claim missing")
		}
		if f.claim(t, x.ID) != nil {
			t.Fatal("installation overlapped SSH/Agent machine resource")
		}
		mustAgent(t, f.service.RunClaim(ctx, agentClaim))
		claim := f.claim(t, x.ID)
		if claim == nil {
			t.Fatal("fresh exclusive installation lease absent")
		}
		now := time.Now().UTC()
		forged := Receipt{CommandID: claim.Attempt.CommandID, AttemptID: claim.Attempt.ID, FencingToken: claim.Attempt.FencingToken, Result: "success", StartedAt: now, FinishedAt: now, Output: map[string]any{"schema": "keel.provision-installation-proof/v1", "installation_id": claim.Target.ProvisionInstallationID, "request_id": request.ID, "snapshot_sha256": request.SnapshotSHA256, "installation_performed": false}}
		if err = f.service.RecordReceipt(ctx, forged); err == nil {
			t.Fatal("invented installation success accepted")
		}
		mustAgent(t, f.service.RunClaim(ctx, claim))
		installation, err := f.store.ReadProvisionInstallation(ctx, f.actor, f.project, claim.Target.ProvisionInstallationID)
		mustAgent(t, err)
		if installation.State != "failed" || installation.AttemptID != nil || installation.BootID != nil || installation.PermitIssuedAt != nil {
			t.Fatal("invalid archive activated a boot or disk permit")
		}
		var assetState, trust string
		mustAgent(t, f.store.DB.QueryRowContext(ctx, "SELECT a.state,m.ssh_trust FROM project_assets a JOIN machines m ON m.id=a.machine_id WHERE a.id=$1", f.asset).Scan(&assetState, &trust))
		if assetState != f.before.State || trust == "established" {
			t.Fatal("pre-write failure projected installation or SSH trust")
		}
	})
	t.Run("unstarted_cancel_and_scope_revocation", func(t *testing.T) {
		f, request, scope := installationKernel(t, true)
		in := CreateProvisionInstallationInput{ExpectedVersion: request.StateVersion, ScopeID: scope.ID, Reason: "isolated cancellation"}
		x, err := f.service.CreateProvisionInstallation(ctx, f.actor, f.project, request.ID, in, platform.RandomID(), "test")
		mustAgent(t, err)
		id := x.Snapshot.Targets[0].ProvisionInstallationID
		if _, _, err = f.store.IssueProvisionBootToken(ctx, f.project, id, "must not issue before activation", time.Minute); err == nil {
			t.Fatal("queued installation minted bootstrap credential")
		}
		_, err = f.service.Control(ctx, f.actor, f.project, x.ID, ControlInput{Action: "cancel", ExpectedVersion: x.StateVersion}, platform.RandomID(), "test")
		mustAgent(t, err)
		installation, err := f.store.ReadProvisionInstallation(ctx, f.actor, f.project, id)
		mustAgent(t, err)
		if installation.State != "canceled" || installation.AttemptID != nil {
			t.Fatal("unstarted cancellation did not retain no-boot proof")
		}
		if _, err = f.service.RerunCandidates(ctx, f.actor, f.project, x.ID, "failed", nil); err == nil {
			t.Fatal("generic rerun admitted native installation")
		}
		_, err = f.store.ChangeProvisionBootScope(ctx, f.reviewer, f.project, scope.ID, "revoke", platform.ProvisionBootScopeActionInput{ExpectedVersion: scope.StateVersion, Reason: "fixture:network-ownership-withdrawn"}, "test")
		mustAgent(t, err)
		if _, err = f.service.CreateProvisionInstallation(ctx, f.actor, f.project, request.ID, in, platform.RandomID(), "test"); err == nil {
			t.Fatal("revoked boot domain admitted")
		}
	})
	t.Run("expired_unactivated_attempt_keeps_resource_reconciling", func(t *testing.T) {
		f, request, scope := installationKernel(t, true)
		x, err := f.service.CreateProvisionInstallation(ctx, f.actor, f.project, request.ID, CreateProvisionInstallationInput{ExpectedVersion: request.StateVersion, ScopeID: scope.ID, Reason: "isolated lease-expiry boundary"}, platform.RandomID(), "test")
		mustAgent(t, err)
		claim := f.claim(t, x.ID)
		if claim == nil {
			t.Fatal("claim missing")
		}
		_, err = f.store.DB.ExecContext(ctx, "UPDATE exec_task_attempts SET lease_until=now()-interval '1 second' WHERE id=$1", claim.Attempt.ID)
		mustAgent(t, err)
		_, err = f.service.ReapExpired(ctx)
		mustAgent(t, err)
		installation, err := f.store.ReadProvisionInstallation(ctx, f.actor, f.project, claim.Target.ProvisionInstallationID)
		mustAgent(t, err)
		if installation.State != "failed" || installation.AttemptID != nil {
			t.Fatal("unactivated source acquired a boot identity on expiry")
		}
		var state string
		mustAgent(t, f.store.DB.QueryRowContext(ctx, "SELECT state FROM exec_resource_leases WHERE attempt_id=$1", claim.Attempt.ID).Scan(&state))
		if state != "reconciling" {
			t.Fatal("expired resource released without receipt")
		}
	})
}
