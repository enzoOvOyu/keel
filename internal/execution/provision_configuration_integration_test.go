package execution

import (
	"context"
	"testing"

	"github.com/enzoOvOyu/keel/internal/platform"
)

// Synthetic committed registration tests current read projection only. No SSH
// handshake, new trust proof or device installation is performed by the fixture.
func TestProvisionInstallationConfigurationPostgres(t *testing.T) {
	ctx := context.Background()
	f := persistedProvisionRegistrationFixture(t, false)
	read := func() *platform.ProvisionInstallationProgress {
		t.Helper()
		out, err := f.store.ReadProvisionInstallationProgress(ctx, f.actor, f.project, f.installation.ID)
		mustAgent(t, err)
		if out.Stages[6].Status != "verified" {
			t.Fatal("current configuration erased historical SSH fact")
		}
		return out
	}
	if read().SSHConfiguration != "configuration_required" {
		t.Fatal("registration implied ordinary runner configuration")
	}
	_, err := f.store.DB.ExecContext(ctx, `UPDATE machines SET ssh_trust='revoked' WHERE id=$1`, f.machine)
	mustAgent(t, err)
	if read().SSHConfiguration != "identity_changed" {
		t.Fatal("revoked trust retained a current configuration match")
	}
	_, err = f.store.DB.ExecContext(ctx, `UPDATE machines SET ssh_trust='established' WHERE id=$1`, f.machine)
	mustAgent(t, err)
	key := f.request.Snapshot.DomainKey
	_, err = f.store.RevokeSSHDomainKey(ctx, f.project, key.ID, 1, "read-only configuration projection fixture")
	mustAgent(t, err)
	if read().SSHConfiguration != "credential_unavailable" {
		t.Fatal("revoked current key remained available")
	}
}
