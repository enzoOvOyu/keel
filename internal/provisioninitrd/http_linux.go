//go:build linux

package provisioninitrd

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"

	"github.com/enzoOvOyu/keel/internal/provisionboot"
	"golang.org/x/sys/unix"
)

type HTTPFiles struct {
	client *http.Client
	base   string
}
type HTTPConfig struct {
	Input        Input
	RootCAs      *x509.CertPool
	Interface    string
	LocalAddress netip.Addr
	// Development permits only loopback TLS, without a privileged device bind.
	// The production kernel-command-line parser never enables this mode.
	Development bool
}

// NewHTTPFiles pins the TCP destination to the explicit boot server and TLS to
// its original hostname. It uses no DNS, environment proxy, redirects or cookies.
func NewHTTPFiles(config HTTPConfig) (*HTTPFiles, error) {
	i := config.Input
	if i.validate(config.Development) != nil || config.RootCAs == nil || !interfacePattern.MatchString(config.Interface) || !config.LocalAddress.Is4() || !i.Clients.Contains(config.LocalAddress) || config.LocalAddress == i.Server || config.LocalAddress == i.Clients.Addr() || config.LocalAddress == broadcast(i.Clients) {
		return nil, ErrUnavailable
	}
	face, err := net.InterfaceByName(config.Interface)
	if err != nil {
		return nil, ErrUnavailable
	}
	if config.Development {
		if config.Interface != "lo" || !config.LocalAddress.IsLoopback() || !i.Server.IsLoopback() {
			return nil, ErrUnavailable
		}
	} else {
		if face.HardwareAddr.String() != i.BootMAC || face.Flags&net.FlagUp == 0 {
			return nil, ErrUnavailable
		}
		addresses, err := face.Addrs()
		if err != nil {
			return nil, ErrUnavailable
		}
		assigned := false
		for _, a := range addresses {
			prefix, err := netip.ParsePrefix(a.String())
			if err == nil && prefix.Addr().Unmap() == config.LocalAddress {
				assigned = true
			}
		}
		if !assigned {
			return nil, ErrUnavailable
		}
	}
	parsed, _ := url.Parse(i.ManifestURL)
	port := parsed.Port()
	if port == "" {
		port = "443"
	}
	destination := net.JoinHostPort(i.Server.String(), port)
	dialer := net.Dialer{Timeout: 10 * time.Second, LocalAddr: &net.TCPAddr{IP: net.IP(config.LocalAddress.AsSlice())}}
	if !config.Development {
		dialer.Control = func(network, address string, raw syscall.RawConn) error {
			var bindErr error
			err := raw.Control(func(fd uintptr) {
				bindErr = unix.SetsockoptString(int(fd), unix.SOL_SOCKET, unix.SO_BINDTODEVICE, config.Interface)
			})
			if err != nil {
				return err
			}
			return bindErr
		}
	}
	transport := &http.Transport{Proxy: nil, DisableCompression: true, DisableKeepAlives: true, MaxConnsPerHost: 1, MaxResponseHeaderBytes: 64 << 10, ResponseHeaderTimeout: 10 * time.Second, TLSHandshakeTimeout: 10 * time.Second, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: config.RootCAs.Clone(), ServerName: parsed.Hostname()}, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		if network != "tcp" || address != net.JoinHostPort(parsed.Hostname(), port) {
			return nil, ErrUnavailable
		}
		return dialer.DialContext(ctx, "tcp4", destination)
	}}
	return &HTTPFiles{client: &http.Client{Transport: transport, Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return ErrUnavailable }}, base: strings.TrimSuffix(i.ManifestURL, "manifest.json")}, nil
}
func (h *HTTPFiles) Close() {
	if h != nil && h.client != nil {
		h.client.CloseIdleConnections()
	}
}
func fileLimit(name string) int64 {
	switch name {
	case "manifest.json":
		return 4 << 20
	case "preflight.json":
		return 64 << 10
	case "user-data":
		return 512 << 10
	case "meta-data":
		return 64 << 10
	}
	return 0
}
func (h *HTTPFiles) Get(ctx context.Context, name string) ([]byte, error) {
	limit := fileLimit(name)
	if h == nil || h.client == nil || limit == 0 {
		return nil, ErrUnavailable
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.base+name, nil)
	if err != nil {
		return nil, ErrUnavailable
	}
	req.Header.Set("Accept-Encoding", "identity")
	response, err := h.client.Do(req)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.ContentLength > limit || response.Header.Get("Content-Encoding") != "" || response.Header.Get("Content-Range") != "" {
		return nil, ErrUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || len(raw) < 1 || int64(len(raw)) > limit || ctx.Err() != nil {
		return nil, ErrUnavailable
	}
	return raw, nil
}

// PostCallback reuses the exact fixed-IP/interface/CA/hostname transport. POSTs
// carry no idempotency key or replayable body and never follow redirects/retry.
func (h *HTTPFiles) postBoot(ctx context.Context, operation, token string, raw []byte) ([]byte, error) {
	requestLimit := provisionboot.MaximumCallbackBytes
	if operation == "write-permit" {
		requestLimit = provisionboot.MaximumWriteCallbackBytes
	}
	if h == nil || h.client == nil || len(raw) == 0 || len(raw) > requestLimit || (operation != "enroll" && operation != "preflight" && operation != "review" && operation != "write-permit" && operation != "write-check" && operation != "write-receipt") || (operation == "enroll" && !digest.MatchString(token)) || (operation != "enroll" && token != "") {
		return nil, ErrUnavailable
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.base+operation, bytes.NewReader(raw))
	if err != nil {
		return nil, ErrUnavailable
	}
	req.GetBody = nil
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Encoding", "identity")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := h.client.Do(req)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer response.Body.Close()
	limit := int64(8192)
	if operation == "review" {
		limit = 128 << 10
	}
	if response.StatusCode != http.StatusOK || response.ContentLength > limit || response.Header.Get("Content-Encoding") != "" || response.Header.Get("Content-Range") != "" {
		return nil, ErrUnavailable
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || len(body) == 0 || int64(len(body)) > limit || ctx.Err() != nil {
		return nil, ErrUnavailable
	}
	return body, nil
}

func (h *HTTPFiles) PostCallback(ctx context.Context, operation, token string, raw []byte) (*provisionboot.BootAcknowledgment, error) {
	if operation != "enroll" && operation != "preflight" {
		return nil, ErrUnavailable
	}
	body, err := h.postBoot(ctx, operation, token, raw)
	if err != nil {
		return nil, err
	}
	var ack provisionboot.BootAcknowledgment
	if json.Unmarshal(body, &ack) != nil {
		return nil, ErrUnavailable
	}
	return &ack, nil
}

func (h *HTTPFiles) ReviewBoot(ctx context.Context, raw []byte) (*provisionboot.ReviewStatus, error) {
	if len(raw) > 8192 {
		return nil, ErrUnavailable
	}
	body, err := h.postBoot(ctx, "review", "", raw)
	if err != nil {
		return nil, err
	}
	var out provisionboot.ReviewStatus
	if json.Unmarshal(body, &out) != nil {
		return nil, ErrUnavailable
	}
	return &out, nil
}
