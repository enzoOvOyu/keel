package execution

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/enzoOvOyu/keel/internal/provisionboot"
	"github.com/enzoOvOyu/keel/internal/provisionfirstboot"
	"golang.org/x/crypto/ssh"
)

func firstBootReadyFixture(t *testing.T, dhcp bool) (*liveProvisionBootFixture, *provisionfirstboot.Secret, *provisionboot.WritePermit) {
	t.Helper()
	ctx := context.Background()
	peer := netip.MustParseAddr("192.0.2.2")
	f := approvedProvisionWriter(t, dhcp)
	in := f.writePermitInput(t)
	status := f.review(t)
	var raw []byte
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT payload FROM provision_installation_events WHERE installation_id=$1 AND event_type='boot.activated'`, f.installation.ID).Scan(&raw))
	var runtime struct {
		Inputs []struct {
			SHA256 string `json:"sha256"`
		} `json:"runtime_inputs"`
	}
	mustAgent(t, json.Unmarshal(raw, &runtime))
	if len(runtime.Inputs) != 8 {
		t.Fatal("writer runtime input fixture missing")
	}
	b := provisionfirstboot.Binding{Schema: provisionfirstboot.BindingSchema, InstallationID: f.installation.ID, BootID: f.boot, GrantID: f.installation.GrantID, StoragePlan: *status.StoragePlan, GPTIdentity: in.Preparation.GPTIdentity, Hostname: f.request.Snapshot.Target.Hostname, Origin: f.scope.Scope.Origin, Server: f.scope.Scope.Server, Clients: f.scope.Scope.Clients, BootCASHA256: runtime.Inputs[4].SHA256, TargetUKISHA256: in.Preparation.TargetUKISHA256, RuntimeInputSHA256: status.RuntimeInputSHA256, InstallerKernelBootID: "00000000-0000-4000-8000-000000000001", CreatedAt: time.Now().UTC()}
	secret, err := provisionfirstboot.NewSecret(b, time.Now())
	mustAgent(t, err)
	in.Preparation.FirstBootBinding = &secret.Binding
	payload, err := provisionboot.WritePermitPayload(in)
	mustAgent(t, err)
	in.Proof, err = provisionboot.SignBootProof(f.key, f.installation.ID, f.boot, f.sequence+1, "write-permit", payload)
	mustAgent(t, err)
	p, err := f.authority.IssueWritePermit(ctx, f.scope.Scope, f.installation.GrantID, peer, in)
	mustAgent(t, err)
	f.sequence++
	if p.FirstBootBindingSHA256 != secret.Binding.SHA256() {
		t.Fatal("public firstboot binding not fixed in permit")
	}
	hash := func(s string) string { sum := sha256.Sum256([]byte(s)); return hex.EncodeToString(sum[:]) }
	var helper string
	for _, a := range f.request.Snapshot.Media.Artifacts {
		if a.Role == "ssh-helper" {
			helper = a.SHA256
		}
	}
	readback := provisionboot.WriteReadback{GPTSHA256: p.GPTSHA256, RootFSSHA256: p.RootFSSHA256, TargetUKISHA256: p.TargetUKISHA256, SSHHelperSHA256: helper, UserDataSHA256: hash(f.request.Snapshot.Seed.UserData), MetaDataSHA256: hash(f.request.Snapshot.Seed.MetaData)}
	r := provisionboot.WriteReceipt{PermitID: p.ID, StoragePlanSHA256: p.StoragePlanSHA256, GPTSHA256: p.GPTSHA256, RootFSSHA256: p.RootFSSHA256, TargetUKISHA256: p.TargetUKISHA256, Outcome: "written", ReadbackSHA256: readback.SHA256(), Reason: "software fixture claims only; no actual disk was used"}
	encoded, _ := json.Marshal(r)
	proof, err := provisionboot.SignBootProof(f.key, f.installation.ID, f.boot, f.sequence+1, "install-stopped", encoded)
	mustAgent(t, err)
	ack, err := f.authority.ReportWriteReceipt(ctx, f.scope.Scope, f.installation.GrantID, peer, provisionboot.WriteReceiptSubmission{Receipt: r, Proof: proof})
	mustAgent(t, err)
	f.sequence++
	if ack.State != "awaiting_first_boot" {
		t.Fatal("writer claim skipped firstboot wait")
	}
	return f, secret, p
}

func firstBootObservationFixture(t *testing.T, secret *provisionfirstboot.Secret) provisionfirstboot.Report {
	t.Helper()
	key, _, err := ed25519.GenerateKey(rand.Reader)
	mustAgent(t, err)
	host, err := ssh.NewPublicKey(key)
	mustAgent(t, err)
	b := secret.Binding
	d := b.StoragePlan.Device
	now := time.Now().UTC()
	return provisionfirstboot.Report{Schema: provisionfirstboot.ReportSchema, InstallationID: b.InstallationID, BootID: b.BootID, GrantID: b.GrantID, BindingSHA256: b.SHA256(), Sequence: 1, Nonce: platform.RandomID(), ObservedAt: now, KernelBootID: "00000000-0000-4000-8000-000000000002", Hostname: b.Hostname, Serial: b.StoragePlan.Spec.Expected.Serial, BootMAC: b.StoragePlan.Spec.Expected.BootMAC, Arch: b.StoragePlan.Spec.Expected.Arch, DiskWWID: d.WWID, DiskSerial: d.Serial, DiskSizeBytes: d.SizeBytes, LogicalBlockBytes: d.LogicalBlockBytes, RootPartitionGUID: b.GPTIdentity.Root, EFIPartitionGUID: b.GPTIdentity.EFI, HostPublicKey: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(host))), HostFingerprint: ssh.FingerprintSHA256(host), HostKeyModifiedAt: now}
}

func TestProvisionInstallationFirstBootPostgres(t *testing.T) {
	ctx := context.Background()
	peer := netip.MustParseAddr("192.0.2.2")
	t.Run("independent_key_http_and_dhcp_identity_only_once", func(t *testing.T) {
		f, secret, _ := firstBootReadyFixture(t, true)
		report := firstBootObservationFixture(t, secret)
		in, err := provisionfirstboot.Sign(secret, report, time.Now())
		mustAgent(t, err)
		ack, err := f.authority.ReportFirstBoot(ctx, f.scope.Scope, f.installation.GrantID, peer, *in)
		mustAgent(t, err)
		if provisionfirstboot.ValidateAcknowledgment(ack, secret.Binding, report, time.Now()) != nil || f.read(t).State != "awaiting_first_boot" {
			t.Fatal("firstboot claim projected final success")
		}
		if _, err = f.authority.ReportFirstBoot(ctx, f.scope.Scope, f.installation.GrantID, peer, *in); err == nil {
			t.Fatal("firstboot replay consumed again")
		}
		var trust, asset string
		mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT m.ssh_trust,a.state FROM machines m JOIN project_assets a ON a.machine_id=m.id WHERE a.id=$1`, f.asset).Scan(&trust, &asset))
		if trust != "unverified" || asset != "provisioning" {
			t.Fatal("callback silently established SSH trust")
		}
	})
	t.Run("missing_auxiliary_dhcp_cannot_complete_registration", func(t *testing.T) {
		f, secret, _ := firstBootReadyFixture(t, false)
		in, err := provisionfirstboot.Sign(secret, firstBootObservationFixture(t, secret), time.Now())
		mustAgent(t, err)
		if _, err = f.authority.ReportFirstBoot(ctx, f.scope.Scope, f.installation.GrantID, peer, *in); err == nil {
			t.Fatal("HTTP report alone ignored required auxiliary channel")
		}
	})
	t.Run("authenticated_hostname_mismatch_is_audited_without_identity", func(t *testing.T) {
		f, secret, _ := firstBootReadyFixture(t, true)
		report := firstBootObservationFixture(t, secret)
		report.Hostname = "wrong-hostname"
		in, err := provisionfirstboot.SignObserved(secret, report, time.Now())
		mustAgent(t, err)
		if _, err = f.authority.ReportFirstBoot(ctx, f.scope.Scope, f.installation.GrantID, peer, *in); err == nil {
			t.Fatal("wrong hostname registered")
		}
		var alerts, identities int
		mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT count(*) FILTER(WHERE event_type='first_boot.hostname_mismatch'),count(*) FILTER(WHERE event_type='first_boot.identity_reported') FROM provision_installation_events WHERE installation_id=$1`, f.installation.ID).Scan(&alerts, &identities))
		if alerts != 1 || identities != 0 {
			t.Fatal("hostname mismatch not distinguished from registered identity")
		}
	})
}
