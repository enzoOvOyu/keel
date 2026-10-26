//go:build linux

package provisionfirstboot

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
	"regexp"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

var ifaceName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,14}$`)

type HTTPConfig struct {
	Binding      Binding
	RootCAs      *x509.CertPool
	Interface    string
	LocalAddress netip.Addr
	// Development allows only loopback TLS without privileged device binding.
	// The installed command never exposes this option.
	Development bool
}
type HTTPTransport struct {
	client   *http.Client
	endpoint string
	binding  Binding
}

func NewHTTPTransport(config HTTPConfig) (*HTTPTransport, error) {
	b := config.Binding
	if b.Validate(time.Now()) != nil || config.RootCAs == nil || !ifaceName.MatchString(config.Interface) || !config.LocalAddress.Is4() || !b.Clients.Contains(config.LocalAddress) || config.LocalAddress == b.Server || config.LocalAddress == b.Clients.Addr() || config.LocalAddress == scopeBroadcast(b.Clients) {
		return nil, ErrDenied
	}
	face, err := net.InterfaceByName(config.Interface)
	if err != nil {
		return nil, ErrDenied
	}
	if config.Development {
		if config.Interface != "lo" || !config.LocalAddress.IsLoopback() || !b.Server.IsLoopback() {
			return nil, ErrDenied
		}
	} else {
		if face.HardwareAddr.String() != b.StoragePlan.Spec.Expected.BootMAC || face.Flags&net.FlagUp == 0 {
			return nil, ErrDenied
		}
		addresses, err := face.Addrs()
		if err != nil {
			return nil, ErrDenied
		}
		assigned := false
		for _, address := range addresses {
			p, err := netip.ParsePrefix(address.String())
			if err == nil && p.Addr().Unmap() == config.LocalAddress {
				assigned = true
			}
		}
		if !assigned {
			return nil, ErrDenied
		}
	}
	origin, _ := url.Parse(b.Origin)
	port := origin.Port()
	if port == "" {
		port = "443"
	}
	destination := net.JoinHostPort(b.Server.String(), port)
	dialer := net.Dialer{Timeout: 10 * time.Second, LocalAddr: &net.TCPAddr{IP: net.IP(config.LocalAddress.AsSlice())}}
	if !config.Development {
		dialer.Control = func(_, _ string, raw syscall.RawConn) error {
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
	transport := &http.Transport{Proxy: nil, DisableCompression: true, DisableKeepAlives: true, MaxConnsPerHost: 1, MaxResponseHeaderBytes: 32 << 10, ResponseHeaderTimeout: 10 * time.Second, TLSHandshakeTimeout: 10 * time.Second, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: config.RootCAs.Clone(), ServerName: origin.Hostname()}, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		if network != "tcp" || address != net.JoinHostPort(origin.Hostname(), port) {
			return nil, ErrDenied
		}
		return dialer.DialContext(ctx, "tcp4", destination)
	}}
	return &HTTPTransport{client: &http.Client{Transport: transport, Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return ErrDenied }}, endpoint: b.Origin + "/boot/" + b.GrantID + "/first-boot", binding: b}, nil
}

func (h *HTTPTransport) Close() {
	if h != nil && h.client != nil {
		h.client.CloseIdleConnections()
	}
}
func (h *HTTPTransport) Submit(ctx context.Context, in Submission) (*Acknowledgment, error) {
	if h == nil || h.client == nil || VerifySignature(h.binding, in, time.Now()) != nil || ctx.Err() != nil {
		return nil, ErrDenied
	}
	raw, err := json.Marshal(in)
	if err != nil || len(raw) > 32<<10 {
		return nil, ErrDenied
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.endpoint, bytes.NewReader(raw))
	if err != nil {
		return nil, ErrDenied
	}
	req.GetBody = nil
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Encoding", "identity")
	response, err := h.client.Do(req)
	if err != nil {
		return nil, ErrDenied
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.ContentLength > 8192 || response.Header.Get("Content-Encoding") != "" || response.Header.Get("Content-Range") != "" {
		return nil, ErrDenied
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 8193))
	if err != nil || len(body) == 0 || len(body) > 8192 || ctx.Err() != nil {
		return nil, ErrDenied
	}
	ack, err := DecodeAcknowledgment(body)
	if err != nil || ValidateAcknowledgment(ack, h.binding, in.Report, time.Now()) != nil {
		return nil, ErrDenied
	}
	return ack, nil
}
