package execution

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/mediaboot"
	"github.com/enzoOvOyu/keel/internal/mediamanifest"
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/enzoOvOyu/keel/internal/provisionboot"
	"github.com/enzoOvOyu/keel/internal/provisionpreflight"
	"github.com/enzoOvOyu/keel/internal/provisionstorage"
)

// A valid, independently signed archive format with deliberately non-executed
// ELF fixtures. It exercises source verification and DB authority, never proves
// a runnable initrd, firmware/DHCP service, hardware facts or installation.
func signedProvisionImageFixture(t *testing.T, s *platform.Store, actor *Actor, project *platform.ProjectSummary, upload func(string, string, string, string, map[string]any) *platform.MediaItemSummary) (*platform.MediaItemSummary, *platform.MediaItemSummary, *platform.MediaItemSummary) {
	t.Helper()
	dir := t.TempDir()
	private, public := filepath.Join(dir, "signer.pem"), filepath.Join(dir, "public.pem")
	_, err := mediamanifest.GenerateKeyFiles(private, public)
	mustAgent(t, err)
	s.MediaManifestSigner, err = mediamanifest.LoadSigner(private)
	mustAgent(t, err)
	s.ProvisionImagePublicKey, err = mediamanifest.LoadPublicKey(public)
	mustAgent(t, err)
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	mustAgent(t, err)
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	mustAgent(t, err)
	set := mediaboot.RuntimeSet{Schema: mediaboot.RuntimeSetSchema, Arch: "amd64"}
	var preflight, ssh *platform.MediaItemSummary
	refs := []platform.MediaArtifactDependency{}
	for index, component := range []*mediaboot.RuntimeComponent{&set.Init, &set.Preflight, &set.SSHHelper, &set.SMART, &set.BootCA} {
		raw := provisionELFFormatFixture(byte(index))
		typ, arch := "os", "amd64"
		if index == 4 {
			raw = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
			typ = "config"
			arch = "noarch"
		}
		item := upload("runtime-"+string(rune('a'+index)), typ, arch, string(raw), nil)
		component.MediaID, component.SHA256 = item.ID, item.Sha256
		refs = append(refs, platform.MediaArtifactDependency{MediaID: item.ID, SHA256: item.Sha256})
		mustAgent(t, os.WriteFile(filepath.Join(dir, item.ID+".bin"), raw, 0600))
		if index == 1 {
			preflight = item
		}
		if index == 2 {
			ssh = item
		}
	}
	config, err := json.Marshal(set)
	mustAgent(t, err)
	root := upload("runtime-set", "config", "amd64", string(config), map[string]any{"artifacts": refs})
	mustAgent(t, os.WriteFile(filepath.Join(dir, root.ID+".bin"), config, 0600))
	manifest, err := s.ExportMediaManifest(context.Background(), actor, project, root.ID, "test")
	mustAgent(t, err)
	raw, err := json.Marshal(manifest)
	mustAgent(t, err)
	output := filepath.Join(t.TempDir(), "readonly-initrd.gz")
	_, err = mediamanifest.CreateInstallerInitrd(context.Background(), raw, s.ProvisionImagePublicKey, dir, output)
	mustAgent(t, err)
	image, err := os.ReadFile(output)
	mustAgent(t, err)
	return upload("signed-initrd", "os", "amd64", string(image), nil), preflight, ssh
}

func provisionELFFormatFixture(tag byte) []byte {
	raw := make([]byte, 161)
	copy(raw, []byte{0x7f, 'E', 'L', 'F', 2, 1, 1})
	binary.LittleEndian.PutUint16(raw[16:], 2)
	binary.LittleEndian.PutUint16(raw[18:], 62)
	binary.LittleEndian.PutUint32(raw[20:], 1)
	binary.LittleEndian.PutUint64(raw[24:], 0x400078)
	binary.LittleEndian.PutUint64(raw[32:], 64)
	binary.LittleEndian.PutUint16(raw[52:], 64)
	binary.LittleEndian.PutUint16(raw[54:], 56)
	binary.LittleEndian.PutUint16(raw[56:], 1)
	binary.LittleEndian.PutUint32(raw[64:], 1)
	binary.LittleEndian.PutUint32(raw[68:], 5)
	binary.LittleEndian.PutUint64(raw[80:], 0x400000)
	binary.LittleEndian.PutUint64(raw[88:], 0x400000)
	binary.LittleEndian.PutUint64(raw[96:], uint64(len(raw)))
	binary.LittleEndian.PutUint64(raw[104:], uint64(len(raw)))
	binary.LittleEndian.PutUint64(raw[112:], 4096)
	copy(raw[120:], "never executed: software format fixture")
	raw[160] = tag
	return raw
}

type liveProvisionBootFixture struct {
	*provisionKernelFixture
	scope        *platform.ProvisionBootScope
	installation *platform.ProvisionInstallation
	claim        *Claim
	authority    *platform.ProvisionBootAuthority
	key          ed25519.PrivateKey
	boot         string
	sequence     int64
	report       []byte
}

func liveProvisionBoot(t *testing.T) *liveProvisionBootFixture {
	t.Helper()
	ctx := context.Background()
	f, request, scope := installationKernelSource(t, true, true)
	x, err := f.service.CreateProvisionInstallation(ctx, f.actor, f.project, request.ID, CreateProvisionInstallationInput{ExpectedVersion: request.StateVersion, ScopeID: scope.ID, Reason: "isolated signed boot authority software check"}, platform.RandomID(), "test")
	mustAgent(t, err)
	_, err = f.service.PumpOutbox(ctx, "boot-source-test", 1000)
	mustAgent(t, err)
	claim, err := f.service.claimExecution(ctx, x.ID, "boot-source-test", 30*time.Second)
	mustAgent(t, err)
	if claim == nil {
		t.Fatal("installation claim missing")
	}
	id := x.Snapshot.Targets[0].ProvisionInstallationID
	verified, err := f.store.VerifyProvisionInstallationSources(ctx, f.project, id)
	mustAgent(t, err)
	installation, err := f.store.ActivateProvisionInstallation(ctx, f.project, id, x.ID, claim.Attempt.ID, claim.Attempt.CommandID, claim.Attempt.FencingToken, verified)
	mustAgent(t, err)
	out := &liveProvisionBootFixture{provisionKernelFixture: f, scope: scope, installation: installation, claim: claim, authority: &platform.ProvisionBootAuthority{Store: f.store}, boot: platform.RandomID()}
	t.Cleanup(func() { clear(out.key) })
	return out
}

func (f *liveProvisionBootFixture) enroll(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	_, token, err := f.store.IssueProvisionBootToken(ctx, f.project, f.installation.ID, "isolated software boot registration", time.Minute)
	mustAgent(t, err)
	public, private, err := ed25519.GenerateKey(rand.Reader)
	mustAgent(t, err)
	f.key = private
	enrollment := provisionboot.BootEnrollment{InstallationID: f.installation.ID, BootID: f.boot, PublicKey: base64.StdEncoding.EncodeToString(public)}
	raw, _ := json.Marshal(enrollment)
	proof, err := provisionboot.SignBootProof(private, f.installation.ID, f.boot, 1, "enroll", raw)
	mustAgent(t, err)
	in := provisionboot.EnrollmentSubmission{Enrollment: enrollment, Proof: proof}
	ack, err := f.authority.EnrollBoot(ctx, f.scope.Scope, f.installation.GrantID, netip.MustParseAddr("192.0.2.2"), token, in)
	mustAgent(t, err)
	if ack.State != "booting" || ack.Sequence != 1 {
		t.Fatal("registration ack")
	}
	if _, err = f.authority.EnrollBoot(ctx, f.scope.Scope, f.installation.GrantID, netip.MustParseAddr("192.0.2.2"), token, in); err == nil {
		t.Fatal("bootstrap consumed twice")
	}
	f.sequence = 1
}

func (f *liveProvisionBootFixture) reportInput(t *testing.T, refresh string) provisionboot.PreflightSubmission {
	t.Helper()
	yes, no := true, false
	now := time.Now().UTC()
	spec := *f.installation.Spec
	observation := provisionpreflight.Observation{Source: provisionpreflight.Source, Firmware: "uefi", StartedAt: now.Add(-time.Second), FinishedAt: now, Serial: spec.Expected.Serial, Arch: spec.Expected.Arch, PCI: []provisionpreflight.PCI{{Address: "0000:01:00.0", Class: "0x020000", Identity: "15b3:1021", PhysicalSlot: "fixture-slot"}}, Ports: []provisionpreflight.Port{{Name: "fixture0", MAC: spec.Expected.BootMAC, PCI: "0000:01:00.0"}}, Disks: []provisionpreflight.Disk{{Device: "/dev/sda", Serial: "fixture-disk-1", Healthy: &yes, Reason: "software fixture only", WWID: "fixture-wwid-1", SizeBytes: 32 << 30, LogicalBlockBytes: 512, DeviceNumber: "8:0", DiskSequence: 11, ReadOnly: &no, Removable: &no, InUse: &no}}}
	report, err := provisionpreflight.Evaluate(spec, observation, now)
	mustAgent(t, err)
	if report.Status != "PASS" {
		t.Fatal("fixture facts not PASS", report)
	}
	raw, _ := json.Marshal(report)
	in := provisionboot.PreflightSubmission{Report: raw, RefreshID: refresh}
	payload, operation, err := provisionboot.PreflightProofPayload(in)
	mustAgent(t, err)
	in.Proof, err = provisionboot.SignBootProof(f.key, f.installation.ID, f.boot, f.sequence+1, operation, payload)
	mustAgent(t, err)
	return in
}

func (f *liveProvisionBootFixture) submit(t *testing.T, refresh string) {
	t.Helper()
	in := f.reportInput(t, refresh)
	ack, err := f.authority.ReportPreflight(context.Background(), f.scope.Scope, f.installation.GrantID, netip.MustParseAddr("192.0.2.2"), in)
	mustAgent(t, err)
	f.sequence++
	f.report = in.Report
	if ack.Sequence != f.sequence || ack.State != "preflight_ready" {
		t.Fatal("report ack")
	}
	if _, err = f.authority.ReportPreflight(context.Background(), f.scope.Scope, f.installation.GrantID, netip.MustParseAddr("192.0.2.2"), in); err == nil {
		t.Fatal("signed report consumed twice")
	}
}

func (f *liveProvisionBootFixture) read(t *testing.T) *platform.ProvisionInstallation {
	t.Helper()
	out, err := f.store.ReadProvisionInstallation(context.Background(), f.actor, f.project, f.installation.ID)
	mustAgent(t, err)
	return out
}

func (f *liveProvisionBootFixture) review(t *testing.T) *provisionboot.ReviewStatus {
	t.Helper()
	query := provisionboot.ReviewQuery{Nonce: platform.RandomID()}
	raw, _ := json.Marshal(query)
	proof, err := provisionboot.SignBootProof(f.key, f.installation.ID, f.boot, f.sequence, "review", raw)
	mustAgent(t, err)
	out, err := f.authority.ReviewBoot(context.Background(), f.scope.Scope, f.installation.GrantID, netip.MustParseAddr("192.0.2.2"), provisionboot.ReviewSubmission{Query: query, Proof: proof})
	mustAgent(t, err)
	if out.Nonce != query.Nonce || out.Sequence != f.sequence {
		t.Fatal("status binding changed")
	}
	return out
}

func TestProvisionInstallationLiveBootPostgres(t *testing.T) {
	ctx := context.Background()
	t.Run("signed_sources_current_files_refresh_and_independent_storage_review", func(t *testing.T) {
		f := liveProvisionBoot(t)
		grant, err := f.authority.LookupBoot(ctx, f.scope.Scope, provisionboot.Client{MAC: f.installation.Spec.Expected.BootMAC, Arch: "amd64", FingerprintSHA256: strings.Repeat("a", 64)})
		mustAgent(t, err)
		peer := netip.MustParseAddr("192.0.2.2")
		for _, name := range []string{"manifest.json", "preflight.json", "user-data", "meta-data", "initrd"} {
			handle, err := f.authority.AuthorizeFile(ctx, f.scope.Scope, grant.ID, peer, name)
			mustAgent(t, err)
			file, err := f.authority.OpenFile(ctx, f.scope.Scope, handle, name)
			mustAgent(t, err)
			raw, readErr := io.ReadAll(file.File)
			file.File.Close()
			mustAgent(t, readErr)
			sum := sha256.Sum256(raw)
			if int64(len(raw)) != file.Size || file.SHA256 != hex.EncodeToString(sum[:]) {
				t.Fatal("file identity")
			}
		}
		f.enroll(t)
		f.submit(t, "")
		before := f.read(t)
		for range 3 {
			if f.review(t).State != "preflight_ready" {
				t.Fatal("status")
			}
		}
		if f.read(t).StateVersion != before.StateVersion {
			t.Fatal("read-only status changed business version")
		}
		request, err := f.store.RequestProvisionPreflightRefresh(ctx, f.actor, f.project, before.ID, platform.ProvisionRequestActionInput{ExpectedVersion: before.StateVersion, Reason: "explicit fresh disk selection facts"}, "test")
		mustAgent(t, err)
		if f.read(t).StateVersion != before.StateVersion {
			t.Fatal("refresh request changed business version")
		}
		if _, err = f.store.RequestProvisionPreflightRefresh(ctx, f.actor, f.project, before.ID, platform.ProvisionRequestActionInput{ExpectedVersion: before.StateVersion, Reason: "duplicate must not create another request"}, "test"); err == nil {
			t.Fatal("duplicate pending request")
		}
		intent := provisionstorage.Intent{Schema: provisionstorage.IntentSchema, Layout: provisionstorage.Layout, WWID: "fixture-wwid-1", Serial: "fixture-disk-1", SizeBytes: 32 << 30, LogicalBlockBytes: 512, EraseSelectedDisk: true}
		if _, err = f.store.SelectProvisionStorage(ctx, f.actor, f.project, before.ID, platform.SelectProvisionStorageInput{ExpectedVersion: before.StateVersion, Reason: "old facts must wait", Intent: intent}, "test"); err == nil {
			t.Fatal("pending refresh allowed old facts selection")
		}
		if f.review(t).RefreshID != request.ID {
			t.Fatal("request not visible to boot")
		}
		f.submit(t, request.ID)
		current := f.read(t)
		if current.StateVersion != before.StateVersion+1 || f.review(t).RefreshID != "" {
			t.Fatal("refresh not consumed exactly once")
		}
		page, err := f.store.ListProvisionInstallations(ctx, f.actor, f.project, platform.Pagination{Page: 1, PageSize: 20})
		mustAgent(t, err)
		if len(page.Items) != 1 || page.Items[0].Preflight != nil || len(page.Items[0].LatestReport) != 0 || current.Preflight == nil || !bytes.Equal(current.LatestReport, f.report) {
			t.Fatal("summary leaked report or detail lost exact evidence")
		}
		selected, err := f.store.SelectProvisionStorage(ctx, f.actor, f.project, current.ID, platform.SelectProvisionStorageInput{ExpectedVersion: current.StateVersion, Reason: "explicit stable identity erase intent", Intent: intent}, "test")
		mustAgent(t, err)
		if _, err = f.store.ApproveProvisionStorage(ctx, f.actor, f.project, current.ID, platform.ProvisionRequestActionInput{ExpectedVersion: selected.StateVersion, Reason: "self approval must fail"}, "test"); err == nil {
			t.Fatal("selector approved own disk")
		}
		approved, err := f.store.ApproveProvisionStorage(ctx, f.reviewer, f.project, current.ID, platform.ProvisionRequestActionInput{ExpectedVersion: selected.StateVersion, Reason: "independent immutable layout review"}, "test")
		mustAgent(t, err)
		if approved.State != "storage_approved" || approved.PermitIssuedAt != nil || f.review(t).State != "storage_approved" {
			t.Fatal("approval state or implicit permit")
		}
		if _, err = f.authority.ReportPreflight(ctx, f.scope.Scope, grant.ID, peer, f.reportInput(t, platform.RandomID())); err == nil {
			t.Fatal("report changed after selection")
		}
	})

	t.Run("concurrent_refresh_requests_and_current_scope_revocation", func(t *testing.T) {
		f := liveProvisionBoot(t)
		f.enroll(t)
		f.submit(t, "")
		current := f.read(t)
		var wg sync.WaitGroup
		results := make(chan error, 2)
		for range 2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := f.store.RequestProvisionPreflightRefresh(ctx, f.actor, f.project, current.ID, platform.ProvisionRequestActionInput{ExpectedVersion: current.StateVersion, Reason: "concurrent current request"}, "test")
				results <- err
			}()
		}
		wg.Wait()
		close(results)
		success := 0
		for err := range results {
			if err == nil {
				success++
			}
		}
		if success != 1 {
			t.Fatal("concurrent requests did not serialize", success)
		}
		_, err := f.store.ChangeProvisionBootScope(ctx, f.actor, f.project, f.scope.ID, "revoke", platform.ProvisionBootScopeActionInput{ExpectedVersion: f.scope.StateVersion, Reason: "withdraw current boot ownership"}, "test")
		mustAgent(t, err)
		if _, err = f.authority.ReportPreflight(ctx, f.scope.Scope, f.installation.GrantID, netip.MustParseAddr("192.0.2.2"), f.reportInput(t, platform.RandomID())); err == nil {
			t.Fatal("revoked scope accepted proof")
		}
	})
}
