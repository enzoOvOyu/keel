package execution

import (
	"context"
	"encoding/json"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/enzoOvOyu/keel/internal/provisionboot"
	"github.com/enzoOvOyu/keel/internal/provisionstorage"
)

// These use the real signed media/approval/lease/DB path, with non-executed
// binary formats and reported software facts. No archive is booted or disk used.
func approvedProvisionWriter(t *testing.T, dhcp ...bool) *liveProvisionBootFixture {
	t.Helper()
	ctx := context.Background()
	f := liveProvisionBoot(t, true)
	if len(dhcp) > 0 && dhcp[0] {
		_, err := f.authority.LookupBoot(ctx, f.scope.Scope, provisionboot.Client{MAC: f.installation.Spec.Expected.BootMAC, Arch: f.installation.Spec.Expected.Arch, FingerprintSHA256: strings.Repeat("f", 64)})
		mustAgent(t, err)
	}
	f.enroll(t)
	f.submit(t, "")
	x := f.read(t)
	intent := provisionstorage.Intent{Schema: provisionstorage.IntentSchema, Layout: provisionstorage.Layout, WWID: "fixture-wwid-1", Serial: "fixture-disk-1", SizeBytes: 32 << 30, LogicalBlockBytes: 512, EraseSelectedDisk: true}
	x, err := f.store.SelectProvisionStorage(ctx, f.actor, f.project, x.ID, platform.SelectProvisionStorageInput{ExpectedVersion: x.StateVersion, Reason: "explicit fixture target", Intent: intent}, "test")
	mustAgent(t, err)
	_, err = f.store.ApproveProvisionStorage(ctx, f.reviewer, f.project, x.ID, platform.ProvisionRequestActionInput{ExpectedVersion: x.StateVersion, Reason: "independent fixture layout review"}, "test")
	mustAgent(t, err)
	return f
}
func (f *liveProvisionBootFixture) writePermitInput(t *testing.T) provisionboot.WritePermitSubmission {
	t.Helper()
	status := f.review(t)
	if status.StoragePlan == nil || status.RuntimeInputSHA256 == "" {
		t.Fatal("approved plan or runtime commitment absent")
	}
	ids, err := provisionstorage.NewGPTIdentity()
	mustAgent(t, err)
	gpt, err := provisionstorage.PrepareGPT(*status.StoragePlan, ids, time.Now())
	mustAgent(t, err)
	report := f.reportInput(t, "").Report
	var root string
	for _, a := range f.request.Snapshot.Media.Artifacts {
		if a.Role == "rootfs" {
			root = a.SHA256
		}
	}
	in := provisionboot.WritePermitSubmission{Preparation: provisionboot.WritePreparation{Nonce: platform.RandomID(), StoragePlanSHA256: status.StoragePlanSHA256, GPTIdentity: ids, GPTSHA256: gpt.SHA256(), RootFSSHA256: root, TargetUKISHA256: f.request.Snapshot.Media.Profile.Recipe.TargetBoot.SHA256, RuntimeInputSHA256: status.RuntimeInputSHA256}, Report: report}
	payload, err := provisionboot.WritePermitPayload(in)
	mustAgent(t, err)
	in.Proof, err = provisionboot.SignBootProof(f.key, f.installation.ID, f.boot, f.sequence+1, "write-permit", payload)
	mustAgent(t, err)
	return in
}
func TestProvisionInstallationWritePermitPostgres(t *testing.T) {
	ctx := context.Background()
	peer := netip.MustParseAddr("192.0.2.2")
	t.Run("fresh_facts_gpt_once_and_readonly_check", func(t *testing.T) {
		f := approvedProvisionWriter(t)
		in := f.writePermitInput(t)
		bad := in
		bad.Preparation.GPTSHA256 = strings.Repeat("f", 64)
		payload, _ := provisionboot.WritePermitPayload(bad)
		bad.Proof, _ = provisionboot.SignBootProof(f.key, f.installation.ID, f.boot, f.sequence+1, "write-permit", payload)
		if _, err := f.authority.IssueWritePermit(ctx, f.scope.Scope, f.installation.GrantID, peer, bad); err == nil {
			t.Fatal("wrong actual GPT commitment accepted")
		}
		var assetVersion int64
		mustAgent(t, f.store.DB.QueryRowContext(ctx, "SELECT version FROM project_assets WHERE id=$1", f.asset).Scan(&assetVersion))
		if assetVersion != f.request.Snapshot.Target.AssetVersion {
			t.Fatal("denied permit modified asset")
		}
		p, err := f.authority.IssueWritePermit(ctx, f.scope.Scope, f.installation.GrantID, peer, in)
		mustAgent(t, err)
		f.sequence++
		x := f.read(t)
		if x.PermitIssuedAt == nil || x.State != "installing" {
			t.Fatal("permit not persisted before response")
		}
		var assetState, trust string
		mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT a.version,a.state,m.ssh_trust FROM project_assets a JOIN machines m ON m.id=a.machine_id WHERE a.id=$1`, f.asset).Scan(&assetVersion, &assetState, &trust))
		if assetVersion != f.request.Snapshot.Target.AssetVersion+1 || assetState != "provisioning" || trust != "unverified" {
			t.Fatal("permit did not atomically activate own asset revision")
		}
		if _, err = f.authority.IssueWritePermit(ctx, f.scope.Scope, f.installation.GrantID, peer, in); err == nil {
			t.Fatal("permit replay allowed")
		}
		for _, stage := range []string{"gpt.write", "root.extract"} {
			q := provisionboot.WriteCheckQuery{PermitID: p.ID, Nonce: platform.RandomID(), Stage: stage}
			raw, _ := json.Marshal(q)
			proof, _ := provisionboot.SignBootProof(f.key, f.installation.ID, f.boot, f.sequence, "write-check", raw)
			a, err := f.authority.CheckWritePermit(ctx, f.scope.Scope, f.installation.GrantID, peer, provisionboot.WriteCheckSubmission{Query: q, Proof: proof})
			mustAgent(t, err)
			if provisionboot.ValidateWriteAuthorization(a, p, q, time.Now()) != nil || f.read(t).StateVersion != x.StateVersion {
				t.Fatal("check changed version or wrong binding")
			}
		}
		if _, err = f.store.StopProvisionBeforeWrite(ctx, f.project, x.ID, x.ExecutionID, true, "connection_lost_after_permit"); err == nil {
			t.Fatal("post-permit became safe stop")
		}
		if f.read(t).State != "unknown" {
			t.Fatal("post-permit uncertainty not retained")
		}
		mustAgent(t, f.store.DB.QueryRowContext(ctx, "SELECT state FROM project_assets WHERE id=$1", f.asset).Scan(&assetState))
		if assetState != "provisioning" {
			t.Fatal("unknown write falsely released provisioning asset")
		}
		var held int
		mustAgent(t, f.store.DB.QueryRowContext(ctx, "SELECT count(*) FROM exec_resource_leases WHERE attempt_id=$1 AND state='held'", f.claim.Attempt.ID).Scan(&held))
		if held != 1 {
			t.Fatal("uncertain disk resource released")
		}
	})
	t.Run("approval_revocation_denies_existing_permit", func(t *testing.T) {
		f := approvedProvisionWriter(t)
		in := f.writePermitInput(t)
		p, err := f.authority.IssueWritePermit(ctx, f.scope.Scope, f.installation.GrantID, peer, in)
		mustAgent(t, err)
		f.sequence++
		scope, err := f.store.ChangeProvisionBootScope(ctx, f.actor, f.project, f.scope.ID, "revoke", platform.ProvisionBootScopeActionInput{ExpectedVersion: f.scope.StateVersion, Reason: "fixture revoked live domain"}, "test")
		mustAgent(t, err)
		if scope.State != "revoked" {
			t.Fatal("scope not revoked")
		}
		q := provisionboot.WriteCheckQuery{PermitID: p.ID, Nonce: platform.RandomID(), Stage: "efi.format"}
		raw, _ := json.Marshal(q)
		proof, _ := provisionboot.SignBootProof(f.key, f.installation.ID, f.boot, f.sequence, "write-check", raw)
		if _, err = f.authority.CheckWritePermit(ctx, f.scope.Scope, f.installation.GrantID, peer, provisionboot.WriteCheckSubmission{Query: q, Proof: proof}); err == nil {
			t.Fatal("revoked scope refreshed write authority")
		}
	})
	t.Run("signed_unknown_receipt_never_registers_or_releases", func(t *testing.T) {
		f := approvedProvisionWriter(t)
		in := f.writePermitInput(t)
		p, err := f.authority.IssueWritePermit(ctx, f.scope.Scope, f.installation.GrantID, peer, in)
		mustAgent(t, err)
		f.sequence++
		r := provisionboot.WriteReceipt{PermitID: p.ID, StoragePlanSHA256: p.StoragePlanSHA256, GPTSHA256: p.GPTSHA256, RootFSSHA256: p.RootFSSHA256, TargetUKISHA256: p.TargetUKISHA256, Outcome: "unknown", Reason: "fixture interrupted after initial write"}
		raw, _ := json.Marshal(r)
		proof, _ := provisionboot.SignBootProof(f.key, f.installation.ID, f.boot, f.sequence+1, "install-stopped", raw)
		ack, err := f.authority.ReportWriteReceipt(ctx, f.scope.Scope, f.installation.GrantID, peer, provisionboot.WriteReceiptSubmission{Receipt: r, Proof: proof})
		mustAgent(t, err)
		if ack.State != "unknown" || f.read(t).State != "unknown" {
			t.Fatal("partial write claimed success")
		}
		var held int
		mustAgent(t, f.store.DB.QueryRowContext(ctx, "SELECT count(*) FROM exec_resource_leases WHERE attempt_id=$1 AND state='held'", f.claim.Attempt.ID).Scan(&held))
		if held != 1 {
			t.Fatal("unknown callback released exclusive resource")
		}
	})
}
