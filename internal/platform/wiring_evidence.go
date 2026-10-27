package platform

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/enzoOvOyu/keel/internal/fabricboot"
	"github.com/enzoOvOyu/keel/internal/netbaseline"
	"github.com/enzoOvOyu/keel/internal/wiring"
)

type WiringCaptureRecord struct {
	CommandID   string        `json:"command_id"`
	RawSHA256   string        `json:"raw_sha256"`
	RawSize     int64         `json:"raw_size"`
	CollectedAt *time.Time    `json:"collected_at"`
	StoppedAt   time.Time     `json:"stopped_at"`
	Evaluation  wiring.Result `json:"evaluation"`
}

// RecordWiringCapture is an execution-owned consumer; there is no upload route.
// It retains a stopped original archive BEFORE the common runner receipt. It
// cannot publish StepReport, G2, NP-3, mappings or an asset milestone by itself.
func (s *Store) RecordWiringCapture(ctx context.Context, binding wiring.ReadbackBinding, raw []byte) (*WiringCaptureRecord, error) {
	if !s.WiringEnabled || s.MediaObjects == nil || len(raw) == 0 || len(raw) > wiring.MaxCaptureBytes {
		return nil, wiring.ErrCaptureRejected
	}
	tx, e := s.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return nil, e
	}
	command, e := loadWiringCommandTx(ctx, tx, binding)
	if e != nil {
		tx.Rollback()
		return nil, e
	}
	var existingDigest string
	e = tx.QueryRowContext(ctx, `SELECT raw_sha256 FROM validation_wiring_capture_records WHERE command_id=$1`, binding.CommandID).Scan(&existingDigest)
	if e == nil {
		if existingDigest != wiring.Hash(raw) {
			tx.Rollback()
			return nil, apiError(409, "WIRING_EVIDENCE_CONFLICT", "Original command capture is immutable")
		}
		if e = tx.Commit(); e != nil {
			return nil, e
		}
		retained, _, e := s.ReadWiringCapture(ctx, binding)
		return retained, e
	}
	if !errors.Is(e, sql.ErrNoRows) {
		tx.Rollback()
		return nil, e
	}
	archive, evaluation, e := wiring.EvaluateArchive(raw, command.Expectation, binding, command.HostOrigin)
	if e != nil {
		tx.Rollback()
		return nil, e
	}
	if archive.Capture.AdvertisingStoppedAt.After(time.Now().UTC().Add(5 * time.Second)) {
		tx.Rollback()
		return nil, wiring.ErrCaptureRejected
	}
	if e = verifyWiringExchangesTx(ctx, tx, binding, archive); e != nil {
		tx.Rollback()
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	sha := wiring.Hash(raw)
	out := &WiringCaptureRecord{CommandID: binding.CommandID, RawSHA256: sha, RawSize: int64(len(raw)), StoppedAt: archive.Capture.AdvertisingStoppedAt, Evaluation: evaluation}
	if !archive.Capture.CollectedAt.IsZero() {
		at := archive.Capture.CollectedAt
		out.CollectedAt = &at
	}
	key := "validation/wiring/" + binding.ProjectID + "/" + command.AssetID + "/" + binding.CommandID + "/" + sha + ".json"
	version, e := s.MediaObjects.Put(ctx, key, bytes.NewReader(raw), int64(len(raw)))
	if e != nil || version == "" || version == "null" {
		return nil, apiError(409, "WIRING_RAW_UNAVAILABLE", "Versioned original wiring archive could not be retained")
	}
	if _, e = s.readWiringRaw(ctx, key, version, sha, int64(len(raw))); e != nil {
		return nil, e
	}
	tx, e = s.DB.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = lockKey(ctx, tx, "wiring-command:"+binding.CommandID); e != nil {
		return nil, e
	}
	if _, e = loadWiringCommandTx(ctx, tx, binding); e != nil {
		return nil, e
	}
	var prior string
	e = tx.QueryRowContext(ctx, `SELECT raw_sha256 FROM validation_wiring_capture_records WHERE command_id=$1`, binding.CommandID).Scan(&prior)
	if e == nil {
		if prior != sha {
			return nil, apiError(409, "WIRING_EVIDENCE_CONFLICT", "The original command already has a different immutable capture")
		}
		return out, tx.Commit()
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return nil, e
	}
	if e = verifyWiringExchangesTx(ctx, tx, binding, archive); e != nil {
		return nil, e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO validation_wiring_capture_records(command_id,origin,collected_at,stopped_at,raw_key,raw_version,raw_sha256,raw_size,evaluation,received_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,statement_timestamp())`, binding.CommandID, archive.Capture.Origin, out.CollectedAt, out.StoppedAt, key, version, sha, len(raw), networkJSON(evaluation))
	if e != nil {
		return nil, e
	}
	return out, tx.Commit()
}

func verifyWiringExchangesTx(ctx context.Context, tx *sql.Tx, binding wiring.ReadbackBinding, archive *wiring.Archive) error {
	// Even a partial host archive cannot hide a collector session still running.
	var unknown bool
	e := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM validation_wiring_readback_dispatches d WHERE d.command_id=$1 AND NOT EXISTS(SELECT 1 FROM validation_wiring_readback_results r WHERE r.child_command_id=d.child_command_id) AND NOT EXISTS(SELECT 1 FROM validation_wiring_readback_queries q WHERE q.child_command_id=d.child_command_id AND q.observation->>'state' IN('completed','stopped','rejected')))`, binding.CommandID).Scan(&unknown)
	if e != nil {
		return e
	}
	if unknown {
		return wiring.ErrOutcomeUnknown
	}
	if archive.Readback == nil {
		return nil
	}
	var count int
	if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM validation_wiring_readback_dispatches WHERE command_id=$1`, binding.CommandID).Scan(&count); e != nil {
		return e
	}
	if count != len(archive.Readback.Exchanges) {
		return wiring.ErrCaptureRejected
	}
	for _, exchange := range archive.Readback.Exchanges {
		var requestRaw, responseRaw []byte
		var requestHash, responseHash string
		e = tx.QueryRowContext(ctx, `SELECT d.request,d.content_hash,r.response,r.content_hash FROM validation_wiring_readback_dispatches d JOIN validation_wiring_readback_results r ON r.child_command_id=d.child_command_id WHERE d.command_id=$1 AND d.child_command_id=$2`, binding.CommandID, exchange.Request.CommandID).Scan(&requestRaw, &requestHash, &responseRaw, &responseHash)
		if e != nil {
			return e
		}
		var originalRequest fabricboot.CollectorRecollectRequest
		var originalResponse fabricboot.CollectorRecollectResponse
		if json.Unmarshal(requestRaw, &originalRequest) != nil || json.Unmarshal(responseRaw, &originalResponse) != nil || netbaseline.ContentHash(originalRequest) != requestHash || netbaseline.ContentHash(originalResponse) != responseHash || string(networkJSON(originalRequest)) != string(networkJSON(exchange.Request)) || string(networkJSON(originalResponse)) != string(networkJSON(exchange.Response)) {
			return wiring.ErrCaptureRejected
		}
	}
	return nil
}
func (s *Store) readWiringRaw(ctx context.Context, key, version, sha string, size int64) ([]byte, error) {
	if s.MediaObjects == nil || version == "" || version == "null" || size < 1 || size > wiring.MaxCaptureBytes {
		return nil, wiring.ErrCaptureRejected
	}
	reader, e := s.MediaObjects.Open(ctx, key, version)
	if e != nil {
		return nil, apiError(409, "WIRING_RAW_UNAVAILABLE", "The retained original archive is unavailable")
	}
	raw, readErr := io.ReadAll(io.LimitReader(reader, wiring.MaxCaptureBytes+1))
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || int64(len(raw)) != size || wiring.Hash(raw) != sha {
		return nil, apiError(409, "WIRING_RAW_CORRUPT", "The retained archive differs from its original version and digest")
	}
	return raw, nil
}

// ReadWiringCapture is for the original execution owner/reconciler. A public
// caller must first authorize project/asset scope; no arbitrary object key is
// accepted. Re-read pinned bytes and recompute, rather than trusting stored PASS.
func (s *Store) ReadWiringCapture(ctx context.Context, binding wiring.ReadbackBinding) (*WiringCaptureRecord, *wiring.Archive, error) {
	tx, e := s.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return nil, nil, e
	}
	defer tx.Rollback()
	command, e := loadWiringCommandTx(ctx, tx, binding)
	if e != nil {
		return nil, nil, e
	}
	var key, version string
	var evaluationRaw []byte
	out := &WiringCaptureRecord{CommandID: binding.CommandID}
	var collected sql.NullTime
	e = tx.QueryRowContext(ctx, `SELECT raw_key,raw_version,raw_sha256,raw_size,collected_at,stopped_at,evaluation FROM validation_wiring_capture_records WHERE command_id=$1`, binding.CommandID).Scan(&key, &version, &out.RawSHA256, &out.RawSize, &collected, &out.StoppedAt, &evaluationRaw)
	if e != nil {
		return nil, nil, e
	}
	if key != "validation/wiring/"+binding.ProjectID+"/"+command.AssetID+"/"+binding.CommandID+"/"+out.RawSHA256+".json" {
		return nil, nil, wiring.ErrCaptureRejected
	}
	if collected.Valid {
		out.CollectedAt = &collected.Time
	}
	if e = tx.Commit(); e != nil {
		return nil, nil, e
	}
	raw, e := s.readWiringRaw(ctx, key, version, out.RawSHA256, out.RawSize)
	if e != nil {
		return nil, nil, e
	}
	archive, evaluation, e := wiring.EvaluateArchive(raw, command.Expectation, binding, command.HostOrigin)
	if e != nil {
		return nil, nil, e
	}
	var retained wiring.Result
	if json.Unmarshal(evaluationRaw, &retained) != nil || string(networkJSON(retained)) != string(networkJSON(evaluation)) || !out.StoppedAt.Equal(archive.Capture.AdvertisingStoppedAt) || (out.CollectedAt == nil) != archive.Capture.CollectedAt.IsZero() || out.CollectedAt != nil && !out.CollectedAt.Equal(archive.Capture.CollectedAt) {
		return nil, nil, wiring.ErrCaptureRejected
	}
	tx, e = s.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return nil, nil, e
	}
	defer tx.Rollback()
	if e = verifyWiringExchangesTx(ctx, tx, binding, archive); e != nil {
		return nil, nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, nil, e
	}
	out.Evaluation = evaluation
	return out, archive, nil
}
