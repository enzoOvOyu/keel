package provisionboot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

type deliveryFixture struct {
	*authorityFixture
	recorded   []FileDelivery
	failRecord bool
}

func (a *deliveryFixture) RecordFileServed(ctx context.Context, s Scope, g Grant, peer netip.Addr, in FileDelivery) error {
	if ctx.Err() != nil || g != a.grant || !s.CheckPeer(peer) {
		return ErrDenied
	}
	a.recorded = append(a.recorded, in)
	if a.failRecord {
		return ErrDenied
	}
	return nil
}

type deliveryResponse struct {
	*httptest.ResponseRecorder
	afterWrite func()
	short      bool
	fail       bool
}

func (w *deliveryResponse) Write(p []byte) (int, error) {
	if w.fail {
		return 0, io.ErrClosedPipe
	}
	if w.short {
		n, _ := w.ResponseRecorder.Write(p[:len(p)/2])
		return n, nil
	}
	n, err := w.ResponseRecorder.Write(p)
	if w.afterWrite != nil {
		w.afterWrite()
	}
	return n, err
}

func TestFileDeliveryRecordsOnlyFullAuthorizedServerWrite(t *testing.T) {
	for _, mode := range []string{"complete", "head", "range", "all_bytes_range", "not_modified", "bad_hash", "short_write", "write_failure", "revoke_after_write", "cancel_after_write", "evidence_unavailable"} {
		t.Run(mode, func(t *testing.T) {
			scope, base := fixture()
			a := &deliveryFixture{authorityFixture: base}
			method := http.MethodGet
			if mode == "head" {
				method = http.MethodHead
			}
			req := httptest.NewRequest(method, scope.Origin+"/boot/"+a.grant.ID+"/rootfs", nil)
			req.RemoteAddr = "192.0.2.22:43210"
			ctx, cancel := context.WithCancel(req.Context())
			defer cancel()
			req = req.WithContext(ctx)
			w := &deliveryResponse{ResponseRecorder: httptest.NewRecorder()}
			switch mode {
			case "range":
				req.Header.Set("Range", "bytes=0-2")
			case "all_bytes_range":
				req.Header.Set("Range", "bytes=0-")
			case "not_modified":
				sum := sha256.Sum256(a.data)
				req.Header.Set("If-None-Match", `"sha256:`+hex.EncodeToString(sum[:])+`"`)
			case "bad_hash":
				a.corrupt = true
			case "short_write":
				w.short = true
			case "write_failure":
				w.fail = true
			case "revoke_after_write":
				w.afterWrite = func() { a.mu.Lock(); a.denied = true; a.mu.Unlock() }
			case "cancel_after_write":
				w.afterWrite = cancel
			case "evidence_unavailable":
				a.failRecord = true
			}
			(&Gateway{Scope: scope, Authority: a}).ServeHTTP(w, req)
			want := mode == "complete" || mode == "evidence_unavailable"
			if (len(a.recorded) == 1) != want {
				t.Fatal("unproven response created delivery evidence", mode, w.Code, len(a.recorded))
			}
			if want {
				sum := sha256.Sum256(a.data)
				got := a.recorded[0]
				if w.Code != http.StatusOK || got.Resource != "rootfs" || got.Bytes != int64(len(a.data)) || got.SHA256 != hex.EncodeToString(sum[:]) || got.ObjectVersion != "immutable-fixture-v1" || w.Body.String() != string(a.data) {
					t.Fatal("response and delivery identity diverged")
				}
			}
		})
	}
}

func TestFileDeliveryCountsUnderlyingWriterWithoutReaderFromBypass(t *testing.T) {
	recorder := httptest.NewRecorder()
	wrapped := &fileResponse{ResponseWriter: recorder}
	if _, ok := any(wrapped).(io.ReaderFrom); ok {
		t.Fatal("copy can bypass evidence byte counting")
	}
	if wrapped.Unwrap() != recorder {
		t.Fatal("response controller cannot reach underlying writer")
	}
	wrapped.WriteHeader(http.StatusPartialContent)
	if _, err := wrapped.Write([]byte("partial")); err != nil {
		t.Fatal(err)
	}
	if wrapped.status != http.StatusPartialContent || wrapped.bytes != 7 || wrapped.failed {
		t.Fatal("partial status was lost")
	}
	failing := &fileResponse{ResponseWriter: &deliveryResponse{ResponseRecorder: httptest.NewRecorder(), fail: true}}
	if _, err := failing.Write([]byte("body")); !errors.Is(err, io.ErrClosedPipe) || !failing.failed {
		t.Fatal("failed write was hidden")
	}
}
