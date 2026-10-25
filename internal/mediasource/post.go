package mediasource

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"time"
)

// CheckURL applies the configured origin policy without making a request.
func (f *Fetcher) CheckURL(source string) error {
	if f == nil {
		return ErrNotAllowed
	}
	u, err := url.Parse(source)
	if err != nil {
		return ErrNotAllowed
	}
	return f.check(u)
}

// PostJSON reuses DNS/dial pinning, metadata exclusions and proxy isolation.
// Notifications never follow redirects or retain a receiver's response body.
func (f *Fetcher) PostJSON(ctx context.Context, target string, headers http.Header, body []byte) (int, error) {
	if len(body) > 64<<10 || f.CheckURL(target) != nil {
		return 0, ErrNotAllowed
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return 0, ErrNotAllowed
	}
	request.Header = headers.Clone()
	if request.Header == nil {
		request.Header = make(http.Header)
	}
	request.Header.Set("Content-Type", "application/cloudevents+json")
	request.Header.Set("User-Agent", "Keel event webhook")
	client := *f.client
	client.Timeout = 8 * time.Second
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(request)
	if err != nil {
		return 0, ErrUnavailable
	}
	defer response.Body.Close()
	// The acknowledgement is its status, not untrusted receiver content.
	io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	return response.StatusCode, nil
}
