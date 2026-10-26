package provisionboot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"
)

type ReadSeekCloser interface {
	io.Reader
	io.Seeker
	io.Closer
}
type Artifact struct {
	File          ReadSeekCloser
	SHA256        string
	Size          int64
	ObjectVersion string
}

// FileAuthority resolves each resource from the frozen approved media closure,
// or deterministic platform rendering, and rechecks the running attempt/fence,
// current approval, revocation and exclusive lease. No directory/file path or
// client-provided URI is accepted. OpenFile must return an immutable pinned file.
type FileAuthority interface {
	AuthorizeFile(context.Context, Scope, string, netip.Addr, string) (Grant, error)
	CheckFile(context.Context, Scope, Grant, netip.Addr, string) error
	OpenFile(context.Context, Scope, Grant, string) (Artifact, error)
}
type Gateway struct {
	Scope         Scope
	Authority     FileAuthority
	MaxConcurrent int
	once          sync.Once
	slots         chan struct{}
}

func resourceLimit(resource string) int64 {
	switch resource {
	case "boot.ipxe", "user-data", "meta-data":
		return 512 << 10
	case "preflight.json":
		return 64 << 10
	case "manifest.json":
		return 4 << 20
	case "ipxe.efi", "undionly.kpxe":
		return 16 << 20
	case "preflight", "ssh-helper":
		return 64 << 20
	case "kernel", "initrd":
		return 1 << 30
	case "rootfs":
		return 64 << 30
	}
	return 0
}
func resourcePath(path string) (string, string, error) {
	parts := strings.Split(path, "/")
	if len(parts) != 4 || parts[0] != "" || parts[1] != "boot" || !opaqueID.MatchString(parts[2]) || resourceLimit(parts[3]) == 0 {
		return "", "", ErrDenied
	}
	return parts[2], parts[3], nil
}
func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	unavailable := func() { http.NotFound(w, r) }
	if g == nil || g.Authority == nil || g.Scope.Validate() != nil {
		unavailable()
		return
	}
	origin, _ := url.Parse(g.Scope.Origin)
	// Direct peer and exact public authority only. Forwarded headers do not turn
	// an untrusted proxy/client address into an approved management-network peer.
	if r.Host != origin.Host || (origin.Scheme == "https" && r.TLS == nil) || r.URL.RawQuery != "" || r.URL.RawPath != "" || r.URL.Fragment != "" {
		unavailable()
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		unavailable()
		return
	}
	peer, err := netip.ParseAddr(host)
	if err != nil || !g.Scope.CheckPeer(peer) {
		unavailable()
		return
	}
	id, resource, err := resourcePath(r.URL.Path)
	if err != nil {
		unavailable()
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
		unavailable()
		return
	}
	select {
	case g.slots <- struct{}{}:
		defer func() { <-g.slots }()
	default:
		w.Header().Set("Retry-After", "1")
		http.Error(w, "boot distribution busy", http.StatusServiceUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	grant, err := g.Authority.AuthorizeFile(ctx, g.Scope, id, peer, resource)
	if err != nil || grant.Validate(g.Scope, time.Now()) != nil || grant.ID != id {
		unavailable()
		return
	}
	artifact, err := g.Authority.OpenFile(ctx, g.Scope, grant, resource)
	if err != nil {
		if artifact.File != nil {
			artifact.File.Close()
		}
		unavailable()
		return
	}
	if artifact.File == nil {
		unavailable()
		return
	}
	defer artifact.File.Close()
	if err = verifyArtifact(ctx, artifact, resource); err != nil {
		unavailable()
		return
	}
	if grant.Validate(g.Scope, time.Now()) != nil || g.Authority.CheckFile(ctx, g.Scope, grant, peer, resource) != nil || ctx.Err() != nil || grant.Validate(g.Scope, time.Now()) != nil {
		unavailable()
		return
	}
	w.Header().Set("ETag", `"sha256:`+artifact.SHA256+`"`)
	w.Header().Set("Content-Type", "application/octet-stream")
	if resource == "boot.ipxe" || resource == "user-data" || resource == "meta-data" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	}
	if resource == "manifest.json" || resource == "preflight.json" {
		w.Header().Set("Content-Type", "application/json")
	}
	// ServeContent implements conditional/Range/HEAD over the verified exact
	// version. Authentication and full verification always precede any 304/206.
	http.ServeContent(w, r.WithContext(ctx), resource, time.Time{}, contextSeeker{ctx: ctx, reader: artifact.File, guard: &fileGuard{ctx: ctx, scope: g.Scope, authority: g.Authority, grant: grant, peer: peer, resource: resource}})
}
func verifyArtifact(ctx context.Context, a Artifact, resource string) error {
	if a.File == nil || !sha256ID.MatchString(a.SHA256) || a.Size < 1 || a.Size > resourceLimit(resource) || resourceLimit(resource) == 0 || len(a.ObjectVersion) < 1 || len(a.ObjectVersion) > 4096 || a.ObjectVersion == "null" {
		return ErrDenied
	}
	if _, err := a.File.Seek(0, io.SeekStart); err != nil {
		return ErrDenied
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(contextReader{ctx, a.File}, a.Size+1))
	if err != nil || n != a.Size || hex.EncodeToString(h.Sum(nil)) != a.SHA256 {
		return ErrDenied
	}
	_, err = a.File.Seek(0, io.SeekStart)
	if err != nil {
		return ErrDenied
	}
	return nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

type contextSeeker struct {
	ctx    context.Context
	reader ReadSeekCloser
	guard  *fileGuard
}

func (r contextSeeker) Read(p []byte) (int, error) {
	if r.guard != nil && !r.guard.check() {
		return 0, ErrDenied
	}
	return contextReader{r.ctx, r.reader}.Read(p)
}
func (r contextSeeker) Seek(offset int64, whence int) (int64, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Seek(offset, whence)
}

// Check every read for expiry/context and revalidate authority at most 250ms
// apart. This bounds revocation delay without one SQL call per 512-byte block.
// A failed check is sticky for this transfer; it never resurrects itself.
type fileGuard struct {
	ctx       context.Context
	scope     Scope
	authority FileAuthority
	grant     Grant
	peer      netip.Addr
	resource  string
	checked   time.Time
	denied    bool
}

func (g *fileGuard) check() bool {
	now := time.Now()
	if g.denied || g.ctx.Err() != nil || !g.grant.ExpiresAt.After(now) {
		return false
	}
	if !g.checked.IsZero() && now.Sub(g.checked) < 250*time.Millisecond {
		return true
	}
	g.checked = now
	g.denied = g.authority.CheckFile(g.ctx, g.scope, g.grant, g.peer, g.resource) != nil
	return !g.denied && g.ctx.Err() == nil && g.grant.ExpiresAt.After(time.Now())
}
