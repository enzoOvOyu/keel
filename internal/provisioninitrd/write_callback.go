package provisioninitrd

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"

	"github.com/enzoOvOyu/keel/internal/provisionboot"
	"github.com/enzoOvOyu/keel/internal/provisionstorage"
)

type WriteSource interface {
	IssueWritePermit(context.Context, []byte) (*provisionboot.WritePermit, error)
	CheckWritePermit(context.Context, []byte) (*provisionboot.WriteAuthorization, error)
	ReportWriteReceipt(context.Context, []byte) (*provisionboot.BootAcknowledgment, error)
}

// WriteSession owns one consumed permit and one terminal report opportunity.
// A failed check stops further work but preserves the key solely for reporting
// unknown. No lost mutation response is retried or treated as absence of writes.
type WriteSession struct {
	mu       sync.Mutex
	boot     *BootSession
	source   WriteSource
	permit   provisionboot.WritePermit
	stopped  bool
	reported bool
}

func randomNonce() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", ErrUnavailable
	}
	return hex.EncodeToString(raw[:]), nil
}

func (s *BootSession) ApprovedStoragePlan() (*provisionstorage.Plan, error) {
	if s == nil || s.closed || s.approvedStorage == nil || !digest.MatchString(s.runtimeInputSHA) {
		return nil, ErrUnavailable
	}
	return provisionstorage.Decode(mustJSON(s.approvedStorage), time.Now())
}

// BeginWrite requires the privately retained authenticated approval and a new
// local report. It sends once, after all immutable content has been staged.
// The authority independently recomputes the report/GPT and current admission.
func (s *BootSession) BeginWrite(ctx context.Context, preparation provisionboot.WritePreparation, report []byte, source WriteSource) (*WriteSession, error) {
	plan, err := s.ApprovedStoragePlan()
	if err != nil || source == nil || ctx.Err() != nil || s.sequence < 2 || s.sequence >= 4095 || preparation.StoragePlanSHA256 != plan.SHA256 || preparation.RuntimeInputSHA256 != s.runtimeInputSHA || provisionstorage.VerifyCurrent(*plan, report, time.Now()) != nil {
		return nil, ErrUnavailable
	}
	gpt, err := provisionstorage.PrepareGPT(*plan, preparation.GPTIdentity, time.Now())
	if err != nil || gpt.SHA256() != preparation.GPTSHA256 {
		return nil, ErrUnavailable
	}
	if preparation.Nonce == "" {
		preparation.Nonce, err = randomNonce()
		if err != nil {
			return nil, err
		}
	} else if !identifier.MatchString(preparation.Nonce) {
		return nil, ErrUnavailable
	}
	in := provisionboot.WritePermitSubmission{Preparation: preparation, Report: append([]byte(nil), report...)}
	payload, err := provisionboot.WritePermitPayload(in)
	if err != nil {
		return nil, ErrUnavailable
	}
	in.Proof, err = provisionboot.SignBootProof(s.key, s.installation, s.boot, s.sequence+1, "write-permit", payload)
	if err != nil {
		return nil, ErrUnavailable
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return nil, ErrUnavailable
	}
	// The private approval can never issue a second permit, even if IO fails.
	s.approvedStorage = nil
	s.sequence++
	permit, err := source.IssueWritePermit(ctx, raw)
	if err != nil || ctx.Err() != nil || provisionboot.ValidateWritePermit(permit, in.Proof, preparation, time.Now()) != nil || permit.ExpiresAt.After(s.expires) {
		s.Close()
		return nil, ErrUnavailable
	}
	return &WriteSession{boot: s, source: source, permit: *permit}, nil
}

func (s *WriteSession) Permit() provisionboot.WritePermit {
	if s == nil {
		return provisionboot.WritePermit{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.permit
}

func (s *WriteSession) Check(ctx context.Context, stage string) (*provisionboot.WriteAuthorization, error) {
	if s == nil {
		return nil, ErrUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped || s.reported || s.boot.closed || !provisionboot.ValidWriteStage(stage) || ctx.Err() != nil {
		return nil, ErrUnavailable
	}
	nonce, err := randomNonce()
	if err != nil {
		s.stopped = true
		return nil, err
	}
	query := provisionboot.WriteCheckQuery{PermitID: s.permit.ID, Nonce: nonce, Stage: stage}
	payload, _ := json.Marshal(query)
	proof, err := provisionboot.SignBootProof(s.boot.key, s.boot.installation, s.boot.boot, s.boot.sequence, "write-check", payload)
	if err != nil {
		s.stopped = true
		return nil, ErrUnavailable
	}
	raw, _ := json.Marshal(provisionboot.WriteCheckSubmission{Query: query, Proof: proof})
	authorization, err := s.source.CheckWritePermit(ctx, raw)
	if err != nil || ctx.Err() != nil || provisionboot.ValidateWriteAuthorization(authorization, &s.permit, query, time.Now()) != nil {
		s.stopped = true
		return nil, ErrUnavailable
	}
	copy := *authorization
	return &copy, nil
}

func (s *WriteSession) Report(ctx context.Context, receipt provisionboot.WriteReceipt) (*provisionboot.BootAcknowledgment, error) {
	if s == nil {
		return nil, ErrUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.reported || s.boot.closed || (s.stopped && receipt.Outcome != "unknown") || provisionboot.ValidateWriteReceipt(receipt, &s.permit) != nil {
		return nil, ErrUnavailable
	}
	s.reported = true
	s.stopped = true
	defer s.boot.Close()
	payload, err := json.Marshal(receipt)
	if err != nil {
		return nil, ErrUnavailable
	}
	proof, err := provisionboot.SignBootProof(s.boot.key, s.boot.installation, s.boot.boot, s.boot.sequence+1, "install-stopped", payload)
	if err != nil {
		return nil, ErrUnavailable
	}
	raw, _ := json.Marshal(provisionboot.WriteReceiptSubmission{Receipt: receipt, Proof: proof})
	s.boot.sequence++
	ack, err := s.source.ReportWriteReceipt(ctx, raw)
	want := "unknown"
	if receipt.Outcome == "written" {
		want = "awaiting_first_boot"
	}
	if err != nil || ctx.Err() != nil || !validAcknowledgment(ack, proof, s.boot.expires) || ack.State != want {
		return nil, ErrUnavailable
	}
	return ack, nil
}
