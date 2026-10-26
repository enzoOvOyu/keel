package execution

import (
	"context"
	"net/netip"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/provisionfirstboot"
)

// Isolated signed-report fixture; no actual OS/driver or SSH measurement.
func TestProvisionInstallationBaselinePostgres(t *testing.T) {
	ctx := context.Background()
	for _, observed := range []bool{false, true} {
		name := "legacy_unknown"
		if observed {
			name = "signed_observation_without_trust_promotion"
		}
		t.Run(name, func(t *testing.T) {
			f, secret, _ := firstBootReadyFixture(t, true)
			report := firstBootObservationFixture(t, secret)
			if observed {
				report.SoftwareBaseline = &provisionfirstboot.SoftwareBaseline{OSID: "ubuntu", OSVersion: "24.04", KernelRelease: "6.8.0-fixture", PackageSource: "dpkg-status/driver-packages-v1", PackageStatus: "observed", DriverPackages: []provisionfirstboot.DriverPackage{{Name: "nvidia-driver-580", Version: "580.1-1", Arch: "amd64"}}}
			}
			in, err := provisionfirstboot.Sign(secret, report, time.Now())
			mustAgent(t, err)
			_, err = f.authority.ReportFirstBoot(ctx, f.scope.Scope, f.installation.GrantID, netip.MustParseAddr("192.0.2.2"), *in)
			mustAgent(t, err)
			progress, err := f.store.ReadProvisionInstallationProgress(ctx, f.actor, f.project, f.installation.ID)
			mustAgent(t, err)
			baseline := progress.InstalledBaseline
			if baseline == nil || baseline.SnapshotSHA256 != f.request.SnapshotSHA256 || len(baseline.Media) != 2 {
				t.Fatal("frozen source baseline missing")
			}
			if !observed && baseline.Observation != nil {
				t.Fatal("legacy software inventory inferred")
			}
			if observed && (baseline.Observation == nil || baseline.Observation.Software.DriverPackages[0].Version != "580.1-1" || baseline.Observation.ReportSHA256 == "" || !baseline.Observation.ObservedAt.Equal(report.ObservedAt)) {
				t.Fatal("signed observation not preserved")
			}
			if progress.State != "awaiting_first_boot" || progress.Stages[6].Status == "verified" {
				t.Fatal("software inventory promoted SSH trust")
			}
		})
	}
}
