package provisionboot

import (
	"context"
	"net/http"
	"net/netip"
)

// FileDelivery describes bytes fully accepted by the server ResponseWriter.
// It is not proof that a client received, verified, booted or executed them.
type FileDelivery struct {
	Resource      string
	SHA256        string
	Bytes         int64
	ObjectVersion string
}

// FileDeliveryAuthority is an optional control-plane evidence sink, called only
// after a complete verified GET and a final live authorization check. Recording
// must recheck the frozen identity, current authority and exclusive lease.
type FileDeliveryAuthority interface {
	RecordFileServed(context.Context, Scope, Grant, netip.Addr, FileDelivery) error
}

// Deliberately do not expose ReaderFrom: ServeContent's copy must pass through
// Write so an optimized underlying ResponseWriter cannot bypass byte counting.
type fileResponse struct {
	http.ResponseWriter
	status int
	bytes  int64
	failed bool
}

func (w *fileResponse) WriteHeader(status int) {
	if status >= 100 && status < 200 {
		w.ResponseWriter.WriteHeader(status)
		return
	}
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *fileResponse) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(p)
	if err != nil || n != len(p) {
		w.failed = true
	}
	w.bytes += int64(n)
	return n, err
}

func (w *fileResponse) Unwrap() http.ResponseWriter { return w.ResponseWriter }
