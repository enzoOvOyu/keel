package httpapi

import (
	"crypto/rand"
	"crypto/sha256"
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
const sztpProgressPath = "/restconf/operations/ietf-sztp-bootstrap-server:report-progress"

// NewSZTPHandler is mounted only on the dedicated TLS listener. It deliberately
// exposes no browser session, proxy identity header, or application API route.
func NewSZTPHandler(store *platform.Store, manufacturerRoots *x509.CertPool) http.Handler {
	if store == nil || manufacturerRoots == nil || len(manufacturerRoots.Subjects()) == 0 {
		panic("SZTP requires store and manufacturer trust roots")
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.URL.Path != sztpRPCPath && r.URL.Path != sztpProgressPath {
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
		var id [16]byte
		if _, err = rand.Read(id[:]); err != nil {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		trace := hex.EncodeToString(id[:])
		if r.URL.Path == sztpProgressPath {
			progress, parseErr := parseSZTPProgress(r)
			if parseErr != nil {
				http.Error(w, "invalid SZTP progress report", http.StatusBadRequest)
				return
			}
			if reportErr := store.RecordZTP143Progress(r.Context(), serial, peer, r.Host, trace, progress); reportErr != nil {
				var pe *platform.Error
				if errors.As(reportErr, &pe) {
					http.Error(w, pe.Message, pe.Status)
				} else if errors.Is(reportErr, platform.ErrNotFound) {
					http.NotFound(w, r)
				} else {
					http.Error(w, "SZTP service unavailable", http.StatusServiceUnavailable)
				}
				return
			}
			w.Header().Set("X-Request-ID", trace)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		signed, err := parseSZTPRequest(r)
		if err != nil {
			http.Error(w, "invalid SZTP request", http.StatusBadRequest)
			return
		}
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

// parseSZTPProgress accepts RFC 8572's JSON-encoded report-progress input.
// Public keys and certificates are validated but only a payload digest is
// persisted; the raw data is not copied into logs or database rows.
func parseSZTPProgress(r *http.Request) (platform.SZTPProgress, error) {
	var out platform.SZTPProgress
	if r.Body == nil || strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0])) != "application/yang-data+json" {
		return out, io.ErrUnexpectedEOF
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 16385))
	if err != nil || len(raw) == 0 || len(raw) > 16384 {
		return out, io.ErrUnexpectedEOF
	}
	var envelope map[string]json.RawMessage
	if err = json.Unmarshal(raw, &envelope); err != nil || len(envelope) != 1 {
		return out, io.ErrUnexpectedEOF
	}
	inputRaw, ok := envelope["ietf-sztp-bootstrap-server:input"]
	if !ok {
		return out, io.ErrUnexpectedEOF
	}
	var input struct {
		ProgressType string `json:"progress-type"`
		Message      string `json:"message"`
		SSHHostKeys  *struct {
			Keys []struct {
				Algorithm string `json:"algorithm"`
				KeyData   string `json:"key-data"`
			} `json:"ssh-host-key"`
		} `json:"ssh-host-keys"`
		TrustAnchorCerts *struct {
			Certs []string `json:"trust-anchor-cert"`
		} `json:"trust-anchor-certs"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(inputRaw)))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&input); err != nil || input.ProgressType == "" || len(input.Message) > 1000 || strings.ContainsRune(input.Message, '\x00') {
		return out, io.ErrUnexpectedEOF
	}
	if input.ProgressType != "bootstrap-complete" && (input.SSHHostKeys != nil || input.TrustAnchorCerts != nil) {
		return out, io.ErrUnexpectedEOF
	}
	if input.SSHHostKeys != nil {
		if len(input.SSHHostKeys.Keys) > 8 {
			return out, io.ErrUnexpectedEOF
		}
		for _, key := range input.SSHHostKeys.Keys {
			if len(key.Algorithm) == 0 || len(key.Algorithm) > 128 || len(key.KeyData) == 0 || len(key.KeyData) > 8192 {
				return out, io.ErrUnexpectedEOF
			}
			decoded, e := base64.StdEncoding.DecodeString(key.KeyData)
			if e != nil || len(decoded) == 0 || len(decoded) > 4096 {
				return out, io.ErrUnexpectedEOF
			}
		}
	}
	if input.TrustAnchorCerts != nil {
		if len(input.TrustAnchorCerts.Certs) > 8 {
			return out, io.ErrUnexpectedEOF
		}
		for _, value := range input.TrustAnchorCerts.Certs {
			decoded, e := base64.StdEncoding.DecodeString(value)
			if e != nil || len(decoded) == 0 || len(decoded) > 4096 {
				return out, io.ErrUnexpectedEOF
			}
		}
	}
	sum := sha256.Sum256(raw)
	return platform.SZTPProgress{Type: input.ProgressType, Message: input.Message, EvidenceSHA256: hex.EncodeToString(sum[:])}, nil
}
