package provisionboot

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func (f *callbackFixture) ReviewBoot(ctx context.Context, s Scope, id string, peer netip.Addr, in ReviewSubmission) (*ReviewStatus, error) {
	payload, _ := json.Marshal(in.Query)
	if ctx.Err() != nil || !s.CheckPeer(peer) || !opaqueID.MatchString(id) || f.sequence < 2 || VerifyBootProof(f.key, f.installation, f.boot, f.sequence, "review", payload, in.Proof) != nil {
		return nil, ErrDenied
	}
	return &ReviewStatus{InstallationID: f.installation, BootID: f.boot, Sequence: f.sequence, Nonce: in.Query.Nonce, State: "preflight_ready", ExpiresAt: f.expires, ReportSHA256: strings.Repeat("a", 64)}, nil
}

func TestBootReviewReadOnlyNonceAndOperationBinding(t *testing.T) {
	s, _ := fixture()
	f := &callbackFixture{token: strings.Repeat("a", 64), expires: time.Now().Add(time.Minute)}
	g := &CallbackGateway{Scope: s, Authority: f}
	enrollment, key := enrollmentFixture(t)
	raw, _ := json.Marshal(enrollment)
	if w := callbackRequest(g, "enroll", f.token, raw, nil); w.Code != 200 {
		t.Fatal("enroll", w.Code)
	}
	report := []byte("exact report bytes")
	proof, err := SignBootProof(key, f.installation, f.boot, 2, "preflight", report)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(PreflightSubmission{Report: report, Proof: proof})
	if w := callbackRequest(g, "preflight", "", raw, nil); w.Code != 200 {
		t.Fatal("report", w.Code)
	}
	for _, nonce := range []string{strings.Repeat("1", 32), strings.Repeat("2", 32)} {
		query := ReviewQuery{Nonce: nonce}
		payload, _ := json.Marshal(query)
		proof, err = SignBootProof(key, f.installation, f.boot, 2, "review", payload)
		if err != nil {
			t.Fatal(err)
		}
		in := ReviewSubmission{Query: query, Proof: proof}
		raw, _ = json.Marshal(in)
		w := callbackRequest(g, "review", "", raw, nil)
		var status ReviewStatus
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &status) != nil || status.Nonce != nonce || f.sequence != 2 {
			t.Fatal("read changed sequence or nonce", w.Code)
		}
		in.Query.Nonce = strings.Repeat("3", 32)
		raw, _ = json.Marshal(in)
		if w := callbackRequest(g, "review", "", raw, nil); w.Code != 404 {
			t.Fatal("unsigned nonce change accepted", w.Code)
		}
		in.Query = query
		in.Proof.Operation = "write-permit"
		raw, _ = json.Marshal(in)
		if w := callbackRequest(g, "review", "", raw, nil); w.Code != 404 {
			t.Fatal("status became permit", w.Code)
		}
	}
}

func TestBootRefreshProofIncludesRequestAndExactReport(t *testing.T) {
	in, key := enrollmentFixture(t)
	report := PreflightSubmission{Report: []byte("raw report\n"), RefreshID: strings.Repeat("c", 32)}
	payload, operation, err := PreflightProofPayload(report)
	if err != nil {
		t.Fatal(err)
	}
	proof, err := SignBootProof(key, in.Proof.InstallationID, in.Proof.BootID, 3, operation, payload)
	if err != nil {
		t.Fatal(err)
	}
	public := key.Public().(ed25519.PublicKey)
	if VerifyBootProof(public, proof.InstallationID, proof.BootID, 3, operation, payload, proof) != nil {
		t.Fatal("original refresh transcript rejected")
	}
	for _, change := range []func(*PreflightSubmission){func(x *PreflightSubmission) { x.RefreshID = strings.Repeat("d", 32) }, func(x *PreflightSubmission) { x.Report = []byte("raw report") }, func(x *PreflightSubmission) { x.RefreshID = "" }} {
		x := report
		change(&x)
		raw, op, err := PreflightProofPayload(x)
		if err == nil && VerifyBootProof(public, proof.InstallationID, proof.BootID, 3, op, raw, proof) == nil {
			t.Fatal("changed request/report accepted")
		}
	}
}
