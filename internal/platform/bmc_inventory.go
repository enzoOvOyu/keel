package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/enzoOvOyu/keel/internal/bmc"
)

type bmcRawSink struct{ objects MediaObjectStore }

func (s *Store) BMCInventoryRawSink() bmc.RawSink { return &bmcRawSink{objects: s.MediaObjects} }
func (s *bmcRawSink) StoreRaw(ctx context.Context, target bmc.Target, path string, body []byte) (bmc.RawReference, error) {
	var empty bmc.RawReference
	if target.Validate() != nil || len(body) == 0 || len(body) > bmc.MaxResourceBytes || s.objects == nil {
		return empty, errors.New("BMC raw storage unavailable")
	}
	if _, e := bmc.CanonicalPath("/redfish/v1/", path); e != nil {
		return empty, e
	}
	digest := sha256.Sum256(body)
	sha := hex.EncodeToString(digest[:])
	key := bmc.RawKey(target, path, sha)
	version, e := s.objects.Put(ctx, key, bytes.NewReader(body), int64(len(body)))
	if e != nil || version == "" || version == "null" {
		return empty, errors.New("BMC raw upload failed")
	}
	reader, e := s.objects.Open(ctx, key, version)
	if e != nil {
		return empty, errors.New("BMC raw version unavailable")
	}
	defer reader.Close()
	actual, e := io.ReadAll(io.LimitReader(reader, bmc.MaxResourceBytes+1))
	if e != nil || !bytes.Equal(actual, body) {
		return empty, errors.New("BMC raw version integrity failed")
	}
	return bmc.RawReference{Key: key, Version: version, SHA256: sha, Size: int64(len(body))}, nil
}

// RecordBMCCollection is a worker-only consumer, not an HTTP import endpoint.
// The request must already exist from domain-owned execution admission. No
// hardware request is made here and no G1/NP assertion or asset mapping is set.
func (s *Store) RecordBMCCollection(ctx context.Context, execution string, fence int64, report *bmc.Snapshot) error {
	if report == nil || report.Validate() != nil || fence < 1 {
		return apiError(400, "BMC_COLLECTION_INVALID", "Invalid bounded raw-backed inventory report")
	}
	if report.CollectedAt.After(time.Now().Add(5 * time.Second)) {
		return apiError(409, "BMC_COLLECTION_TIME_INVALID", "Collection time is in the future")
	}
	// Verify every fixed raw object before opening the short evidence transaction.
	for _, resource := range report.Resources {
		body, e := s.OpenCapturedArtifact(ctx, resource.Raw.Key, resource.Raw.Version, resource.Raw.SHA256, resource.Raw.Size)
		if e != nil {
			return e
		}
		raw, e := io.ReadAll(io.LimitReader(body, bmc.MaxResourceBytes+1))
		body.Close()
		if e != nil || bmc.CanonicalDigest(raw) != resource.DataSHA256 {
			return apiError(422, "BMC_RAW_STRUCTURED_MISMATCH", "Structured inventory does not match retained raw bytes")
		}
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = lockKey(ctx, tx, "keel-admin-initialization"); e != nil {
		return e
	}
	var target []byte
	var level, origin, creator string
	e = tx.QueryRowContext(ctx, `SELECT target,level,origin,created_by FROM bmc_collection_requests WHERE id=$1 AND project_id=$2 AND execution_id=$3 FOR UPDATE`, report.Target.RequestID, report.Target.ProjectID, execution).Scan(&target, &level, &origin, &creator)
	if errors.Is(e, sql.ErrNoRows) {
		return ErrNotFound
	}
	if e != nil {
		return e
	}
	if bmc.CanonicalDigest(target) != bmc.CanonicalDigest(networkJSON(report.Target)) || level != report.Level || origin != report.Origin {
		return apiError(409, "BMC_COLLECTION_SCOPE_CHANGED", "Inventory does not match the frozen request")
	}
	var valid bool
	e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM exec_task_attempts a JOIN exec_executions x ON x.id=a.execution_id JOIN exec_targets t ON t.id=a.target_id JOIN project_assets p ON p.id=$4 JOIN machines m ON m.id=p.machine_id JOIN users u ON u.id=$7 WHERE a.execution_id=$1 AND x.project_id=$2 AND t.resource_key=$3 AND a.fencing_token=$5 AND a.state='running' AND a.lease_until>now() AND x.desired_state IN('running','paused') AND p.project_id=$2 AND p.machine_id=$3 AND p.version=$6 AND m.serial_number=$8 AND u.enabled AND (u.is_admin OR EXISTS(SELECT 1 FROM memberships mb WHERE mb.project_id=$2 AND mb.user_id=u.id AND mb.role IN('DE','SRE','DL'))))`, execution, report.Target.ProjectID, report.Target.MachineID, report.Target.AssetID, fence, report.Target.AssetVersion, creator, report.Target.Serial).Scan(&valid)
	if e != nil {
		return e
	}
	if !valid {
		return apiError(409, "BMC_COLLECTION_FENCE_LOST", "Collection lease, current asset or owner authority changed")
	}
	raw := networkJSON(report)
	hash := bmc.CanonicalDigest(raw)
	var previous string
	e = tx.QueryRowContext(ctx, `SELECT content_hash FROM bmc_collection_records WHERE id=$1`, report.Target.RequestID).Scan(&previous)
	if e == nil {
		if previous != hash {
			return apiError(409, "BMC_COLLECTION_CONFLICT", "Existing collection cannot be overwritten")
		}
		return tx.Commit()
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO bmc_collection_records(id,project_id,asset_id,machine_id,execution_id,fencing_token,level,origin,state,parser_version,collected_at,received_at,content_hash,report) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,now(),$12,$13)`, report.Target.RequestID, report.Target.ProjectID, report.Target.AssetID, report.Target.MachineID, execution, fence, report.Level, report.Origin, report.State, report.ParserVersion, report.CollectedAt, hash, raw)
	if e != nil {
		return e
	}
	for _, r := range report.Resources {
		_, e = tx.ExecContext(ctx, `INSERT INTO bmc_collection_resources(collection_id,resource_path,resource_type,structured,data_sha256,raw_key,raw_version,raw_sha256,raw_size,received_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, report.Target.RequestID, r.Path, r.Type, []byte(r.Data), r.DataSHA256, r.Raw.Key, r.Raw.Version, r.Raw.SHA256, r.Raw.Size, r.ReceivedAt)
		if e != nil {
			return e
		}
	}
	if e = appendAudit(ctx, tx, creator, report.Target.ProjectID, "bmc.collection.record", report.Target.RequestID, "worker", map[string]any{"execution_id": execution, "fencing_token": fence, "level": level, "origin": origin, "state": report.State, "raw_resources": len(report.Resources), "content_hash": hash}); e != nil {
		return e
	}
	return tx.Commit()
}
