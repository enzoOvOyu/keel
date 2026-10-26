package provisionpreflight

import (
	"bytes"
	"encoding/json"
	"io"
	"time"
	"unicode/utf8"
)

// EvaluateReport is a data verifier, not an authenticated callback handler.
// Callers must first authenticate a one-time scoped callback and verify its live
// execution attempt, command, fence and exclusive machine lease. A report never
// authorizes installation by itself, even if every check passes.
func EvaluateReport(expected Spec, raw []byte, now time.Time) (*Report, error) {
	if expected.Validate(now) != nil || len(raw) > 2<<20 || !utf8.Valid(raw) || uniqueSpecKeys(raw) != nil {
		return nil, ErrInvalid
	}
	var supplied Report
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&supplied) != nil {
		return nil, ErrInvalid
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF || supplied.Schema != Schema {
		return nil, ErrInvalid
	}
	// Compare the entire frozen expectation, not only request ID or host serial.
	// Marshal normalized UTC times so equivalent timestamp offsets are harmless.
	expected.ExpiresAt = expected.ExpiresAt.UTC()
	supplied.Spec.ExpiresAt = supplied.Spec.ExpiresAt.UTC()
	approved, _ := json.Marshal(expected)
	observed, _ := json.Marshal(supplied.Spec)
	if !bytes.Equal(approved, observed) {
		return nil, ErrInvalid
	}
	computed, err := Evaluate(expected, supplied.Observation, now)
	if err != nil {
		return nil, err
	}
	checks, _ := json.Marshal(computed.Checks)
	claims, _ := json.Marshal(supplied.Checks)
	if computed.Status != supplied.Status || !bytes.Equal(checks, claims) {
		return nil, ErrInvalid
	}
	return computed, nil
}
