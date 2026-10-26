package platform

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestProvisionProgressDoesNotInventMissingStages(t *testing.T) {
	now := time.Now().UTC()
	x := &ProvisionInstallation{ID: strings.Repeat("a", 32), State: "registered", StateVersion: 9}
	facts := []provisionProgressFact{{ProvisionProgressEvidence: ProvisionProgressEvidence{ID: 1, Kind: "first_boot.identity_reported", At: now}}, {ProvisionProgressEvidence: ProvisionProgressEvidence{ID: 2, Kind: "first_boot.ssh_verified", At: now}}}
	p := projectProvisionProgress(x, facts, ProvisionProgressResource{LeaseState: "released"}, now)
	if len(p.Stages) != 7 {
		t.Fatal("seven ordered stages required")
	}
	for _, i := range []int{0, 1, 2, 3} {
		if p.Stages[i].Status != "pending" {
			t.Fatalf("stage %s was inferred from later facts", p.Stages[i].ID)
		}
	}
	if p.Stages[4].Status != "reported" || p.Stages[5].Status != "verified" || p.Stages[6].Status != "verified" {
		t.Fatal("actual signed callback and SSH facts must remain distinct")
	}
}

func TestProvisionProgressDHCPAndEnrollmentAreOnlyObserved(t *testing.T) {
	now := time.Now().UTC()
	x := &ProvisionInstallation{State: "booting"}
	facts := []provisionProgressFact{{ProvisionProgressEvidence: ProvisionProgressEvidence{ID: 1, Kind: "boot.dhcp_observed", At: now}}, {ProvisionProgressEvidence: ProvisionProgressEvidence{ID: 2, Kind: "boot.enrolled", At: now}}}
	p := projectProvisionProgress(x, facts, ProvisionProgressResource{LeaseState: "none"}, now)
	if p.Stages[0].Status != "observed" || p.Stages[0].Reason != "dhcp_auxiliary_only" || p.Stages[1].Status != "observed" || p.Stages[2].Status != "pending" || p.Stages[5].Status != "pending" {
		t.Fatal("DHCP/enrollment cannot prove lease grant, download, callback or SSH")
	}
}

func TestProvisionProgressUnknownDoesNotInventHeldLease(t *testing.T) {
	now := time.Now().UTC()
	x := &ProvisionInstallation{State: "unknown"}
	p := projectProvisionProgress(x, nil, ProvisionProgressResource{MachineReserved: true, LeaseState: "released", LeaseExpired: true}, now)
	if !p.Resource.MachineReserved || !p.Resource.ReconciliationRequired || p.Resource.LeaseState != "released" {
		t.Fatal("unknown reservation and actual lease state must be independent")
	}
}

func TestProvisionProgressReceiptAndRepairAreNotTrust(t *testing.T) {
	now := time.Now().UTC()
	x := &ProvisionInstallation{State: "awaiting_first_boot"}
	facts := []provisionProgressFact{{ProvisionProgressEvidence: ProvisionProgressEvidence{ID: 1, Kind: "write.receipt_reported", At: now}, Outcome: "written"}, {ProvisionProgressEvidence: ProvisionProgressEvidence{ID: 2, Kind: "first_boot.identity_reported", At: now}}, {ProvisionProgressEvidence: ProvisionProgressEvidence{ID: 3, Kind: "first_boot.ssh_started", At: now}}, {ProvisionProgressEvidence: ProvisionProgressEvidence{ID: 4, Kind: "first_boot.ssh_repair_required", At: now}}}
	p := projectProvisionProgress(x, facts, ProvisionProgressResource{MachineReserved: true, LeaseState: "reconciling"}, now)
	if p.Stages[3].Status != "reported" || p.Stages[6].Status != "unknown" || !p.Resource.ReconciliationRequired {
		t.Fatal("receipt/repair must not establish SSH trust")
	}
	raw, _ := json.Marshal(p)
	for _, secret := range []string{"payload", "signature", "private_key", "report_sha256"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("raw or privileged event field exposed: %s", secret)
		}
	}
}

func TestProvisionProgressHostnameMismatchIsRegistrationFailure(t *testing.T) {
	now := time.Now().UTC()
	x := &ProvisionInstallation{State: "awaiting_first_boot"}
	p := projectProvisionProgress(x, []provisionProgressFact{{ProvisionProgressEvidence: ProvisionProgressEvidence{ID: 1, Kind: "first_boot.hostname_mismatch", At: now}}}, ProvisionProgressResource{LeaseState: "held"}, now)
	if p.Stages[5].Status != "failed" || p.Stages[5].Reason != "hostname_mismatch" || p.Stages[6].Status != "pending" {
		t.Fatal("hostname mismatch must be visible and cannot authorize SSH")
	}
}

func TestProvisionProgressDeliveryDoesNotProveExecutionOrClientHash(t *testing.T) {
	now := time.Now().UTC()
	x := &ProvisionInstallation{State: "booting"}
	facts := []provisionProgressFact{
		{ProvisionProgressEvidence: ProvisionProgressEvidence{ID: 1, Kind: "boot.file_served", At: now}, Resource: "boot.ipxe"},
		{ProvisionProgressEvidence: ProvisionProgressEvidence{ID: 2, Kind: "boot.file_served", At: now}, Resource: "rootfs"},
		{ProvisionProgressEvidence: ProvisionProgressEvidence{ID: 3, Kind: "boot.file_served", At: now}, Resource: "kernel"},
		{ProvisionProgressEvidence: ProvisionProgressEvidence{ID: 4, Kind: "boot.file_served", At: now}, Resource: "../../arbitrary"},
	}
	p := projectProvisionProgress(x, facts, ProvisionProgressResource{LeaseState: "held"}, now)
	if p.Stages[1].Status != "observed" || p.Stages[2].Status != "reported" || len(p.Stages[2].Evidence) != 2 || p.Stages[3].Status != "pending" {
		t.Fatal("server delivery must not claim client hash, execution or installation")
	}
}

func TestProvisionProgressResolvedSSHDoesNotRetainStaleRepairFlag(t *testing.T) {
	now := time.Now().UTC()
	x := &ProvisionInstallation{State: "registered"}
	facts := []provisionProgressFact{{ProvisionProgressEvidence: ProvisionProgressEvidence{ID: 1, Kind: "first_boot.ssh_repair_required", At: now}}, {ProvisionProgressEvidence: ProvisionProgressEvidence{ID: 2, Kind: "first_boot.ssh_verified", At: now}}}
	p := projectProvisionProgress(x, facts, ProvisionProgressResource{LeaseState: "released"}, now)
	if p.Resource.ReconciliationRequired || p.Stages[6].Status != "verified" {
		t.Fatal("resolved SSH must replace the earlier repair candidate")
	}
}
