//go:build linux

package provisioninitrd

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/provisionboot"
	"github.com/enzoOvOyu/keel/internal/provisionpreflight"
	"github.com/enzoOvOyu/keel/internal/provisionstorage"
)

type writeSourceFixture struct {
	key                              ed25519.PublicKey
	sequence                         int64
	issued, checked, reported        int
	permit                           *provisionboot.WritePermit
	expires                          time.Time
	lostIssue, badNonce, lostReceipt bool
}

func (f *writeSourceFixture) IssueWritePermit(ctx context.Context, raw []byte) (*provisionboot.WritePermit, error) {
	f.issued++
	var in provisionboot.WritePermitSubmission
	if provisionboot.DecodeCallback(raw, &in) != nil {
		return nil, ErrUnavailable
	}
	payload, err := provisionboot.WritePermitPayload(in)
	if err != nil || ctx.Err() != nil || provisionboot.VerifyBootProof(f.key, in.Proof.InstallationID, in.Proof.BootID, f.sequence+1, "write-permit", payload, in.Proof) != nil {
		return nil, ErrUnavailable
	}
	f.sequence++
	p := in.Preparation
	f.permit = &provisionboot.WritePermit{Schema: provisionboot.WritePermitSchema, ID: strings.Repeat("e", 32), InstallationID: in.Proof.InstallationID, BootID: in.Proof.BootID, Sequence: f.sequence, Nonce: p.Nonce, StoragePlanSHA256: p.StoragePlanSHA256, GPTSHA256: p.GPTSHA256, RootFSSHA256: p.RootFSSHA256, TargetUKISHA256: p.TargetUKISHA256, RuntimeInputSHA256: p.RuntimeInputSHA256, IssuedAt: time.Now(), ExpiresAt: f.expires}
	if f.lostIssue {
		return nil, ErrUnavailable
	}
	return f.permit, nil
}

func (f *writeSourceFixture) CheckWritePermit(ctx context.Context, raw []byte) (*provisionboot.WriteAuthorization, error) {
	f.checked++
	var in provisionboot.WriteCheckSubmission
	if provisionboot.DecodeCallback(raw, &in) != nil {
		return nil, ErrUnavailable
	}
	payload, _ := json.Marshal(in.Query)
	if ctx.Err() != nil || provisionboot.VerifyBootProof(f.key, f.permit.InstallationID, f.permit.BootID, f.sequence, "write-check", payload, in.Proof) != nil {
		return nil, ErrUnavailable
	}
	now := time.Now()
	out := &provisionboot.WriteAuthorization{PermitID: f.permit.ID, InstallationID: f.permit.InstallationID, BootID: f.permit.BootID, Sequence: f.sequence, Nonce: in.Query.Nonce, Stage: in.Query.Stage, StoragePlanSHA256: f.permit.StoragePlanSHA256, GPTSHA256: f.permit.GPTSHA256, CheckedAt: now, ValidUntil: now.Add(time.Second)}
	if f.badNonce {
		out.Nonce = strings.Repeat("0", 32)
	}
	return out, nil
}

func (f *writeSourceFixture) ReportWriteReceipt(ctx context.Context, raw []byte) (*provisionboot.BootAcknowledgment, error) {
	f.reported++
	var in provisionboot.WriteReceiptSubmission
	if provisionboot.DecodeCallback(raw, &in) != nil {
		return nil, ErrUnavailable
	}
	payload, _ := json.Marshal(in.Receipt)
	if ctx.Err() != nil || provisionboot.VerifyBootProof(f.key, f.permit.InstallationID, f.permit.BootID, f.sequence+1, "install-stopped", payload, in.Proof) != nil || provisionboot.ValidateWriteReceipt(in.Receipt, f.permit) != nil {
		return nil, ErrUnavailable
	}
	f.sequence++
	if f.lostReceipt {
		return nil, ErrUnavailable
	}
	state := "unknown"
	if in.Receipt.Outcome == "written" {
		state = "awaiting_first_boot"
	}
	return &provisionboot.BootAcknowledgment{InstallationID: f.permit.InstallationID, BootID: f.permit.BootID, Sequence: f.sequence, State: state, ExpiresAt: f.expires}, nil
}

func writableSessionFixture(t *testing.T) (*BootSession, provisionboot.WritePreparation, []byte, *writeSourceFixture) {
	t.Helper()
	_, files, runner, _ := prepareFixture(t)
	var spec provisionpreflight.Spec
	var prior provisionpreflight.Report
	json.Unmarshal(files.Preflight, &spec)
	json.Unmarshal(runner.report, &prior)
	no, yes := false, true
	intent := provisionstorage.Intent{Schema: provisionstorage.IntentSchema, Layout: provisionstorage.Layout, WWID: "naa.fixture-1", Serial: "fixture-disk-1", SizeBytes: 32 << 30, LogicalBlockBytes: 512, EraseSelectedDisk: true}
	prior.Observation.Disks[0] = provisionpreflight.Disk{Device: "/dev/sda", DeviceNumber: "8:0", DiskSequence: 71, WWID: intent.WWID, Serial: intent.Serial, SizeBytes: intent.SizeBytes, LogicalBlockBytes: 512, Healthy: &yes, ReadOnly: &no, Removable: &no, InUse: &no}
	report, err := provisionpreflight.Evaluate(spec, prior.Observation, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(report)
	plan, err := provisionstorage.Build(spec, raw, intent, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	ids := provisionstorage.GPTIdentity{Disk: "11111111-1111-4111-8111-111111111111", EFI: "22222222-2222-4222-8222-222222222222", Root: "33333333-3333-4333-8333-333333333333"}
	gpt, err := provisionstorage.PrepareGPT(*plan, ids, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	s := &BootSession{key: private, installation: strings.Repeat("a", 32), boot: strings.Repeat("b", 32), sequence: 2, expires: spec.ExpiresAt, spec: spec, approvedStorage: plan, runtimeInputSHA: strings.Repeat("c", 64), lastReportSHA256: checksum(raw)}
	t.Cleanup(s.Close)
	preparation := provisionboot.WritePreparation{StoragePlanSHA256: plan.SHA256, GPTIdentity: ids, GPTSHA256: gpt.SHA256(), RootFSSHA256: strings.Repeat("d", 64), TargetUKISHA256: strings.Repeat("e", 64), RuntimeInputSHA256: s.runtimeInputSHA}
	return s, preparation, raw, &writeSourceFixture{key: public, sequence: 2, expires: spec.ExpiresAt}
}

func TestInitrdWritePermitAndReceiptAreSingleSend(t *testing.T) {
	s, p, report, source := writableSessionFixture(t)
	w, err := s.BeginWrite(context.Background(), p, report, source)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := w.Check(context.Background(), "gpt.write"); err != nil {
			t.Fatal(err)
		}
	}
	if source.sequence != 3 || s.sequence != 3 {
		t.Fatal("readonly check consumed sequence")
	}
	if _, err := s.BeginWrite(context.Background(), p, report, source); err == nil || source.issued != 1 {
		t.Fatal("permit could be reissued")
	}
	permit := w.Permit()
	receipt := provisionboot.WriteReceipt{PermitID: permit.ID, StoragePlanSHA256: permit.StoragePlanSHA256, GPTSHA256: permit.GPTSHA256, RootFSSHA256: permit.RootFSSHA256, TargetUKISHA256: permit.TargetUKISHA256, Outcome: "written", ReadbackSHA256: strings.Repeat("f", 64), Reason: "fixture_readback_only"}
	ack, err := w.Report(context.Background(), receipt)
	if err != nil || ack.State != "awaiting_first_boot" || !s.closed {
		t.Fatal("write receipt mistaken for completed installation", err)
	}
	if _, err := w.Report(context.Background(), receipt); err == nil || source.reported != 1 {
		t.Fatal("receipt replayed")
	}
}

func TestInitrdWriteLossAndInvalidChecksNeverAllowRetry(t *testing.T) {
	s, p, report, source := writableSessionFixture(t)
	source.lostIssue = true
	if _, err := s.BeginWrite(context.Background(), p, report, source); err == nil || !s.closed {
		t.Fatal("lost issue ACK did not close session")
	}
	if _, err := s.BeginWrite(context.Background(), p, report, source); err == nil || source.issued != 1 {
		t.Fatal("lost mutation was retried")
	}
	s, p, report, source = writableSessionFixture(t)
	w, err := s.BeginWrite(context.Background(), p, report, source)
	if err != nil {
		t.Fatal(err)
	}
	source.badNonce = true
	if _, err := w.Check(context.Background(), "gpt.write"); err == nil {
		t.Fatal("wrong nonce accepted")
	}
	if _, err := w.Check(context.Background(), "root.format"); err == nil || source.checked != 1 {
		t.Fatal("denied writer resumed")
	}
	permit := w.Permit()
	receipt := provisionboot.WriteReceipt{PermitID: permit.ID, StoragePlanSHA256: permit.StoragePlanSHA256, GPTSHA256: permit.GPTSHA256, RootFSSHA256: permit.RootFSSHA256, TargetUKISHA256: permit.TargetUKISHA256, Outcome: "unknown", Reason: "authorization_lost"}
	source.lostReceipt = true
	if _, err := w.Report(context.Background(), receipt); err == nil || source.reported != 1 {
		t.Fatal("lost receipt fabricated ACK")
	}
	if _, err := w.Report(context.Background(), receipt); err == nil || source.reported != 1 {
		t.Fatal("lost receipt retried")
	}
}

func TestInitrdWriteCannotUseUnapprovedOrStaleFacts(t *testing.T) {
	for _, which := range []string{"approval", "runtime", "report", "gpt"} {
		t.Run(which, func(t *testing.T) {
			s, p, report, source := writableSessionFixture(t)
			switch which {
			case "approval":
				s.approvedStorage = nil
			case "runtime":
				p.RuntimeInputSHA256 = strings.Repeat("f", 64)
			case "report":
				report = []byte(`{"status":"PASS"}`)
			case "gpt":
				p.GPTSHA256 = strings.Repeat("f", 64)
			}
			if _, err := s.BeginWrite(context.Background(), p, report, source); err == nil || source.issued != 0 {
				t.Fatal("invalid preconditions reached permit mutation")
			}
		})
	}
}
