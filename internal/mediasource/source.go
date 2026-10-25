// Package mediasource fetches explicitly allowed HTTP(S) sources. Source bytes
// remain untrusted until the registry digest and independent approval gates pass.
package mediasource

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

var ErrNotAllowed = errors.New("media source URL is outside the configured origin policy")
var ErrUnavailable = errors.New("media source could not be read completely")

// Public documentation identifies cloud-local metadata addresses outside the
// generic link-local ranges. Never dial these even under an allowed DNS origin.
// https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/retrieve-iid.html
// https://www.alibabacloud.com/help/en/ecs/user-guide/view-instance-metadata/
var blockedMetadataIPv4 = netip.MustParseAddr("100.100.100.200")
var blockedMetadataIPv6 = netip.MustParseAddr("fd00:ec2::254")

type Fetcher struct {
	client      *http.Client
	origins     map[string]bool
	addresses   map[string]bool
	development bool
	resolve     func(context.Context, string) ([]netip.Addr, error)
	dial        func(context.Context, string, string) (net.Conn, error)
}

func Load() (*Fetcher, error) {
	return New(os.Getenv("KEEL_MEDIA_FETCH_ORIGINS"), os.Getenv("KEEL_ENV") == "development")
}
func New(origins string, development bool) (*Fetcher, error) {
	if strings.TrimSpace(origins) == "" {
		return nil, nil
	}
	entries := strings.Split(origins, ",")
	if len(entries) > 32 {
		return nil, ErrNotAllowed
	}
	f := &Fetcher{origins: map[string]bool{}, addresses: map[string]bool{}, development: development}
	f.resolve = func(ctx context.Context, host string) ([]netip.Addr, error) {
		return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	f.dial = dialer.DialContext
	for _, entry := range entries {
		u, err := url.Parse(strings.TrimSpace(entry))
		if err != nil {
			return nil, ErrNotAllowed
		}
		if u.Path != "" && u.Path != "/" {
			return nil, ErrNotAllowed
		}
		origin, address, err := sourceIdentity(u, development)
		if err != nil {
			return nil, err
		}
		f.origins[origin] = true
		f.addresses[address] = true
	}
	transport := &http.Transport{Proxy: nil, DialContext: f.dialSource, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 15 * time.Second, MaxResponseHeaderBytes: 64 << 10, DisableCompression: true, DisableKeepAlives: true}
	f.client = &http.Client{Transport: transport, Timeout: 5 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 3 {
			return ErrNotAllowed
		}
		return f.check(req.URL)
	}}
	return f, nil
}
func sourceIdentity(u *url.URL, development bool) (string, string, error) {
	if u == nil || u.Host == "" || u.Opaque != "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Scheme != "https" && !(development && u.Scheme == "http")) {
		return "", "", ErrNotAllowed
	}
	host := strings.ToLower(u.Hostname())
	if host == "" || strings.ContainsAny(host, "%\\ /\t\r\n") {
		return "", "", ErrNotAllowed
	}
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return "", "", ErrNotAllowed
	}
	address := net.JoinHostPort(host, strconv.Itoa(number))
	return u.Scheme + "://" + address, address, nil
}
func (f *Fetcher) check(u *url.URL) error {
	origin, _, err := sourceIdentity(u, f.development)
	if err != nil || !f.origins[origin] {
		return ErrNotAllowed
	}
	return nil
}
func (f *Fetcher) dialSource(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, ErrNotAllowed
	}
	number, err := strconv.Atoi(port)
	if err != nil {
		return nil, ErrNotAllowed
	}
	port = strconv.Itoa(number)
	if !f.addresses[net.JoinHostPort(strings.ToLower(host), port)] {
		return nil, ErrNotAllowed
	}
	addresses, err := f.resolve(ctx, host)
	if err != nil || len(addresses) == 0 || len(addresses) > 8 {
		return nil, ErrUnavailable
	}
	literal, parseErr := netip.ParseAddr(host)
	allowLoopback := f.development && (strings.EqualFold(host, "localhost") || parseErr == nil && literal.IsLoopback())
	for _, ip := range addresses {
		ip = ip.Unmap()
		if !ip.IsValid() || ip.Zone() != "" || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip == blockedMetadataIPv4 || ip == blockedMetadataIPv6 || ip.IsLoopback() && !allowLoopback || !ip.IsGlobalUnicast() && !ip.IsLoopback() {
			return nil, ErrNotAllowed
		}
	}
	for _, ip := range addresses {
		connection, e := f.dial(ctx, network, net.JoinHostPort(ip.Unmap().String(), port))
		if e == nil {
			return connection, nil
		}
		if ctx.Err() != nil {
			return nil, ErrUnavailable
		}
	}
	return nil, ErrUnavailable
}
func (f *Fetcher) Open(ctx context.Context, source string) (io.ReadCloser, error) {
	if f == nil {
		return nil, ErrUnavailable
	}
	u, err := url.Parse(source)
	if err != nil || f.check(u) != nil {
		return nil, ErrNotAllowed
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, ErrNotAllowed
	}
	request.Header.Set("User-Agent", "Keel media source fetcher")
	response, err := f.client.Do(request)
	if err != nil {
		if errors.Is(err, ErrNotAllowed) {
			return nil, ErrNotAllowed
		}
		return nil, ErrUnavailable
	}
	if response.StatusCode != http.StatusOK || response.ContentLength > 1<<30 || response.Header.Get("Content-Encoding") != "" && response.Header.Get("Content-Encoding") != "identity" {
		response.Body.Close()
		return nil, ErrUnavailable
	}
	return response.Body, nil
}

// ValidateReference rejects credentials, query strings and fragments before the
// URL enters immutable registry metadata; actual access still needs an allowlist.
func ValidateReference(source string) error {
	u, err := url.Parse(source)
	if err != nil {
		return ErrNotAllowed
	}
	_, _, err = sourceIdentity(u, true)
	return err
}
