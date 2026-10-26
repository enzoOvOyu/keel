package provisionboot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/netip"
	"time"
)

func (g *CallbackGateway) serveWriteCallback(ctx context.Context, w http.ResponseWriter, r *http.Request, grant, operation string, peer netip.Addr, raw []byte) {
	deny := func() { http.NotFound(w, r) }
	authority, ok := g.Authority.(WriteAuthority)
	if !ok {
		deny()
		return
	}
	var out any
	switch operation {
	case "write-permit":
		var in WritePermitSubmission
		if DecodeCallback(raw, &in) != nil || in.Proof.Operation != "write-permit" || in.Proof.Sequence < 3 {
			deny()
			return
		}
		if _, err := WritePermitPayload(in); err != nil {
			deny()
			return
		}
		permit, err := authority.IssueWritePermit(ctx, g.Scope, grant, peer, in)
		if err != nil || ctx.Err() != nil || ValidateWritePermit(permit, in.Proof, in.Preparation, time.Now()) != nil {
			deny()
			return
		}
		out = permit
	case "write-check":
		var in WriteCheckSubmission
		if DecodeCallback(raw, &in) != nil || in.Proof.Operation != "write-check" || in.Proof.Sequence < 3 || !opaqueID.MatchString(in.Query.PermitID) || !opaqueID.MatchString(in.Query.Nonce) || !ValidWriteStage(in.Query.Stage) {
			deny()
			return
		}
		a, err := authority.CheckWritePermit(ctx, g.Scope, grant, peer, in)
		if err != nil || a == nil || ctx.Err() != nil || a.InstallationID != in.Proof.InstallationID || a.BootID != in.Proof.BootID || a.Sequence != in.Proof.Sequence || !sha256ID.MatchString(a.StoragePlanSHA256) || !sha256ID.MatchString(a.GPTSHA256) {
			deny()
			return
		}
		p := &WritePermit{ID: in.Query.PermitID, InstallationID: in.Proof.InstallationID, BootID: in.Proof.BootID, Sequence: in.Proof.Sequence, StoragePlanSHA256: a.StoragePlanSHA256, GPTSHA256: a.GPTSHA256, ExpiresAt: a.ValidUntil}
		if ValidateWriteAuthorization(a, p, in.Query, time.Now()) != nil {
			deny()
			return
		}
		out = a
	case "write-receipt":
		var in WriteReceiptSubmission
		if DecodeCallback(raw, &in) != nil || in.Proof.Operation != "install-stopped" || in.Proof.Sequence < 4 {
			deny()
			return
		}
		ack, err := authority.ReportWriteReceipt(ctx, g.Scope, grant, peer, in)
		state := "unknown"
		if in.Receipt.Outcome == "written" {
			state = "awaiting_first_boot"
		}
		if err != nil || ack == nil || ctx.Err() != nil || ack.InstallationID != in.Proof.InstallationID || ack.BootID != in.Proof.BootID || ack.Sequence != in.Proof.Sequence || ack.State != state || !ack.ExpiresAt.After(time.Now()) {
			deny()
			return
		}
		out = ack
	default:
		deny()
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}
