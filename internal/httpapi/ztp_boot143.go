package httpapi

import (
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/enzoOvOyu/keel/internal/fabricboot"
	"github.com/enzoOvOyu/keel/internal/platform"
)

const sztpRPCPath = "/restconf/operations/ietf-sztp-bootstrap-server:get-bootstrapping-data"

// NewSZTPHandler is mounted only on the dedicated TLS listener. It deliberately
// exposes no browser session, proxy identity header, or application API route.
func NewSZTPHandler(store *platform.Store, manufacturerRoots *x509.CertPool) http.Handler {
	if store == nil || manufacturerRoots == nil || len(manufacturerRoots.Subjects()) == 0 {
		panic("SZTP requires store and manufacturer trust roots")
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.URL.Path != sztpRPCPath {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.URL.RawQuery != "" {
			http.Error(w, "query parameters are not accepted", http.StatusBadRequest)
			return
		}
		serial, err := fabricboot.VerifyIDevID(r.TLS, manufacturerRoots, time.Now().UTC())
		if err != nil {
			http.Error(w, "device certificate rejected", http.StatusUnauthorized)
			return
		}
		peer, ok := directBootPeer(r.RemoteAddr)
		if !ok {
			http.NotFound(w, r)
			return
		}
		signed, err := parseSZTPRequest(r)
		if err != nil {
			http.Error(w, "invalid SZTP request", http.StatusBadRequest)
			return
		}
		var id [16]byte
		if _, err = rand.Read(id[:]); err != nil {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		trace := hex.EncodeToString(id[:])
		result, err := store.ServeZTP143(r.Context(), serial, peer, r.Host, trace, signed)
		if err != nil {
			var pe *platform.Error
			if errors.As(err, &pe) {
				http.Error(w, pe.Message, pe.Status)
				return
			}
			http.Error(w, "SZTP service unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/yang-data+json")
		w.Header().Set("X-Request-ID", trace)
		_ = json.NewEncoder(w).Encode(map[string]any{"ietf-sztp-bootstrap-server:output": map[string]string{"conveyed-information": base64.StdEncoding.EncodeToString(result.CMS)}})
	})
}

// RFC 7951 represents a YANG empty leaf as [null]. Unknown input keys are
// rejected so malformed/surprising device claims cannot silently affect data.
func parseSZTPRequest(r *http.Request) (bool, error) {
	if r.Body == nil {
		return false, nil
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 4097))
	if err != nil || len(raw) > 4096 {
		return false, io.ErrUnexpectedEOF
	}
	if len(raw) == 0 {
		return false, nil
	}
	if media := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0])); media != "application/yang-data+json" {
		return false, io.ErrUnexpectedEOF
	}
	var envelope map[string]json.RawMessage
	if err = json.Unmarshal(raw, &envelope); err != nil || len(envelope) != 1 {
		return false, io.ErrUnexpectedEOF
	}
	inputRaw, ok := envelope["ietf-sztp-bootstrap-server:input"]
	if !ok {
		return false, io.ErrUnexpectedEOF
	}
	var input map[string]json.RawMessage
	if err = json.Unmarshal(inputRaw, &input); err != nil {
		return false, err
	}
	signed := false
	for key, value := range input {
		switch key {
		case "signed-data-preferred":
			if string(value) != "[null]" {
				return false, io.ErrUnexpectedEOF
			}
			signed = true
		case "hw-model", "os-name", "os-version", "nonce":
			// Device metadata is intentionally ignored for policy selection.
			var v string
			if err = json.Unmarshal(value, &v); err != nil || len(v) > 256 {
				return false, io.ErrUnexpectedEOF
			}
		default:
			return false, io.ErrUnexpectedEOF
		}
	}
	return signed, nil
}
