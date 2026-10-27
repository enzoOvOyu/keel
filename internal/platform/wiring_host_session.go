package platform

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/enzoOvOyu/keel/internal/fabricboot"
	"github.com/enzoOvOyu/keel/internal/netbaseline"
	"github.com/enzoOvOyu/keel/internal/wiring"
)

// RunWiringCommand consumes an ALREADY registered common Execution command.
// It is not an admission endpoint or scheduler. Driver/client are server-owned
// installed adapters; no browser can supply either or an arbitrary command.
// A durable host-start intent precedes even Inspect. A second invocation can
// read retained proof, but never calls Begin again, including after a crash.
func (s *Store) RunWiringCommand(ctx context.Context, binding wiring.ReadbackBinding, driver wiring.Driver, client wiring.RecollectionClient) (*WiringCaptureRecord, error) {
	if !s.WiringEnabled || s.MediaObjects == nil || driver == nil || client == nil {
		return nil, wiring.ErrCaptureRejected
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = lockKey(ctx, tx, "wiring-command:"+binding.CommandID); e != nil {
		return nil, e
	}
	command, e := loadWiringCommandTx(ctx, tx, binding)
	if e != nil {
		return nil, e
	}
	if driver.Origin() != command.HostOrigin {
		return nil, wiring.ErrCaptureRejected
	}
	var exists bool
	if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM validation_wiring_capture_records WHERE command_id=$1)`, binding.CommandID).Scan(&exists); e != nil {
		return nil, e
	}
	if exists {
		if e = tx.Commit(); e != nil {
			return nil, e
		}
		record, _, e := s.ReadWiringCapture(ctx, binding)
		return record, e
	}
	if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM validation_wiring_host_dispatches WHERE command_id=$1)`, binding.CommandID).Scan(&exists); e != nil {
		return nil, e
	}
	if exists {
		return nil, wiring.ErrOutcomeUnknown
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO validation_wiring_host_dispatches(command_id,committed_at) VALUES($1,statement_timestamp())`, binding.CommandID)
	if e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, wiring.ErrOutcomeUnknown
	}
	archive, captureErr := wiring.CaptureWithReadback(ctx, command.Expectation, binding.CommandID, driver, wiring.NewRecollector(binding, client, &WiringReadbackLedger{Store: s}))
	finish, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if errors.Is(captureErr, wiring.ErrCaptureRejected) {
		// CaptureRejected is defined only before Begin; collector cannot have run.
		if e = s.recordWiringNotStarted(finish, binding); e != nil {
			return nil, wiring.ErrOutcomeUnknown
		}
		return nil, wiring.ErrCaptureRejected
	}
	if captureErr != nil {
		var unknown *wiring.UnknownCapture
		if errors.As(captureErr, &unknown) && unknown.HostStopped != nil {
			raw, e := json.Marshal(unknown.HostStopped)
			if e != nil {
				return nil, wiring.ErrOutcomeUnknown
			}
			if e = s.RecordWiringHostStopped(finish, binding, raw); e != nil {
				return nil, wiring.ErrOutcomeUnknown
			}
		}
		return nil, wiring.ErrOutcomeUnknown
	}
	if archive == nil {
		return nil, wiring.ErrOutcomeUnknown
	}
	raw, e := json.Marshal(archive)
	if e != nil {
		return nil, wiring.ErrOutcomeUnknown
	}
	if e = s.RecordWiringHostStopped(finish, binding, raw); e != nil {
		return nil, wiring.ErrOutcomeUnknown
	}
	record, e := s.RecordWiringCapture(finish, binding, raw)
	if e != nil {
		return nil, wiring.ErrOutcomeUnknown
	}
	return record, nil
}
func (s *Store) recordWiringNotStarted(ctx context.Context, binding wiring.ReadbackBinding) error {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = lockKey(ctx, tx, "wiring-command:"+binding.CommandID); e != nil {
		return e
	}
	if _, e = loadWiringCommandTx(ctx, tx, binding); e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO validation_wiring_host_proofs(command_id,outcome,received_at) VALUES($1,'local_not_started',statement_timestamp())`, binding.CommandID)
	if e != nil {
		return e
	}
	return tx.Commit()
}

// RecordWiringHostStopped retains ONLY the positive host-side stop/restoration
// proof, even if the switch session is still unknown. It never closes the task
// or releases any resource. Raw original host bytes remain independently pinned.
func (s *Store) RecordWiringHostStopped(ctx context.Context, binding wiring.ReadbackBinding, raw []byte) error {
	if !s.WiringEnabled || s.MediaObjects == nil {
		return wiring.ErrOutcomeUnknown
	}
	tx, e := s.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return e
	}
	command, e := loadWiringCommandTx(ctx, tx, binding)
	if e != nil {
		tx.Rollback()
		return e
	}
	archive, _, e := wiring.EvaluateArchive(raw, command.Expectation, binding, command.HostOrigin)
	if e != nil {
		tx.Rollback()
		return e
	}
	var committed time.Time
	if e = tx.QueryRowContext(ctx, `SELECT committed_at FROM validation_wiring_host_dispatches WHERE command_id=$1`, binding.CommandID).Scan(&committed); e != nil {
		tx.Rollback()
		return e
	}
	if archive.Capture.StartedAt.Before(committed) {
		tx.Rollback()
		return wiring.ErrCaptureRejected
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	sha := wiring.Hash(raw)
	hostHash, e := wiring.HostEvidenceHash(archive)
	if e != nil {
		return e
	}
	key := "validation/wiring-host/" + binding.ProjectID + "/" + command.AssetID + "/" + binding.CommandID + "/" + sha + ".json"
	version, e := s.MediaObjects.Put(ctx, key, bytes.NewReader(raw), int64(len(raw)))
	if e != nil || version == "" || version == "null" {
		return wiring.ErrOutcomeUnknown
	}
	if _, e = s.readWiringRaw(ctx, key, version, sha, int64(len(raw))); e != nil {
		return e
	}
	tx, e = s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = lockKey(ctx, tx, "wiring-command:"+binding.CommandID); e != nil {
		return e
	}
	var prior, outcome string
	e = tx.QueryRowContext(ctx, `SELECT coalesce(raw_sha256,''),outcome FROM validation_wiring_host_proofs WHERE command_id=$1`, binding.CommandID).Scan(&prior, &outcome)
	if e == nil {
		if prior != sha || outcome != "stopped" {
			return wiring.ErrCaptureRejected
		}
		return tx.Commit()
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO validation_wiring_host_proofs(command_id,outcome,stopped_at,host_evidence_hash,raw_key,raw_version,raw_sha256,raw_size,received_at) VALUES($1,'stopped',$2,$3,$4,$5,$6,$7,statement_timestamp())`, binding.CommandID, archive.Capture.AdvertisingStoppedAt, hostHash, key, version, sha, len(raw))
	if e != nil {
		return e
	}
	return tx.Commit()
}

func verifyWiringHostBindingTx(ctx context.Context, tx *sql.Tx, binding wiring.ReadbackBinding, archive *wiring.Archive) error {
	hostHash, e := wiring.HostEvidenceHash(archive)
	if e != nil {
		return e
	}
	var original string
	var stopped time.Time
	e = tx.QueryRowContext(ctx, `SELECT host_evidence_hash,stopped_at FROM validation_wiring_host_proofs WHERE command_id=$1 AND outcome='stopped'`, binding.CommandID).Scan(&original, &stopped)
	if e != nil {
		return e
	}
	if original != hostHash || !wiringStoredTimeEqual(stopped, archive.Capture.AdvertisingStoppedAt) {
		return wiring.ErrCaptureRejected
	}
	return nil
}

// ReadWiringHostStopped reverse-reads the independent original host artifact.
// It does not inspect a live host or infer stopped from absence of a process.
func (s *Store) ReadWiringHostStopped(ctx context.Context, binding wiring.ReadbackBinding) (*wiring.Archive, error) {
	tx, e := s.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	command, e := loadWiringCommandTx(ctx, tx, binding)
	if e != nil {
		return nil, e
	}
	var outcome string
	var stopped sql.NullTime
	var key, version, sha, hostHash sql.NullString
	var size sql.NullInt64
	e = tx.QueryRowContext(ctx, `SELECT outcome,stopped_at,raw_key,raw_version,raw_sha256,host_evidence_hash,raw_size FROM validation_wiring_host_proofs WHERE command_id=$1`, binding.CommandID).Scan(&outcome, &stopped, &key, &version, &sha, &hostHash, &size)
	if e != nil {
		return nil, e
	}
	if outcome == "local_not_started" {
		return nil, wiring.ErrCaptureRejected
	}
	if outcome != "stopped" || !stopped.Valid || !key.Valid || !version.Valid || !sha.Valid || !hostHash.Valid || !size.Valid || key.String != "validation/wiring-host/"+binding.ProjectID+"/"+command.AssetID+"/"+binding.CommandID+"/"+sha.String+".json" {
		return nil, wiring.ErrCaptureRejected
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	raw, e := s.readWiringRaw(ctx, key.String, version.String, sha.String, size.Int64)
	if e != nil {
		return nil, e
	}
	archive, _, e := wiring.EvaluateArchive(raw, command.Expectation, binding, command.HostOrigin)
	if e != nil {
		return nil, e
	}
	reconstructed, e := wiring.HostEvidenceHash(archive)
	if e != nil || reconstructed != hostHash.String || !wiringStoredTimeEqual(stopped.Time, archive.Capture.AdvertisingStoppedAt) {
		return nil, wiring.ErrCaptureRejected
	}
	return archive, nil
}

// QueryWiringCommand recovers evidence only. It never calls Driver, starts an
// interface or POSTs a collector request, and never records a common task receipt
// or releases quarantined resources. Operator-authorized Execution reconciliation
// must still consume the final retained record under its original fence.
func (s *Store) QueryWiringCommand(ctx context.Context, binding wiring.ReadbackBinding, client wiring.RecollectionClient) (*WiringCaptureRecord, error) {
	if !s.WiringEnabled || client == nil {
		return nil, wiring.ErrOutcomeUnknown
	}
	work, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	retained, _, retainedErr := s.ReadWiringCapture(work, binding)
	if retainedErr == nil {
		return retained, nil
	}
	if !errors.Is(retainedErr, sql.ErrNoRows) {
		return nil, retainedErr
	}
	host, e := s.ReadWiringHostStopped(work, binding)
	if e != nil {
		return nil, e
	}
	tx, e := s.DB.BeginTx(work, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return nil, e
	}
	command, e := loadWiringCommandTx(work, tx, binding)
	if e != nil {
		tx.Rollback()
		return nil, e
	}
	var owns bool
	e = tx.QueryRowContext(work, `SELECT EXISTS(SELECT 1 FROM exec_task_attempts a WHERE a.id=$1 AND a.command_id=$2 AND a.fencing_token=$3 AND ((a.state='running' AND a.lease_until>clock_timestamp()) OR a.state='unknown') AND keel_wiring_exclusive_scope(a.id,a.fencing_token,$4,$5,a.state='running'))`, binding.AttemptID, binding.CommandID, binding.FencingToken, command.Expectation.MachineID, networkJSON(command.Expectation.Ports)).Scan(&owns)
	if e != nil || !owns {
		tx.Rollback()
		return nil, wiring.ErrOutcomeUnknown
	}
	rows, e := tx.QueryContext(work, `SELECT request,content_hash FROM validation_wiring_readback_dispatches WHERE command_id=$1 ORDER BY child_command_id`, binding.CommandID)
	if e != nil {
		tx.Rollback()
		return nil, e
	}
	requests := []fabricboot.CollectorRecollectRequest{}
	for rows.Next() {
		var raw []byte
		var hash string
		if e = rows.Scan(&raw, &hash); e != nil {
			rows.Close()
			tx.Rollback()
			return nil, e
		}
		var request fabricboot.CollectorRecollectRequest
		if json.Unmarshal(raw, &request) != nil || request.Validate() != nil || netbaseline.ContentHash(request) != hash {
			rows.Close()
			tx.Rollback()
			return nil, wiring.ErrOutcomeUnknown
		}
		requests = append(requests, request)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		tx.Rollback()
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	allowed := map[string]string{}
	planned, planErr := wiring.BuildReadbackRequests(binding, wiring.ReadbackScope{PlanDigest: host.PlanDigest, CommandID: binding.CommandID, ScopeHash: command.Expectation.ScopeHash, AdvertisingAt: host.Capture.AdvertisingAt, Ports: command.Expectation.Ports})
	if planErr != nil && len(requests) > 0 {
		return nil, wiring.ErrOutcomeUnknown
	}
	for _, r := range planned {
		allowed[r.CommandID] = string(networkJSON(r))
	}
	exchanges := []wiring.ReadbackExchange{}
	ledger := &WiringReadbackLedger{Store: s}
	for _, request := range requests {
		if allowed[request.CommandID] != string(networkJSON(request)) {
			return nil, wiring.ErrOutcomeUnknown
		}
		var cached []byte
		var hash string
		var at time.Time
		e = s.DB.QueryRowContext(work, `SELECT r.response,r.content_hash,coalesce((q.observation->>'finished_at')::timestamptz,r.received_at) FROM validation_wiring_readback_results r LEFT JOIN validation_wiring_readback_queries q ON q.child_command_id=r.child_command_id WHERE r.child_command_id=$1`, request.CommandID).Scan(&cached, &hash, &at)
		if e == nil {
			var response fabricboot.CollectorRecollectResponse
			if json.Unmarshal(cached, &response) != nil || netbaseline.ContentHash(response) != hash || response.Validate(request, at) != nil {
				return nil, wiring.ErrOutcomeUnknown
			}
			exchanges = append(exchanges, wiring.ReadbackExchange{Request: request, Response: response})
			continue
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return nil, e
		}
		var observation fabricboot.CollectorRecollectObservation
		e = s.DB.QueryRowContext(work, `SELECT observation,content_hash FROM validation_wiring_readback_queries WHERE child_command_id=$1`, request.CommandID).Scan(&cached, &hash)
		if e == nil {
			if json.Unmarshal(cached, &observation) != nil || netbaseline.ContentHash(observation) != hash {
				return nil, wiring.ErrOutcomeUnknown
			}
		} else if errors.Is(e, sql.ErrNoRows) {
			proof, e := client.QueryRecollection(work, request)
			if e != nil || proof == nil || !proof.Terminal() || proof.Validate(request, time.Now().UTC()) != nil {
				return nil, wiring.ErrOutcomeUnknown
			}
			observation = *proof
			if e = ledger.Observe(work, binding, request, observation); e != nil {
				return nil, wiring.ErrOutcomeUnknown
			}
		} else {
			return nil, e
		}
		if !observation.Terminal() || observation.Validate(request, time.Now().UTC()) != nil {
			return nil, wiring.ErrOutcomeUnknown
		}
		if observation.State == "completed" {
			if e = ledger.Complete(work, binding, request, *observation.Result); e != nil {
				return nil, wiring.ErrOutcomeUnknown
			}
			exchanges = append(exchanges, wiring.ReadbackExchange{Request: request, Response: *observation.Result})
		}
	}
	archive, e := wiring.AttachOriginalReadback(host, binding, exchanges)
	if e != nil {
		// Both host and collector are positively stopped, but samples outside the
		// old advertising window cannot support a gate. Retain a BLOCKED host-only
		// capture; do not prolong process quarantine merely because data is unusable.
		archive, e = wiring.AttachOriginalReadback(host, binding, nil)
		if e != nil {
			return nil, wiring.ErrOutcomeUnknown
		}
	}
	raw, e := json.Marshal(archive)
	if e != nil {
		return nil, e
	}
	if len(raw) > wiring.MaxCaptureBytes {
		// All sessions are terminal, but a bounded complete aggregate is missing.
		// Keep original host proof and BLOCKED diagnostics; do not re-run devices.
		archive, e = wiring.AttachOriginalReadback(host, binding, nil)
		if e != nil {
			return nil, wiring.ErrOutcomeUnknown
		}
		raw, e = json.Marshal(archive)
		if e != nil {
			return nil, e
		}
	}
	return s.RecordWiringCapture(work, binding, raw)
}
