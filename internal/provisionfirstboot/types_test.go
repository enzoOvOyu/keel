package provisionfirstboot

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/provisionpreflight"
	"github.com/enzoOvOyu/keel/internal/provisionstorage"
	"golang.org/x/crypto/ssh"
)

// Identity, hardware and key material below are isolated software fixtures.
// They are not collected from a device or used in an actual SSH connection.
func firstBootFixture(t *testing.T, now time.Time) (*Secret, Report) {
	t.Helper()
	one, zero, yes, no := 1, 0, true, false
	spec := provisionpreflight.Spec{Schema: provisionpreflight.Schema, ProjectID: strings.Repeat("1", 32), RequestID: strings.Repeat("2", 32), MachineID: strings.Repeat("3", 32), AttemptID: strings.Repeat("4", 32), CommandID: strings.Repeat("5", 32), FencingToken: 17, SnapshotSHA256: strings.Repeat("a", 64), ExpiresAt: now.Add(10 * time.Minute), Expected: provisionpreflight.Expectation{Serial: "firstboot-fixture", Arch: "amd64", BootMAC: "02:00:00:00:00:01", NICCount: &one, GPUCount: &zero, MinimumDisks: 1}}
	intent := provisionstorage.Intent{Schema: provisionstorage.IntentSchema, Layout: provisionstorage.Layout, WWID: "eui.0123456789abcdef", Serial: "fixture-disk", SizeBytes: 1 << 40, LogicalBlockBytes: 4096, EraseSelectedDisk: true}
	observation := provisionpreflight.Observation{Source: provisionpreflight.Source, Firmware: "uefi", StartedAt: now.Add(-time.Second), FinishedAt: now, Serial: spec.Expected.Serial, Arch: "amd64", PCI: []provisionpreflight.PCI{{Address: "0000:01:00.0", Class: "0x020000", Identity: "15b3:1021", PhysicalSlot: "slot-1"}}, Ports: []provisionpreflight.Port{{Name: "eth0", MAC: spec.Expected.BootMAC, PCI: "0000:01:00.0"}}, Disks: []provisionpreflight.Disk{{Device: "/dev/nvme0n1", Healthy: &yes, Reason: "fixture", WWID: intent.WWID, Serial: intent.Serial, SizeBytes: intent.SizeBytes, LogicalBlockBytes: intent.LogicalBlockBytes, DeviceNumber: "259:0", DiskSequence: 1, ReadOnly: &no, Removable: &no, InUse: &no}}}
	report, err := provisionpreflight.Evaluate(spec, observation, now)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(report)
	plan, err := provisionstorage.Build(spec, raw, intent, now)
	if err != nil {
		t.Fatal(err)
	}
	binding := Binding{Schema: BindingSchema, InstallationID: strings.Repeat("6", 32), BootID: strings.Repeat("7", 32), GrantID: strings.Repeat("8", 32), StoragePlan: *plan, GPTIdentity: provisionstorage.GPTIdentity{Disk: "11111111-1111-4111-8111-111111111111", EFI: "22222222-2222-4222-8222-222222222222", Root: "33333333-3333-4333-8333-333333333333"}, Hostname: "fixture-node", Origin: "https://boot.example.test", Server: netip.MustParseAddr("192.0.2.1"), Clients: netip.MustParsePrefix("192.0.2.0/24"), BootCASHA256: strings.Repeat("b", 64), TargetUKISHA256: strings.Repeat("c", 64), RuntimeInputSHA256: strings.Repeat("d", 64), InstallerKernelBootID: "44444444-4444-4444-8444-444444444444", CreatedAt: now.Add(-10 * time.Second)}
	secret, err := NewSecret(binding, now)
	if err != nil {
		t.Fatal(err)
	}
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	host, err := ssh.NewPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	actual := Report{Schema: ReportSchema, InstallationID: binding.InstallationID, BootID: binding.BootID, GrantID: binding.GrantID, BindingSHA256: secret.Binding.SHA256(), Sequence: 1, Nonce: strings.Repeat("9", 32), ObservedAt: now, KernelBootID: "55555555-5555-4555-8555-555555555555", Hostname: binding.Hostname, Serial: spec.Expected.Serial, BootMAC: spec.Expected.BootMAC, Arch: "amd64", DiskWWID: intent.WWID, DiskSerial: intent.Serial, DiskSizeBytes: intent.SizeBytes, LogicalBlockBytes: intent.LogicalBlockBytes, RootPartitionGUID: binding.GPTIdentity.Root, EFIPartitionGUID: binding.GPTIdentity.EFI, HostPublicKey: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(host))), HostFingerprint: ssh.FingerprintSHA256(host), HostKeyModifiedAt: now.Add(-2 * time.Second)}
	return secret, actual
}

func TestFirstBootProofBindsNewBootDiskAndActualHostKey(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	secret, report := firstBootFixture(t, now)
	in, err := Sign(secret, report, now)
	if err != nil || VerifySubmission(secret.Binding, *in, now) != nil {
		t.Fatal("valid first boot proof rejected", err)
	}
	for name, change := range map[string]func(*Report){
		"same_installer_kernel":  func(r *Report) { r.KernelBootID = secret.Binding.InstallerKernelBootID },
		"foreign_installation":   func(r *Report) { r.InstallationID = strings.Repeat("a", 32) },
		"foreign_binding":        func(r *Report) { r.BindingSHA256 = strings.Repeat("e", 64) },
		"foreign_disk":           func(r *Report) { r.DiskWWID = "eui.foreign" },
		"wrong_root_partition":   func(r *Report) { r.RootPartitionGUID = secret.Binding.GPTIdentity.EFI },
		"wrong_host_fingerprint": func(r *Report) { r.HostFingerprint = "SHA256:other" },
		"old_image_host_key":     func(r *Report) { r.HostKeyModifiedAt = secret.Binding.CreatedAt.Add(-time.Second) },
		"stale_observation":      func(r *Report) { r.ObservedAt = now.Add(-31 * time.Second) },
		"wrong_sequence":         func(r *Report) { r.Sequence = 2 },
		"changed_nonce":          func(r *Report) { r.Nonce = strings.Repeat("a", 32) },
		"hostname_mismatch":      func(r *Report) { r.Hostname = "other-node" },
	} {
		t.Run(name, func(t *testing.T) {
			copy := *in
			change(&copy.Report)
			if VerifySubmission(secret.Binding, copy, now) == nil {
				t.Fatal("modified observation retained valid proof")
			}
		})
	}
	copy := *in
	copy.Signature = strings.Repeat("A", len(copy.Signature))
	if VerifySignature(secret.Binding, copy, now) == nil {
		t.Fatal("forged signature accepted")
	}
}

func TestFirstBootAuthenticatedHostnameMismatchIsDiagnosticOnly(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	secret, report := firstBootFixture(t, now)
	report.Hostname = "wrong-hostname"
	if _, err := Sign(secret, report, now); err == nil {
		t.Fatal("strict signature accepted mismatch")
	}
	in, err := SignObserved(secret, report, now)
	if err != nil || VerifySignature(secret.Binding, *in, now) != nil {
		t.Fatal("authenticated diagnostic lost", err)
	}
	if VerifySubmission(secret.Binding, *in, now) == nil || ValidateReport(secret.Binding, in.Report, now) == nil {
		t.Fatal("diagnostic established identity")
	}
	report.Serial = "foreign"
	if _, err := SignObserved(secret, report, now); err == nil {
		t.Fatal("hostname exception weakened machine identity")
	}
}

func TestFirstBootSecretAndSubmissionRequireCanonicalJSON(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	secret, report := firstBootFixture(t, now)
	raw, _ := json.Marshal(secret)
	decoded, err := DecodeSecret(raw, now)
	if err != nil || decoded.Binding.SHA256() != secret.Binding.SHA256() {
		t.Fatal("canonical secret rejected", err)
	}
	for _, bad := range [][]byte{append(bytes.Clone(raw), '\n'), bytes.Replace(raw, []byte(`"private_key":`), []byte(`"Private_key":`), 1), bytes.Replace(raw, []byte(`"binding":`), []byte(`"unknown":`), 1)} {
		if _, err := DecodeSecret(bad, now); err == nil {
			t.Fatal("noncanonical secret accepted")
		}
	}
	wrong := *secret
	wrong.PrivateKey = strings.Repeat("A", 88)
	bad, _ := json.Marshal(wrong)
	if _, err := DecodeSecret(bad, now); err == nil {
		t.Fatal("unbound private key accepted")
	}
	in, err := Sign(secret, report, now)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(in)
	if _, err := DecodeSubmission(raw); err != nil {
		t.Fatal(err)
	}
	bad = bytes.Replace(raw, []byte(`"signature":`), []byte(`"Signature":`), 1)
	if _, err := DecodeSubmission(bad); err == nil {
		t.Fatal("signature alias accepted")
	}
}

type fixtureCollector struct {
	report Report
	calls  int
	err    error
}

func (c *fixtureCollector) Collect(context.Context, Binding) (Report, error) {
	c.calls++
	return c.report, c.err
}

type fixtureTransport struct {
	binding    Binding
	calls      int
	err        error
	wrongNonce bool
}

func (c *fixtureTransport) Submit(_ context.Context, in Submission) (*Acknowledgment, error) {
	c.calls++
	if c.err != nil {
		return nil, c.err
	}
	nonce := in.Report.Nonce
	if c.wrongNonce {
		nonce = strings.Repeat("a", 32)
	}
	return &Acknowledgment{InstallationID: c.binding.InstallationID, BootID: c.binding.BootID, BindingSHA256: c.binding.SHA256(), Nonce: nonce, State: "awaiting_first_boot", HostFingerprint: in.Report.HostFingerprint, ReceivedAt: in.Report.ObservedAt}, nil
}

type fixtureJournal struct {
	begun           bool
	submission, ack []byte
	failAck         bool
}

func (j *fixtureJournal) Begin(context.Context, string) error {
	if j.begun {
		return ErrDenied
	}
	j.begun = true
	return nil
}
func (j *fixtureJournal) SaveSubmission(_ context.Context, raw []byte) error {
	j.submission = bytes.Clone(raw)
	return nil
}
func (j *fixtureJournal) SaveAcknowledgment(_ context.Context, raw []byte) error {
	if j.failAck {
		return ErrDenied
	}
	j.ack = bytes.Clone(raw)
	return nil
}

func TestFirstBootOnceRetainsAmbiguityWithoutAutomaticReplay(t *testing.T) {
	for _, mode := range []string{"acknowledged", "lost_ack", "wrong_nonce", "ack_storage_failure"} {
		t.Run(mode, func(t *testing.T) {
			now := time.Now().UTC().Truncate(time.Microsecond)
			secret, report := firstBootFixture(t, now)
			collector := &fixtureCollector{report: report}
			transport := &fixtureTransport{binding: secret.Binding, wrongNonce: mode == "wrong_nonce"}
			journal := &fixtureJournal{failAck: mode == "ack_storage_failure"}
			if mode == "lost_ack" {
				transport.err = errors.New("lost acknowledgment")
			}
			result, err := RunOnce(context.Background(), secret, collector, transport, journal, func() time.Time { return now })
			if transport.calls != 1 || len(journal.submission) == 0 || result == nil || result.SSHTrustEstablished {
				t.Fatal("callback/evidence/trust boundary lost")
			}
			if (err == nil) != (mode == "acknowledged") || (result.Status == "pending_ssh") != (mode == "acknowledged") {
				t.Fatal("ambiguous callback claimed acknowledgment", err)
			}
			if _, err := RunOnce(context.Background(), secret, collector, transport, journal, func() time.Time { return now }); err == nil || transport.calls != 1 || collector.calls != 1 {
				t.Fatal("existing attempt automatically replayed")
			}
		})
	}
}
