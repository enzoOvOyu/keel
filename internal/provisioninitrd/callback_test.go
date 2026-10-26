package provisioninitrd

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/provisionboot"
	"github.com/enzoOvOyu/keel/internal/provisionpreflight"
)

// Pure protocol boundary; no real HTTP, helper, privileged runtime or DB.
type callbackSourceFixture struct {
	key                 ed25519.PublicKey
	installation        string
	boot                string
	expires             time.Time
	calls               int
	lost                bool
	changedAck          bool
	token               string
	sequence            int64
	reportSHA           string
	refreshID           string
	reviewState         string
	badNonce            bool
	reviewCalls         int
	approveAfterRefresh bool
}

func (f *callbackSourceFixture) PostCallback(ctx context.Context, operation, token string, raw []byte) (*provisionboot.BootAcknowledgment, error) {
	f.calls++
	var proof provisionboot.BootProof
	state := "booting"
	if operation == "enroll" {
		var in provisionboot.EnrollmentSubmission
		if provisionboot.DecodeCallback(raw, &in) != nil || token != f.token {
			return nil, ErrUnavailable
		}
		key, err := in.Enrollment.Key()
		payload, _ := json.Marshal(in.Enrollment)
		if err != nil || provisionboot.VerifyBootProof(key, in.Enrollment.InstallationID, in.Enrollment.BootID, 1, "enroll", payload, in.Proof) != nil {
			return nil, ErrUnavailable
		}
		f.key = key
		f.installation = in.Enrollment.InstallationID
		f.boot = in.Enrollment.BootID
		f.sequence = 1
		proof = in.Proof
	} else {
		var in provisionboot.PreflightSubmission
		if operation != "preflight" || token != "" || provisionboot.DecodeCallback(raw, &in) != nil {
			return nil, ErrUnavailable
		}
		payload, op, err := provisionboot.PreflightProofPayload(in)
		if err != nil || (f.sequence > 1 && (in.RefreshID == "" || in.RefreshID != f.refreshID)) || provisionboot.VerifyBootProof(f.key, f.installation, f.boot, f.sequence+1, op, payload, in.Proof) != nil {
			return nil, ErrUnavailable
		}
		proof = in.Proof
		state = "preflight_ready"
		f.sequence++
		f.reportSHA = checksum(in.Report)
		f.refreshID = ""
		if f.approveAfterRefresh && f.sequence > 2 {
			f.reviewState = "storage_approved"
		}
	}
	if f.lost {
		return nil, ErrUnavailable
	}
	ack := &provisionboot.BootAcknowledgment{InstallationID: proof.InstallationID, BootID: proof.BootID, Sequence: proof.Sequence, State: state, ExpiresAt: f.expires}
	if f.changedAck {
		ack.InstallationID = strings.Repeat("9", 32)
	}
	return ack, nil
}
func bootstrapFixture(t *testing.T, input Input, files *Files) *provisionboot.Bootstrap {
	t.Helper()
	var spec provisionpreflight.Spec
	if json.Unmarshal(files.Preflight, &spec) != nil {
		t.Fatal("fixture spec")
	}
	return &provisionboot.Bootstrap{Schema: provisionboot.BootstrapSchema, Token: strings.Repeat("a", 64), Binding: provisionboot.BootstrapBinding{ID: strings.Repeat("a", 32), InstallationID: strings.Repeat("b", 32), ProjectID: spec.ProjectID, GrantID: input.GrantID, Spec: spec, ExpiresAt: time.Now().Add(time.Minute)}}
}
func TestInitrdSignedEnrollmentThenExactReportOnce(t *testing.T) {
	input, files, runner, _ := prepareFixture(t)
	bootstrap := bootstrapFixture(t, input, files)
	source := &callbackSourceFixture{expires: bootstrap.Binding.Spec.ExpiresAt, token: bootstrap.Token}
	session, err := EnrollBoot(context.Background(), input, files, bootstrap, source)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	p, err := Prepare(context.Background(), input, files, runner, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	ack, err := session.Report(context.Background(), p, source)
	if err != nil || ack.State != "preflight_ready" || source.calls != 2 {
		t.Fatal("signed report failed", err)
	}
	if _, err = session.Report(context.Background(), p, source); err == nil || source.calls != 2 {
		t.Fatal("report automatically replayable")
	}
}
func TestInitrdRejectsBootstrapMismatchBeforeCallback(t *testing.T) {
	for name, change := range map[string]func(*provisionboot.Bootstrap){
		"grant":      func(b *provisionboot.Bootstrap) { b.Binding.GrantID = strings.Repeat("9", 32) },
		"project":    func(b *provisionboot.Bootstrap) { b.Binding.ProjectID = strings.Repeat("9", 32) },
		"fence":      func(b *provisionboot.Bootstrap) { b.Binding.Spec.FencingToken++ },
		"serial":     func(b *provisionboot.Bootstrap) { b.Binding.Spec.Expected.Serial = "other-server" },
		"expired":    func(b *provisionboot.Bootstrap) { b.Binding.ExpiresAt = time.Now().Add(-time.Second) },
		"bad_secret": func(b *provisionboot.Bootstrap) { b.Token = "public-grant-is-not-a-token" },
	} {
		t.Run(name, func(t *testing.T) {
			input, files, _, _ := prepareFixture(t)
			b := bootstrapFixture(t, input, files)
			change(b)
			source := &callbackSourceFixture{token: b.Token}
			if _, err := EnrollBoot(context.Background(), input, files, b, source); err == nil || source.calls != 0 {
				t.Fatal("mismatched binding reached callback")
			}
		})
	}
}
func TestInitrdLostOrChangedAcknowledgmentDoesNotRetry(t *testing.T) {
	for _, lost := range []bool{true, false} {
		input, files, _, _ := prepareFixture(t)
		b := bootstrapFixture(t, input, files)
		source := &callbackSourceFixture{expires: b.Binding.Spec.ExpiresAt, token: b.Token, lost: lost, changedAck: !lost}
		if _, err := EnrollBoot(context.Background(), input, files, b, source); err == nil || source.calls != 1 {
			t.Fatal("ambiguous enrollment retried or accepted")
		}
	}
	input, files, runner, _ := prepareFixture(t)
	b := bootstrapFixture(t, input, files)
	source := &callbackSourceFixture{expires: b.Binding.Spec.ExpiresAt, token: b.Token}
	session, err := EnrollBoot(context.Background(), input, files, b, source)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	p, err := Prepare(context.Background(), input, files, runner, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	source.lost = true
	if _, err = session.Report(context.Background(), p, source); err == nil {
		t.Fatal("lost report acknowledgment accepted")
	}
	if _, err = session.Report(context.Background(), p, source); err == nil || source.calls != 2 {
		t.Fatal("ambiguous report replayed")
	}
}

func (f *callbackSourceFixture) ReviewBoot(ctx context.Context, raw []byte) (*provisionboot.ReviewStatus, error) {
	f.reviewCalls++
	var in provisionboot.ReviewSubmission
	if provisionboot.DecodeCallback(raw, &in) != nil {
		return nil, ErrUnavailable
	}
	payload, _ := json.Marshal(in.Query)
	if ctx.Err() != nil || f.lost || provisionboot.VerifyBootProof(f.key, f.installation, f.boot, f.sequence, "review", payload, in.Proof) != nil {
		return nil, ErrUnavailable
	}
	state := f.reviewState
	if state == "" {
		state = "preflight_ready"
	}
	out := &provisionboot.ReviewStatus{InstallationID: f.installation, BootID: f.boot, Sequence: f.sequence, Nonce: in.Query.Nonce, State: state, ExpiresAt: f.expires, ReportSHA256: f.reportSHA, RefreshID: f.refreshID}
	if state != "preflight_ready" {
		out.StoragePlanSHA256 = strings.Repeat("c", 64)
	}
	if f.badNonce {
		out.Nonce = strings.Repeat("0", 32)
	}
	return out, nil
}

func enrolledReportedFixture(t *testing.T) (Input, *Files, *fixtureRunner, *BootSession, *callbackSourceFixture, *Preparation) {
	t.Helper()
	input, files, runner, _ := prepareFixture(t)
	b := bootstrapFixture(t, input, files)
	source := &callbackSourceFixture{expires: b.Binding.Spec.ExpiresAt, token: b.Token}
	session, err := EnrollBoot(context.Background(), input, files, b, source)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(session.Close)
	p, err := Prepare(context.Background(), input, files, runner, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = session.Report(context.Background(), p, source); err != nil {
		t.Fatal(err)
	}
	return input, files, runner, session, source, p
}
func TestInitrdReviewDoesNotConsumeSequenceOrCollectWithoutRequest(t *testing.T) {
	_, _, runner, session, source, _ := enrolledReportedFixture(t)
	for range 3 {
		out, err := session.Review(context.Background(), source)
		if err != nil || out.State != "preflight_ready" || session.sequence != 2 || source.sequence != 2 || runner.runs != 1 {
			t.Fatal("read-only status changed report or sequence", err)
		}
	}
}
func TestInitrdExplicitRefreshUsesSameBootAndConsumesRequestOnce(t *testing.T) {
	_, _, runner, session, source, p := enrolledReportedFixture(t)
	source.refreshID = strings.Repeat("d", 32)
	status, err := session.Review(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	// Calling Report again without the controller request transcript is denied.
	if _, err = session.Report(context.Background(), p, source); err == nil {
		t.Fatal("ordinary report repeated")
	}
	if _, err = session.report(context.Background(), p, source, strings.Repeat("e", 32)); err == nil {
		t.Fatal("changed refresh identity accepted")
	}
	ack, err := session.report(context.Background(), p, source, status.RefreshID)
	if err != nil || ack.Sequence != 3 || source.boot != session.boot || source.refreshID != "" || runner.runs != 1 {
		t.Fatal("refresh binding failed", err)
	}
	if _, err = session.report(context.Background(), p, source, status.RefreshID); err == nil {
		t.Fatal("request consumed twice")
	}
	source.reviewState = "storage_approved"
	out, err := session.AwaitStorageReview(context.Background(), Input{}, nil, p, runner, source, func(*Preparation, int64) error { t.Fatal("approval unexpectedly collected facts"); return nil })
	if err != nil || out.Status != "storage_approved_no_write_permit" || out.InstallationPerformed || runner.runs != 1 {
		t.Fatal("approval became installation or recollection", err)
	}
}
func TestInitrdReviewRejectsReplayedResponseAndLostRefreshAck(t *testing.T) {
	t.Run("nonce_mismatch", func(t *testing.T) {
		_, _, _, session, source, _ := enrolledReportedFixture(t)
		source.badNonce = true
		if _, err := session.Review(context.Background(), source); err == nil || !session.closed {
			t.Fatal("old nonce accepted")
		}
	})
	t.Run("lost_ack", func(t *testing.T) {
		_, _, _, session, source, p := enrolledReportedFixture(t)
		source.refreshID = strings.Repeat("d", 32)
		status, err := session.Review(context.Background(), source)
		if err != nil {
			t.Fatal(err)
		}
		source.lost = true
		if _, err = session.report(context.Background(), p, source, status.RefreshID); err == nil || !session.closed {
			t.Fatal("ambiguous refresh accepted")
		}
		calls := source.calls
		if _, err = session.report(context.Background(), p, source, status.RefreshID); err == nil || source.calls != calls {
			t.Fatal("ambiguous refresh resent")
		}
	})
}

func TestInitrdRequestedRefreshRecollectsAndRetainsBeforeApproval(t *testing.T) {
	input, files, runner, session, source, p := enrolledReportedFixture(t)
	source.refreshID = strings.Repeat("d", 32)
	source.approveAfterRefresh = true
	retained := 0
	out, err := session.AwaitStorageReview(context.Background(), input, files, p, runner, source, func(p *Preparation, sequence int64) error {
		retained++
		if sequence != 3 || p.BootCallbackStatus != "acknowledged" || p.ReportSHA256 != checksum(p.ReportBytes) {
			t.Fatal("exact refresh evidence not retained")
		}
		return nil
	})
	if err != nil || out.Status != "storage_approved_no_write_permit" || out.InstallationPerformed || runner.runs != 2 || retained != 1 || source.calls != 3 || source.reviewCalls != 2 {
		t.Fatal("explicit refresh did not complete read-only review", err)
	}
}
