package execution

import (
	"context"
	"testing"

	"github.com/enzoOvOyu/keel/internal/platform"
)

// This fixture records protocol/database facts only, without any SSH transport or
// real installation. Queue membership is evidence, never retry authorization.
func TestProvisionInstallationRepairListPostgres(t *testing.T) {
	ctx := context.Background()
	f, secret, _ := firstBootReadyFixture(t, true)
	reportFirstBootSSHFixture(t, f, secret)
	page := platform.Pagination{Page: 1, PageSize: 1}
	read := func() platform.Page[platform.ProvisionInstallation] {
		t.Helper()
		out, err := f.store.ListProvisionInstallationRepairs(ctx, f.actor, f.project, page)
		mustAgent(t, err)
		return out
	}
	if out := read(); out.Total != 0 || len(out.Items) != 0 {
		t.Fatal("firstboot report alone fabricated a repair")
	}
	c := f.claim
	work, err := f.store.PrepareProvisionFirstSSH(ctx, f.project, f.installation.ID, c.Attempt.ExecutionID, c.Attempt.ID, c.Attempt.CommandID, c.Attempt.FencingToken)
	mustAgent(t, err)
	if work == nil {
		t.Fatal("missing fixture dispatch")
	}
	mustAgent(t, f.store.RecordProvisionFirstSSHRepair(ctx, f.project, f.installation.ID, c.Attempt.ID, c.Attempt.CommandID, c.Attempt.FencingToken, "verification_unknown"))
	out := read()
	if out.Total != 1 || len(out.Items) != 1 || out.Items[0].ID != f.installation.ID || out.Items[0].Preflight != nil || len(out.Items[0].LatestReport) != 0 {
		t.Fatal("repair list lost scope, total or bounded summary")
	}
	all, err := f.store.ListProvisionInstallations(ctx, f.actor, f.project, page)
	mustAgent(t, err)
	if all.Total != 1 || len(all.Items) != 1 {
		t.Fatal("normal installation list changed")
	}
	page.Page = 2
	if out = read(); out.Total != 1 || len(out.Items) != 0 {
		t.Fatal("filtered pagination inconsistent")
	}
	page.Page = 1
	expireRegisteredProvisionFixture(t, f)
	out = read()
	if out.Total != 1 || len(out.Items) != 1 || out.Items[0].State != "unknown" {
		t.Fatal("lease expiry hid quarantined repair")
	}
	if _, err = f.store.ListProvisionInstallationRepairs(ctx, f.actor, platform.RandomID(), page); err == nil {
		t.Fatal("repair listing ignored project authority")
	}
	if _, err = f.store.ListProvisionInstallationRepairs(ctx, nil, f.project, page); err == nil {
		t.Fatal("repair listing accepted anonymous actor")
	}
	if _, err = f.store.ListProvisionInstallationRepairs(ctx, f.actor, f.project, platform.Pagination{}); err == nil {
		t.Fatal("repair listing accepted invalid pagination")
	}
	// Synthetic terminal projection solely exercises the read filter. It is not
	// proof of a failed device operation or permission to release its native claim.
	_, err = f.store.DB.ExecContext(ctx, `UPDATE provision_installations SET state='failed',state_version=state_version+1 WHERE id=$1`, f.installation.ID)
	mustAgent(t, err)
	if out = read(); out.Total != 0 || len(out.Items) != 0 {
		t.Fatal("terminal installation remained in repair queue")
	}
}
