package provisioninitrd

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/enzoOvOyu/keel/internal/provisionboot"
	"github.com/enzoOvOyu/keel/internal/provisionpreflight"
)

type CallbackSource interface {
	PostCallback(context.Context, string, string, []byte) (*provisionboot.BootAcknowledgment, error)
}
type BootSession struct {
	key            ed25519.PrivateKey
	installation   string
	boot           string
	sequence       int64
	expires        time.Time
	spec           provisionpreflight.Spec
	manifestSHA256 string
	closed         bool
}

// EnrollBoot accepts only an independently delivered protected bootstrap whose
// whole frozen Spec matches the verified boot files. It never obtains a token
// from public resources or derives identity from the observed MAC alone.
func EnrollBoot(ctx context.Context, input Input, files *Files, bootstrap *provisionboot.Bootstrap, source CallbackSource) (*BootSession, error) {
	if bootstrap == nil || files == nil || source == nil || ctx.Err() != nil {
		return nil, ErrUnavailable
	}
	now := time.Now()
	manifest, spec, err := provisionboot.VerifyBootFiles(files.Manifest, input.ManifestSHA256, files.Preflight, files.Seed, now)
	if err != nil || manifest.GrantID != input.GrantID || manifest.MAC != input.BootMAC {
		return nil, ErrUnavailable
	}
	b := bootstrap.Binding
	want, _ := json.Marshal(spec)
	given, _ := json.Marshal(b.Spec)
	if bootstrap.Schema != provisionboot.BootstrapSchema || !digest.MatchString(bootstrap.Token) || !identifier.MatchString(b.ID) || !identifier.MatchString(b.InstallationID) || b.ProjectID != spec.ProjectID || b.GrantID != input.GrantID || !bytes.Equal(want, given) || !b.ExpiresAt.After(now) || b.ExpiresAt.After(spec.ExpiresAt) {
		return nil, ErrUnavailable
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, ErrUnavailable
	}
	var nonce [16]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		clear(private)
		return nil, ErrUnavailable
	}
	boot := hex.EncodeToString(nonce[:])
	enrollment := provisionboot.BootEnrollment{InstallationID: b.InstallationID, BootID: boot, PublicKey: base64.StdEncoding.EncodeToString(public)}
	payload, err := json.Marshal(enrollment)
	if err != nil {
		clear(private)
		return nil, ErrUnavailable
	}
	proof, err := provisionboot.SignBootProof(private, b.InstallationID, boot, 1, "enroll", payload)
	if err != nil {
		clear(private)
		return nil, ErrUnavailable
	}
	raw, err := json.Marshal(provisionboot.EnrollmentSubmission{Enrollment: enrollment, Proof: proof})
	if err != nil {
		clear(private)
		return nil, ErrUnavailable
	}
	ack, err := source.PostCallback(ctx, "enroll", bootstrap.Token, raw)
	// A lost acknowledgment is ambiguous. Never retry with the same token or
	// automatically create another boot identity after it may be consumed.
	if err != nil || ctx.Err() != nil || !validAcknowledgment(ack, proof, spec.ExpiresAt) || ack.State != "booting" {
		clear(private)
		return nil, ErrUnavailable
	}
	return &BootSession{key: private, installation: b.InstallationID, boot: boot, sequence: 1, expires: spec.ExpiresAt, spec: *spec, manifestSHA256: input.ManifestSHA256}, nil
}
func validAcknowledgment(ack *provisionboot.BootAcknowledgment, p provisionboot.BootProof, expires time.Time) bool {
	return ack != nil && ack.InstallationID == p.InstallationID && ack.BootID == p.BootID && ack.Sequence == p.Sequence && ack.ExpiresAt.Equal(expires) && ack.ExpiresAt.After(time.Now())
}
func (s *BootSession) Close() {
	if s != nil {
		clear(s.key)
		s.closed = true
	}
}

// Report sends exact locally verified helper bytes once. This phase ends at
// storage review or preflight failure; it has no write-permit operation.
func (s *BootSession) Report(ctx context.Context, p *Preparation, source CallbackSource) (*provisionboot.BootAcknowledgment, error) {
	if s == nil || s.closed || s.sequence != 1 || p == nil || source == nil || p.InstallationPerformed || p.Schema != PreparationSchema || p.ManifestSHA256 != s.manifestSHA256 || !s.expires.After(time.Now()) {
		return nil, ErrUnavailable
	}
	report, err := provisionpreflight.EvaluateReport(s.spec, p.ReportBytes, time.Now())
	sum := sha256.Sum256(p.ReportBytes)
	if err != nil || report == nil || hex.EncodeToString(sum[:]) != p.ReportSHA256 {
		return nil, ErrUnavailable
	}
	proof, err := provisionboot.SignBootProof(s.key, s.installation, s.boot, 2, "preflight", p.ReportBytes)
	if err != nil {
		return nil, ErrUnavailable
	}
	raw, err := json.Marshal(provisionboot.PreflightSubmission{Report: p.ReportBytes, Proof: proof})
	if err != nil {
		return nil, ErrUnavailable
	}
	// Consume the local send opportunity before IO; loss cannot cause replay.
	s.sequence = 2
	ack, err := source.PostCallback(ctx, "preflight", "", raw)
	if err != nil || ctx.Err() != nil || !validAcknowledgment(ack, proof, s.expires) || (report.Status == "PASS" && ack.State != "preflight_ready") || (report.Status != "PASS" && ack.State != "failed") {
		s.Close()
		return nil, ErrUnavailable
	}
	return ack, nil
}
