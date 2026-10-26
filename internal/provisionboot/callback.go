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
	if len(parts) != 4 || parts[0] != "" || parts[1] != "boot" || !opaqueID.MatchString(parts[2]) || (parts[3] != "enroll" && parts[3] != "preflight") {
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
	if parts[3] == "enroll" {
		limit = 8192
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
		if DecodeCallback(raw, &in) != nil || len(in.Report) == 0 || len(in.Report) > 2<<20 || in.Proof.Operation != "preflight" || in.Proof.Sequence < 2 {
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
