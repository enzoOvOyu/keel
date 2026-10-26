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
	key          ed25519.PublicKey
	installation string
	boot         string
	expires      time.Time
	calls        int
	lost         bool
	changedAck   bool
	token        string
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
		proof = in.Proof
	} else {
		var in provisionboot.PreflightSubmission
		if operation != "preflight" || token != "" || provisionboot.DecodeCallback(raw, &in) != nil || provisionboot.VerifyBootProof(f.key, f.installation, f.boot, 2, "preflight", in.Report, in.Proof) != nil {
			return nil, ErrUnavailable
		}
		proof = in.Proof
		state = "preflight_ready"
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
