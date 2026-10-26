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
	"github.com/enzoOvOyu/keel/internal/provisionstorage"
)

type CallbackSource interface {
	PostCallback(context.Context, string, string, []byte) (*provisionboot.BootAcknowledgment, error)
}
type BootSession struct {
	key              ed25519.PrivateKey
	installation     string
	boot             string
	sequence         int64
	expires          time.Time
	spec             provisionpreflight.Spec
	manifestSHA256   string
	closed           bool
	lastReportSHA256 string
	refreshID        string
	approvedStorage  *provisionstorage.Plan
	runtimeInputSHA  string
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
	return s.report(ctx, p, source, "")
}
func (s *BootSession) report(ctx context.Context, p *Preparation, source CallbackSource, refreshID string) (*provisionboot.BootAcknowledgment, error) {
	if s == nil || s.closed || (refreshID == "" && s.sequence != 1) || (refreshID != "" && (s.refreshID != refreshID || s.sequence < 2 || s.sequence > 32)) || p == nil || source == nil || p.InstallationPerformed || p.Schema != PreparationSchema || p.ManifestSHA256 != s.manifestSHA256 || !s.expires.After(time.Now()) {
		return nil, ErrUnavailable
	}
	report, err := provisionpreflight.EvaluateReport(s.spec, p.ReportBytes, time.Now())
	sum := sha256.Sum256(p.ReportBytes)
	if err != nil || report == nil || hex.EncodeToString(sum[:]) != p.ReportSHA256 {
		return nil, ErrUnavailable
	}
	in := provisionboot.PreflightSubmission{Report: p.ReportBytes, RefreshID: refreshID}
	payload, operation, err := provisionboot.PreflightProofPayload(in)
	if err != nil {
		return nil, ErrUnavailable
	}
	proof, err := provisionboot.SignBootProof(s.key, s.installation, s.boot, s.sequence+1, operation, payload)
	if err != nil {
		return nil, ErrUnavailable
	}
	in.Proof = proof
	raw, err := json.Marshal(in)
	if err != nil {
		return nil, ErrUnavailable
	}
	// Consume the local send opportunity before IO; loss cannot cause replay.
	s.sequence++
	s.refreshID = ""
	ack, err := source.PostCallback(ctx, "preflight", "", raw)
	if err != nil || ctx.Err() != nil || !validAcknowledgment(ack, proof, s.expires) || (report.Status == "PASS" && ack.State != "preflight_ready") || (report.Status != "PASS" && ack.State != "failed") {
		s.Close()
		return nil, ErrUnavailable
	}
	s.lastReportSHA256 = p.ReportSHA256
	return ack, nil
}

type ReviewSource interface {
	CallbackSource
	ReviewBoot(context.Context, []byte) (*provisionboot.ReviewStatus, error)
}

func (s *BootSession) Review(ctx context.Context, source ReviewSource) (*provisionboot.ReviewStatus, error) {
	if s == nil || s.closed || s.sequence < 2 || s.lastReportSHA256 == "" || source == nil || !s.expires.After(time.Now()) || ctx.Err() != nil {
		return nil, ErrUnavailable
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, ErrUnavailable
	}
	query := provisionboot.ReviewQuery{Nonce: hex.EncodeToString(nonce[:])}
	payload, _ := json.Marshal(query)
	proof, err := provisionboot.SignBootProof(s.key, s.installation, s.boot, s.sequence, "review", payload)
	if err != nil {
		return nil, ErrUnavailable
	}
	raw, _ := json.Marshal(provisionboot.ReviewSubmission{Query: query, Proof: proof})
	status, err := source.ReviewBoot(ctx, raw)
	if err != nil || ctx.Err() != nil || provisionboot.ValidateReviewStatus(status, proof, query.Nonce, time.Now()) != nil || !status.ExpiresAt.Equal(s.expires) || status.ReportSHA256 != s.lastReportSHA256 {
		s.Close()
		return nil, ErrUnavailable
	}
	s.refreshID = status.RefreshID
	s.runtimeInputSHA = status.RuntimeInputSHA256
	if status.State == "storage_approved" && status.StoragePlan != nil {
		// Keep a private snapshot: neither caller mutation nor a later public
		// status object can alter the reviewed plan used by the writer.
		raw, marshalErr := json.Marshal(status.StoragePlan)
		plan, decodeErr := provisionstorage.Decode(raw, time.Now())
		want, _ := json.Marshal(s.spec)
		if marshalErr != nil || decodeErr != nil || !bytes.Equal(want, mustJSON(plan.Spec)) || plan.SHA256 != status.StoragePlanSHA256 {
			s.Close()
			return nil, ErrUnavailable
		}
		s.approvedStorage = plan
	}
	return status, nil
}

func mustJSON(v any) []byte { raw, _ := json.Marshal(v); return raw }

// AwaitStorageReview keeps the same boot identity and bounded network lease.
// Only an explicit controller request triggers another read-only collection.
// Approval ends this phase; it never enters a writer or starts another lease.
func (s *BootSession) AwaitStorageReview(ctx context.Context, input Input, files *Files, p *Preparation, runner HelperRunner, source ReviewSource, retain func(*Preparation, int64) error) (*Preparation, error) {
	if s == nil || p == nil || p.Report == nil || p.Report.Status != "PASS" || runner == nil || retain == nil {
		return p, ErrUnavailable
	}
	ctx, cancel := context.WithDeadline(ctx, s.expires)
	defer cancel()
	for {
		status, err := s.Review(ctx, source)
		if err != nil {
			return p, err
		}
		p.BootReview = status
		if status.State == "storage_approved" {
			p.Status = "storage_approved_no_write_permit"
			return p, nil
		}
		if status.RefreshID != "" {
			next, err := Prepare(ctx, input, files, runner, time.Now)
			if err != nil {
				s.Close()
				return p, ErrUnavailable
			}
			next.BootCallbackStatus = "unknown"
			next.BootAcknowledgment, err = s.report(ctx, next, source, status.RefreshID)
			if err == nil {
				next.BootCallbackStatus = "acknowledged"
			}
			p = next
			if retain(p, s.sequence) != nil {
				s.Close()
				return p, ErrUnavailable
			}
			if err != nil {
				return p, err
			}
			if p.Report.Status != "PASS" {
				return p, nil
			}
			continue
		}
		timer := time.NewTimer(3 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			s.Close()
			return p, ErrUnavailable
		case <-timer.C:
		}
	}
}
