package provisionboot

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"
)

// CallbackAuthority must compare Scope against the current approved database
// scope inside the same transaction that consumes the proof. Peer admission is
// a network boundary, never authentication or machine identity by itself.
type CallbackAuthority interface {
	EnrollBoot(context.Context, Scope, string, netip.Addr, string, EnrollmentSubmission) (*BootAcknowledgment, error)
	ReportPreflight(context.Context, Scope, string, netip.Addr, PreflightSubmission) (*BootAcknowledgment, error)
}
type ReviewAuthority interface {
	ReviewBoot(context.Context, Scope, string, netip.Addr, ReviewSubmission) (*ReviewStatus, error)
}
type WriteAuthority interface {
	IssueWritePermit(context.Context, Scope, string, netip.Addr, WritePermitSubmission) (*WritePermit, error)
	CheckWritePermit(context.Context, Scope, string, netip.Addr, WriteCheckSubmission) (*WriteAuthorization, error)
	ReportWriteReceipt(context.Context, Scope, string, netip.Addr, WriteReceiptSubmission) (*BootAcknowledgment, error)
}
type CallbackGateway struct {
	Scope         Scope
	Authority     CallbackAuthority
	MaxConcurrent int
	once          sync.Once
	slots         chan struct{}
}

func (g *CallbackGateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	deny := func() { http.NotFound(w, r) }
	if g == nil || g.Authority == nil || g.Scope.Validate() != nil {
		deny()
		return
	}
	origin, _ := url.Parse(g.Scope.Origin)
	if origin.Scheme != "https" || r.TLS == nil || r.Host != origin.Host || r.URL.RawQuery != "" || r.URL.RawPath != "" || r.URL.Fragment != "" || r.Header.Get("Origin") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("Content-Encoding") != "" {
		deny()
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		deny()
		return
	}
	peer, err := netip.ParseAddr(host)
	if err != nil || !g.Scope.CheckPeer(peer) {
		deny()
		return
	}
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) != 4 || parts[0] != "" || parts[1] != "boot" || !opaqueID.MatchString(parts[2]) || (parts[3] != "enroll" && parts[3] != "preflight" && parts[3] != "review" && parts[3] != "write-permit" && parts[3] != "write-check" && parts[3] != "write-receipt" && parts[3] != "first-boot") {
		deny()
		return
	}
	kind, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || kind != "application/json" || len(params) > 1 || (len(params) == 1 && !strings.EqualFold(params["charset"], "utf-8")) {
		http.Error(w, "application/json required", http.StatusUnsupportedMediaType)
		return
	}
	var token string
	if parts[3] == "enroll" {
		values := r.Header.Values("Authorization")
		if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") {
			deny()
			return
		}
		token = strings.TrimPrefix(values[0], "Bearer ")
		if len(token) != 64 || !sha256ID.MatchString(token) {
			deny()
			return
		}
	} else if len(r.Header.Values("Authorization")) != 0 {
		deny()
		return
	}
	g.once.Do(func() {
		n := g.MaxConcurrent
		if n == 0 {
			n = 8
		}
		if n >= 1 && n <= 64 {
			g.slots = make(chan struct{}, n)
		}
	})
	if g.slots == nil {
		deny()
		return
	}
	select {
	case g.slots <- struct{}{}:
		defer func() { <-g.slots }()
	default:
		w.Header().Set("Retry-After", "1")
		http.Error(w, "boot callback busy", http.StatusServiceUnavailable)
		return
	}
	limit := int64(MaximumCallbackBytes)
	if parts[3] == "write-permit" {
		limit = MaximumWriteCallbackBytes
	}
	if parts[3] != "preflight" && parts[3] != "write-permit" {
		limit = 8192
	}
	if parts[3] == "first-boot" {
		limit = 32 << 10
	}
	if r.ContentLength > limit {
		http.Error(w, "callback too large", http.StatusRequestEntityTooLarge)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	controller := http.NewResponseController(w)
	_ = controller.SetReadDeadline(time.Now().Add(15 * time.Second))
	defer controller.SetReadDeadline(time.Time{})
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		deny()
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if parts[3] == "first-boot" {
		g.serveFirstBoot(ctx, w, r, parts[2], peer, raw)
		return
	}
	if parts[3] == "write-permit" || parts[3] == "write-check" || parts[3] == "write-receipt" {
		g.serveWriteCallback(ctx, w, r, parts[2], parts[3], peer, raw)
		return
	}
	if parts[3] == "review" {
		authority, ok := g.Authority.(ReviewAuthority)
		var in ReviewSubmission
		if !ok || DecodeCallback(raw, &in) != nil || !opaqueID.MatchString(in.Query.Nonce) || in.Proof.Operation != "review" || in.Proof.Sequence < 2 {
			deny()
			return
		}
		out, err := authority.ReviewBoot(ctx, g.Scope, parts[2], peer, in)
		if err != nil || ctx.Err() != nil || ValidateReviewStatus(out, in.Proof, in.Query.Nonce, time.Now()) != nil {
			deny()
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
		return
	}
	var ack *BootAcknowledgment
	var proof BootProof
	if parts[3] == "enroll" {
		var in EnrollmentSubmission
		if DecodeCallback(raw, &in) != nil || in.Proof.Operation != "enroll" || in.Proof.Sequence != 1 || in.Enrollment.InstallationID != in.Proof.InstallationID || in.Enrollment.BootID != in.Proof.BootID {
			deny()
			return
		}
		proof = in.Proof
		ack, err = g.Authority.EnrollBoot(ctx, g.Scope, parts[2], peer, token, in)
	} else {
		var in PreflightSubmission
		if DecodeCallback(raw, &in) != nil {
			deny()
			return
		}
		_, operation, payloadErr := PreflightProofPayload(in)
		if payloadErr != nil || in.Proof.Operation != operation || in.Proof.Sequence < 2 {
			deny()
			return
		}
		proof = in.Proof
		ack, err = g.Authority.ReportPreflight(ctx, g.Scope, parts[2], peer, in)
	}
	if err != nil || ack == nil || ctx.Err() != nil || ack.InstallationID != proof.InstallationID || ack.BootID != proof.BootID || ack.Sequence != proof.Sequence || !ack.ExpiresAt.After(time.Now()) || (parts[3] == "enroll" && ack.State != "booting") || (parts[3] == "preflight" && ack.State != "preflight_ready" && ack.State != "failed") {
		deny()
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(ack)
}

func ValidateReviewStatus(out *ReviewStatus, proof BootProof, nonce string, now time.Time) error {
	if out == nil || out.InstallationID != proof.InstallationID || out.BootID != proof.BootID || out.Sequence != proof.Sequence || !opaqueID.MatchString(nonce) || out.Nonce != nonce || !out.ExpiresAt.After(now) || !sha256ID.MatchString(out.ReportSHA256) {
		return ErrDenied
	}
	if out.RefreshID != "" && (!opaqueID.MatchString(out.RefreshID) || out.State != "preflight_ready") {
		return ErrDenied
	}
	if out.RuntimeInputSHA256 != "" && !sha256ID.MatchString(out.RuntimeInputSHA256) {
		return ErrDenied
	}
	if out.StoragePlan != nil && (out.State != "storage_approved" || out.StoragePlan.SHA256 != out.StoragePlanSHA256 || out.StoragePlan.Validate(now) != nil) {
		return ErrDenied
	}
	switch out.State {
	case "preflight_ready":
		if out.StoragePlanSHA256 != "" {
			return ErrDenied
		}
	case "awaiting_storage_review", "storage_approved":
		if !sha256ID.MatchString(out.StoragePlanSHA256) {
			return ErrDenied
		}
	default:
		return ErrDenied
	}
	return nil
}
