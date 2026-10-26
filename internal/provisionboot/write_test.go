package provisionboot

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/provisionstorage"
)

func writePreparationFixture() WritePreparation {
	return WritePreparation{Nonce: strings.Repeat("1", 32), StoragePlanSHA256: strings.Repeat("a", 64), GPTIdentity: provisionstorage.GPTIdentity{Disk: "00000000-0000-4000-8000-000000000001", EFI: "00000000-0000-4000-8000-000000000002", Root: "00000000-0000-4000-8000-000000000003"}, GPTSHA256: strings.Repeat("b", 64), RootFSSHA256: strings.Repeat("c", 64), TargetUKISHA256: strings.Repeat("d", 64), RuntimeInputSHA256: strings.Repeat("e", 64)}
}

func TestWritePermitProofBindsFreshExactFactsAndPreparedBytes(t *testing.T) {
	enrolled, key := enrollmentFixture(t)
	in := WritePermitSubmission{Preparation: writePreparationFixture(), Report: []byte("exact new report\n")}
	payload, err := WritePermitPayload(in)
	if err != nil {
		t.Fatal(err)
	}
	proof, err := SignBootProof(key, enrolled.Proof.InstallationID, enrolled.Proof.BootID, 3, "write-permit", payload)
	if err != nil {
		t.Fatal(err)
	}
	public := key.Public().(ed25519.PublicKey)
	if VerifyBootProof(public, proof.InstallationID, proof.BootID, 3, "write-permit", payload, proof) != nil {
		t.Fatal("original preparation denied")
	}
	for name, change := range map[string]func(*WritePermitSubmission){
		"nonce":      func(x *WritePermitSubmission) { x.Preparation.Nonce = strings.Repeat("2", 32) },
		"disk_guid":  func(x *WritePermitSubmission) { x.Preparation.GPTIdentity.Disk = x.Preparation.GPTIdentity.Root },
		"gpt_bytes":  func(x *WritePermitSubmission) { x.Preparation.GPTSHA256 = strings.Repeat("f", 64) },
		"raw_report": func(x *WritePermitSubmission) { x.Report = []byte("exact new report") },
		"rootfs":     func(x *WritePermitSubmission) { x.Preparation.RootFSSHA256 = strings.Repeat("f", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			copy := in
			change(&copy)
			changed, err := WritePermitPayload(copy)
			if err == nil && VerifyBootProof(public, proof.InstallationID, proof.BootID, 3, "write-permit", changed, proof) == nil {
				t.Fatal("changed preparation retained signature")
			}
		})
	}
	if VerifyBootProof(public, proof.InstallationID, proof.BootID, 3, "review", payload, proof) == nil {
		t.Fatal("review signature substituted for permit")
	}
	large := in
	large.Report = make([]byte, 2<<20)
	bounded, err := WritePermitPayload(large)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = SignBootProof(key, proof.InstallationID, proof.BootID, 3, "write-permit", bounded); err != nil {
		t.Fatal("maximum exact report lost envelope allowance")
	}
}

func writePermitFixture(now time.Time) *WritePermit {
	p := writePreparationFixture()
	return &WritePermit{Schema: WritePermitSchema, ID: strings.Repeat("9", 32), InstallationID: strings.Repeat("4", 32), BootID: strings.Repeat("5", 32), Sequence: 3, Nonce: p.Nonce, StoragePlanSHA256: p.StoragePlanSHA256, GPTSHA256: p.GPTSHA256, RootFSSHA256: p.RootFSSHA256, TargetUKISHA256: p.TargetUKISHA256, RuntimeInputSHA256: p.RuntimeInputSHA256, IssuedAt: now, ExpiresAt: now.Add(time.Minute)}
}

func TestWriteAuthorizationRejectsStaleNonceStageAndLeaseWindow(t *testing.T) {
	now := time.Now().UTC()
	p := writePermitFixture(now)
	q := WriteCheckQuery{PermitID: p.ID, Nonce: strings.Repeat("2", 32), Stage: "gpt.write"}
	a := WriteAuthorization{PermitID: p.ID, InstallationID: p.InstallationID, BootID: p.BootID, Sequence: p.Sequence, Nonce: q.Nonce, Stage: q.Stage, StoragePlanSHA256: p.StoragePlanSHA256, GPTSHA256: p.GPTSHA256, CheckedAt: now, ValidUntil: now.Add(time.Second)}
	if ValidateWriteAuthorization(&a, p, q, now) != nil {
		t.Fatal("current authorization denied")
	}
	for name, change := range map[string]func(*WriteAuthorization){
		"nonce":            func(x *WriteAuthorization) { x.Nonce = strings.Repeat("3", 32) },
		"stage":            func(x *WriteAuthorization) { x.Stage = "root.extract" },
		"expired":          func(x *WriteAuthorization) { x.ValidUntil = now },
		"unbounded":        func(x *WriteAuthorization) { x.ValidUntil = now.Add(3 * time.Second) },
		"stale_checked_at": func(x *WriteAuthorization) { x.CheckedAt = now.Add(-3 * time.Second) },
		"other_disk_bytes": func(x *WriteAuthorization) { x.GPTSHA256 = strings.Repeat("f", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			copy := a
			change(&copy)
			if ValidateWriteAuthorization(&copy, p, q, now) == nil {
				t.Fatal("stale or wrong authorization accepted")
			}
		})
	}
	if ValidateWriteAuthorization(&a, p, q, now.Add(2*time.Second)) == nil {
		t.Fatal("elapsed authorization reusable")
	}
}

type writeCallbackFixture struct {
	*callbackFixture
	permit *WritePermit
}

func (f *writeCallbackFixture) IssueWritePermit(ctx context.Context, s Scope, id string, peer netip.Addr, in WritePermitSubmission) (*WritePermit, error) {
	payload, err := WritePermitPayload(in)
	if err != nil || ctx.Err() != nil || !s.CheckPeer(peer) || f.permit != nil || VerifyBootProof(f.key, f.installation, f.boot, f.sequence+1, "write-permit", payload, in.Proof) != nil {
		return nil, ErrDenied
	}
	f.permit = writePermitFixture(time.Now().UTC())
	f.permit.Nonce = in.Preparation.Nonce
	f.sequence++
	f.permit.Sequence = f.sequence
	return f.permit, nil
}
func (f *writeCallbackFixture) CheckWritePermit(ctx context.Context, s Scope, id string, peer netip.Addr, in WriteCheckSubmission) (*WriteAuthorization, error) {
	payload, _ := json.Marshal(in.Query)
	if f.permit == nil || ctx.Err() != nil || !s.CheckPeer(peer) || in.Query.PermitID != f.permit.ID || VerifyBootProof(f.key, f.installation, f.boot, f.sequence, "write-check", payload, in.Proof) != nil {
		return nil, ErrDenied
	}
	now := time.Now().UTC()
	return &WriteAuthorization{PermitID: f.permit.ID, InstallationID: f.installation, BootID: f.boot, Sequence: f.sequence, Nonce: in.Query.Nonce, Stage: in.Query.Stage, StoragePlanSHA256: f.permit.StoragePlanSHA256, GPTSHA256: f.permit.GPTSHA256, CheckedAt: now, ValidUntil: now.Add(time.Second)}, nil
}
func (f *writeCallbackFixture) ReportWriteReceipt(ctx context.Context, s Scope, id string, peer netip.Addr, in WriteReceiptSubmission) (*BootAcknowledgment, error) {
	payload, _ := json.Marshal(in.Receipt)
	if f.permit == nil || ctx.Err() != nil || !s.CheckPeer(peer) || ValidateWriteReceipt(in.Receipt, f.permit) != nil || VerifyBootProof(f.key, f.installation, f.boot, f.sequence+1, "install-stopped", payload, in.Proof) != nil {
		return nil, ErrDenied
	}
	f.sequence++
	state := "unknown"
	if in.Receipt.Outcome == "written" {
		state = "awaiting_first_boot"
	}
	return &BootAcknowledgment{InstallationID: f.installation, BootID: f.boot, Sequence: f.sequence, State: state, ExpiresAt: f.expires}, nil
}

func TestWriteGatewayOneShotPermitReadonlyChecksAndUnknownReceipt(t *testing.T) {
	s, _ := fixture()
	enrolled, key := enrollmentFixture(t)
	f := &writeCallbackFixture{callbackFixture: &callbackFixture{key: key.Public().(ed25519.PublicKey), installation: enrolled.Proof.InstallationID, boot: enrolled.Proof.BootID, sequence: 2, expires: time.Now().Add(time.Minute)}}
	g := &CallbackGateway{Scope: s, Authority: f}
	in := WritePermitSubmission{Preparation: writePreparationFixture(), Report: []byte("facts fixture only")}
	payload, _ := WritePermitPayload(in)
	in.Proof, _ = SignBootProof(key, f.installation, f.boot, 3, "write-permit", payload)
	raw, _ := json.Marshal(in)
	if w := callbackRequest(g, "write-permit", "", raw, nil); w.Code != 200 {
		t.Fatal("permit", w.Code)
	}
	if w := callbackRequest(g, "write-permit", "", raw, nil); w.Code != 404 {
		t.Fatal("permit replay", w.Code)
	}
	for _, stage := range []string{"gpt.write", "root.extract"} {
		query := WriteCheckQuery{PermitID: f.permit.ID, Nonce: strings.Repeat("7", 32), Stage: stage}
		body, _ := json.Marshal(query)
		proof, _ := SignBootProof(key, f.installation, f.boot, 3, "write-check", body)
		request, _ := json.Marshal(WriteCheckSubmission{Query: query, Proof: proof})
		if w := callbackRequest(g, "write-check", "", request, nil); w.Code != 200 || f.sequence != 3 {
			t.Fatal("check consumed sequence", w.Code)
		}
		query.Stage = "final.sync"
		request, _ = json.Marshal(WriteCheckSubmission{Query: query, Proof: proof})
		if w := callbackRequest(g, "write-check", "", request, nil); w.Code != 404 {
			t.Fatal("unsigned stage mutation accepted", w.Code)
		}
	}
	receipt := WriteReceipt{PermitID: f.permit.ID, StoragePlanSHA256: f.permit.StoragePlanSHA256, GPTSHA256: f.permit.GPTSHA256, RootFSSHA256: f.permit.RootFSSHA256, TargetUKISHA256: f.permit.TargetUKISHA256, Outcome: "unknown", Reason: "connection interrupted"}
	body, _ := json.Marshal(receipt)
	proof, _ := SignBootProof(key, f.installation, f.boot, 4, "install-stopped", body)
	raw, _ = json.Marshal(WriteReceiptSubmission{Receipt: receipt, Proof: proof})
	if w := callbackRequest(g, "write-receipt", "", raw, nil); w.Code != 200 || f.sequence != 4 {
		t.Fatal("unknown receipt", w.Code)
	}
	if w := callbackRequest(g, "write-receipt", "", raw, nil); w.Code != 404 {
		t.Fatal("receipt replay", w.Code)
	}
}
