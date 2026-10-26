package provisionboot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/netip"
	"time"

	"github.com/enzoOvOyu/keel/internal/provisionfirstboot"
)

type FirstBootAuthority interface {
	ReportFirstBoot(context.Context, Scope, string, netip.Addr, provisionfirstboot.Submission) (*provisionfirstboot.Acknowledgment, error)
}

func (g *CallbackGateway) serveFirstBoot(ctx context.Context, w http.ResponseWriter, r *http.Request, grant string, peer netip.Addr, raw []byte) {
	deny := func() { http.NotFound(w, r) }
	authority, ok := g.Authority.(FirstBootAuthority)
	in, err := provisionfirstboot.DecodeSubmission(raw)
	if !ok || err != nil || in.Report.GrantID != grant {
		deny()
		return
	}
	out, err := authority.ReportFirstBoot(ctx, g.Scope, grant, peer, *in)
	if err != nil || out == nil || ctx.Err() != nil || out.InstallationID != in.Report.InstallationID || out.BootID != in.Report.BootID || out.BindingSHA256 != in.Report.BindingSHA256 || out.Nonce != in.Report.Nonce || out.State != "awaiting_first_boot" || out.HostFingerprint != in.Report.HostFingerprint || out.ReceivedAt.Before(time.Now().Add(-30*time.Second)) || out.ReceivedAt.After(time.Now().Add(time.Second)) {
		deny()
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}
