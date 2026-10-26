package provisionfirstboot

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"time"
)

type Collector interface {
	Collect(context.Context, Binding) (Report, error)
}
type Transport interface {
	Submit(context.Context, Submission) (*Acknowledgment, error)
}

// Journal durably reserves a single local attempt before observation or IO.
// Existing intent, including an interrupted attempt, must never be overwritten
// or interpreted as permission to automatically retry the callback.
type Journal interface {
	Begin(context.Context, string) error
	SaveSubmission(context.Context, []byte) error
	SaveAcknowledgment(context.Context, []byte) error
}

type Result struct {
	InstallationID      string `json:"installation_id"`
	Status              string `json:"status"`
	SSHTrustEstablished bool   `json:"ssh_trust_established"`
}

func RunOnce(ctx context.Context, secret *Secret, collector Collector, transport Transport, journal Journal, clock func() time.Time) (*Result, error) {
	if secret == nil || collector == nil || transport == nil || journal == nil || ctx.Err() != nil {
		return nil, ErrDenied
	}
	if clock == nil {
		clock = time.Now
	}
	// Freeze caller-owned nested plan expectations before any asynchronous IO.
	raw, err := json.Marshal(secret)
	if err != nil {
		return nil, ErrDenied
	}
	frozen, err := DecodeSecret(raw, clock())
	clear(raw)
	if err != nil {
		return nil, ErrDenied
	}
	defer func() { frozen.PrivateKey = "" }()
	binding := frozen.Binding
	out := &Result{InstallationID: binding.InstallationID, Status: "unknown", SSHTrustEstablished: false}
	if journal.Begin(ctx, binding.SHA256()) != nil {
		return out, ErrDenied
	}
	report, err := collector.Collect(ctx, binding)
	if err != nil || ctx.Err() != nil {
		return out, ErrDenied
	}
	var nonce [16]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return out, ErrDenied
	}
	report.Schema, report.InstallationID, report.BootID, report.GrantID = ReportSchema, binding.InstallationID, binding.BootID, binding.GrantID
	report.Sequence, report.Nonce, report.BindingSHA256 = 1, hex.EncodeToString(nonce[:]), binding.SHA256()
	in, err := SignObserved(frozen, report, clock())
	if err != nil {
		return out, ErrDenied
	}
	raw, err = json.Marshal(in)
	if err != nil || journal.SaveSubmission(ctx, raw) != nil || ctx.Err() != nil {
		return out, ErrDenied
	}
	// This POST occurs once. An absent/mismatched response or local ACK-write
	// failure is ambiguous and must be reconciled rather than retried on boot.
	ack, err := transport.Submit(ctx, *in)
	if err != nil || ctx.Err() != nil || ValidateAcknowledgment(ack, binding, report, clock()) != nil {
		return out, ErrDenied
	}
	raw, err = json.Marshal(ack)
	if err != nil || journal.SaveAcknowledgment(ctx, raw) != nil {
		return out, ErrDenied
	}
	out.Status = "pending_ssh"
	return out, nil
}
