package bmc

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

// Secrets have no JSON representation and are resolved only after execution
// admission by the owner of the frozen credential batch/version.
type Credentials struct {
	Username  string    `json:"-"`
	Password  string    `json:"-"`
	ExpiresAt time.Time `json:"-"`
	BatchRef  string    `json:"-"`
	Version   string    `json:"-"`
}
type CredentialProvider interface {
	Resolve(context.Context, Target) (Credentials, error)
}

var ErrCredentialExpired = errors.New("BMC credential version expired")

type Client struct {
	root        *url.URL
	http        *http.Client
	credentials CredentialProvider
	target      Target
	origin      string
}

func NewClient(endpoint string, transport *http.Transport, timeout time.Duration, provider CredentialProvider, target Target, origin string) (*Client, error) {
	root, e := url.Parse(endpoint)
	if e != nil || root.Scheme != "https" || root.Hostname() == "" || root.User != nil || root.Opaque != "" || root.RawQuery != "" || root.Fragment != "" || (root.Path != "" && root.Path != "/") || root.RawPath != "" || provider == nil || transport == nil || timeout < time.Second || timeout > time.Minute {
		return nil, errors.New("BMC inventory requires a fixed HTTPS origin, trusted transport and bounded timeout")
	}
	if e = target.Validate(); e != nil {
		return nil, e
	}
	if origin != "sim" && origin != "real" {
		return nil, errors.New("explicit BMC evidence origin is required")
	}
	t := transport.Clone()
	if t.TLSClientConfig == nil {
		t.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	} else {
		t.TLSClientConfig = t.TLSClientConfig.Clone()
		if t.TLSClientConfig.InsecureSkipVerify {
			return nil, errors.New("BMC TLS verification cannot be disabled")
		}
		if t.TLSClientConfig.MinVersion < tls.VersionTLS12 {
			t.TLSClientConfig.MinVersion = tls.VersionTLS12
		}
	}
	// Never honor ambient HTTP(S)_PROXY for an authenticated BMC connection.
	t.Proxy = nil
	return &Client{root: root, http: &http.Client{Transport: t, Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("BMC redirects are not permitted") }}, credentials: provider, target: target, origin: origin}, nil
}
func (c *Client) Origin() string { return c.origin }
func (c *Client) Close() error   { c.http.CloseIdleConnections(); return nil }

// CanonicalPath bounds navigation to the service tree. Next links may retain
// their opaque query; cross-origin links, encoded separators and dot segments
// never receive credentials. Reference: DMTF DSP0266, service root and next link.
func CanonicalPath(parent, reference string) (string, error) {
	if len(reference) == 0 || len(reference) > 2048 {
		return "", errors.New("invalid Redfish resource reference")
	}
	base, e := url.Parse("https://bmc.invalid" + parent)
	if e != nil {
		return "", e
	}
	link, e := url.Parse(reference)
	if e != nil || link.User != nil || link.Opaque != "" || link.Fragment != "" {
		return "", errors.New("invalid Redfish resource reference")
	}
	if link.IsAbs() || link.Host != "" {
		return "", errors.New("absolute links require the configured-origin check")
	}
	// Check the supplied path before resolution, which otherwise normalizes '..'.
	if link.RawPath != "" || strings.Contains(link.Path, "\\") || strings.ContainsRune(link.Path, 0) || strings.ContainsAny(reference, "\r\n") {
		return "", errors.New("encoded or invalid Redfish resource path")
	}
	for _, part := range strings.Split(link.Path, "/") {
		if part == "." || part == ".." {
			return "", errors.New("Redfish dot segments are not permitted")
		}
	}
	query, e := url.ParseQuery(link.RawQuery)
	if e != nil {
		return "", errors.New("invalid Redfish paging query")
	}
	for key, values := range query {
		if key != "$skip" && key != "$skiptoken" && key != "$top" {
			return "", errors.New("only bounded Redfish paging queries are permitted")
		}
		if len(values) != 1 || len(values[0]) > 512 {
			return "", errors.New("invalid Redfish paging query")
		}
	}
	out := base.ResolveReference(link)
	clean := path.Clean(out.Path)
	if clean != "/redfish/v1" && !strings.HasPrefix(clean, "/redfish/v1/") {
		return "", errors.New("Redfish link leaves the service tree")
	}
	if out.Path != clean && out.Path != clean+"/" {
		return "", errors.New("noncanonical Redfish resource path")
	}
	if out.RawQuery != "" {
		return out.Path + "?" + out.RawQuery, nil
	}
	return out.Path, nil
}
func (c *Client) Resolve(parent, reference string) (string, error) {
	link, e := url.Parse(reference)
	if e != nil || link.Fragment != "" || link.Opaque != "" || link.User != nil {
		return "", errors.New("invalid Redfish resource reference")
	}
	if link.IsAbs() || link.Host != "" {
		if link.Scheme != c.root.Scheme || !strings.EqualFold(link.Host, c.root.Host) || link.User != nil {
			return "", errors.New("cross-origin Redfish resource reference")
		}
		reference = link.RequestURI()
	}
	return CanonicalPath(parent, reference)
}
func (c *Client) Read(ctx context.Context, reference string) (Document, error) {
	var empty Document
	resource, e := c.Resolve("/redfish/v1/", reference)
	if e != nil {
		return empty, failure("", "partial_failure", "invalid_resource_link", false)
	}
	credentials, e := c.credentials.Resolve(ctx, c.target)
	if e != nil {
		if errors.Is(e, ErrCredentialExpired) {
			return empty, failure(resource, "authentication_failed", "credential_expired", true)
		}
		return empty, failure(resource, "authentication_failed", "credential_unavailable", false)
	}
	if credentials.BatchRef != c.target.CredentialBatchRef || credentials.Version != c.target.CredentialVersion {
		return empty, failure(resource, "authentication_failed", "credential_identity_mismatch", false)
	}
	if credentials.Username == "" || credentials.Password == "" || credentials.ExpiresAt.IsZero() || !time.Now().Before(credentials.ExpiresAt) {
		return empty, failure(resource, "authentication_failed", "credential_expired", true)
	}
	destination := *c.root
	parsed, _ := url.Parse(resource)
	destination.Path = parsed.Path
	destination.RawPath = ""
	destination.RawQuery = parsed.RawQuery
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, destination.String(), nil)
	if e != nil {
		return empty, failure(resource, "partial_failure", "invalid_resource_request", false)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("OData-Version", "4.0")
	req.SetBasicAuth(credentials.Username, credentials.Password)
	response, e := c.http.Do(req)
	if e != nil {
		var certificate *tls.CertificateVerificationError
		if errors.As(e, &certificate) {
			return empty, failure(resource, "partial_failure", "transport_or_tls_failed", false)
		}
		var timeout net.Error
		if errors.Is(e, context.DeadlineExceeded) || errors.As(e, &timeout) && timeout.Timeout() {
			return empty, failure(resource, "timeout", "request_timeout", true)
		}
		if errors.Is(e, context.Canceled) {
			return empty, failure(resource, "partial_failure", "cancelled", false)
		}
		var connection *net.OpError
		if errors.As(e, &connection) && connection.Op == "dial" {
			return empty, failure(resource, "partial_failure", "transport_unavailable", true)
		}
		return empty, failure(resource, "partial_failure", "transport_or_tls_failed", true)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		f := failure(resource, "partial_failure", "http_response_failed", response.StatusCode == 429 || response.StatusCode >= 500)
		f.HTTPStatus = response.StatusCode
		switch response.StatusCode {
		case 401, 403:
			f.Class = "authentication_failed"
			f.Code = "authentication_rejected"
			f.Retryable = false
		case 404, 405, 501:
			f.Class = "version_unsupported"
			f.Code = "resource_unsupported"
			f.Retryable = false
		}
		return empty, f
	}
	body, e := io.ReadAll(io.LimitReader(response.Body, MaxResourceBytes+1))
	if e != nil {
		return empty, failure(resource, "partial_failure", "response_read_failed", true)
	}
	if len(body) > MaxResourceBytes {
		return empty, failure(resource, "partial_failure", "resource_size_limit", false)
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var object map[string]any
	if decoder.Decode(&object) != nil || object == nil {
		return empty, failure(resource, "partial_failure", "invalid_json_resource", false)
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return empty, failure(resource, "partial_failure", "trailing_json_resource", false)
	}
	return Document{Path: resource, Body: body, ReceivedAt: time.Now().UTC()}, nil
}
