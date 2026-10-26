package execution

import (
	"context"
	"testing"

	"github.com/enzoOvOyu/keel/internal/platform"
)

// Synthetic already-committed SSH/native proof, never a real boot measurement.
func TestProvisionInstallationRolloutPostgres(t *testing.T) {
	ctx := context.Background()
	t.Run("registration_without_native_settlement_does_not_release", func(t *testing.T) {
		f := persistedProvisionRegistrationFixture(t, false)
		preview, err := f.store.PreviewProvisionRollout(ctx, f.actor, f.project, f.request.ID)
		mustAgent(t, err)
		if preview.ReleaseReady || len(preview.Canaries) != 1 || preview.Canaries[0].Verified {
			t.Fatal("domain registration bypassed native settlement")
		}
		mustAgent(t, f.service.Complete(ctx, f.claim))
		preview, err = f.store.PreviewProvisionRollout(ctx, f.actor, f.project, f.request.ID)
		mustAgent(t, err)
		if !preview.ReleaseReady || len(preview.Canaries) != 1 || !preview.Canaries[0].Verified || preview.Canaries[0].InstallationID != f.installation.ID {
			t.Fatal("settled immutable proof missing")
		}
		progress, err := f.store.ReadProvisionInstallationProgress(ctx, f.actor, f.project, f.installation.ID)
		mustAgent(t, err)
		if progress.Rollout == nil || progress.Rollout.Mode != "canary" || progress.Rollout.IdentitySHA256 != preview.IdentitySHA256 {
			t.Fatal("rollout selection missing from progress")
		}
		if _, err = f.store.PreviewProvisionRollout(ctx, f.actor, platform.RandomID(), f.request.ID); err == nil {
			t.Fatal("canary evidence crossed project")
		}
		_, err = f.store.DB.ExecContext(ctx, `UPDATE provision_installation_rollouts SET mode='validated',canary_ids=jsonb_build_array(installation_id) WHERE installation_id=$1`, f.installation.ID)
		if err == nil {
			t.Fatal("immutable canary intent changed")
		}
	})
	t.Run("wrong_transcript_cannot_be_canary_success", func(t *testing.T) {
		f := persistedProvisionRegistrationFixture(t, true)
		if err := f.service.Complete(ctx, f.claim); err == nil {
			t.Fatal("invalid transcript settled")
		}
		preview, err := f.store.PreviewProvisionRollout(ctx, f.actor, f.project, f.request.ID)
		mustAgent(t, err)
		if preview.ReleaseReady || preview.Canaries[0].Verified {
			t.Fatal("invalid transcript released rollout")
		}
	})
}
