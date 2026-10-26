//go:build linux

package provisioninitrd

import (
	"context"
	"io"
	"net/http"
	"time"
)

// ArtifactSource exposes one exact frozen role, never a caller-selected URL,
// filesystem path, Range, redirect or automatically retried transfer.
type ArtifactSource interface {
	CopyArtifact(context.Context, string, int64, io.Writer) error
}

func (h *HTTPFiles) CopyArtifact(ctx context.Context, name string, size int64, output io.Writer) error {
	if h == nil || h.client == nil || name != "rootfs" || size < 96 || size > MaxStagedRootFSBytes || output == nil || ctx.Err() != nil {
		return ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.base+"rootfs", nil)
	if err != nil {
		return ErrUnavailable
	}
	req.Header.Set("Accept-Encoding", "identity")
	// Reuse the fixed-IP/interface/TLS transport and redirect refusal, with an
	// explicit large-object deadline instead of the small-file 30-second limit.
	client := *h.client
	client.Timeout = 5 * time.Minute
	response, err := client.Do(req)
	if err != nil {
		return ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.ContentLength != size || response.Header.Get("Content-Encoding") != "" || response.Header.Get("Content-Range") != "" {
		return ErrUnavailable
	}
	n, err := io.CopyBuffer(output, io.LimitReader(response.Body, size+1), make([]byte, 128<<10))
	if err != nil || n != size || ctx.Err() != nil {
		return ErrUnavailable
	}
	return nil
}
