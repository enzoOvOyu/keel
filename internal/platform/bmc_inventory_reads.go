package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/enzoOvOyu/keel/internal/bmc"
)

type BMCCollectionSummary struct {
	ID                     string    `json:"id"`
	ProjectID              string    `json:"project_id"`
	AssetID                string    `json:"asset_id"`
	MachineID              string    `json:"machine_id"`
	ExecutionID            string    `json:"execution_id"`
	Level                  string    `json:"level"`
	Origin                 string    `json:"origin"`
	State                  string    `json:"state"`
	ParserVersion          string    `json:"parser_version"`
	CollectedAt            time.Time `json:"collected_at"`
	ReceivedAt             time.Time `json:"received_at"`
	ContentHash            string    `json:"content_hash"`
	IdentityMatched        bool      `json:"identity_matched"`
	AssetCurrent           bool      `json:"asset_current"`
	Freshness              string    `json:"freshness"`
	FreshnessMaxAgeSeconds int64     `json:"freshness_max_age_seconds"`
	ResourceCount          int       `json:"resource_count"`
	FailureCount           int       `json:"failure_count"`
}
type BMCCollectionDetail struct {
	BMCCollectionSummary
	Report *bmc.Snapshot `json:"report"`
}

func bmcFreshness(summary *BMCCollectionSummary, now time.Time) {
	summary.Freshness = "unknown"
	if summary.State == "failed" || !summary.IdentityMatched || summary.CollectedAt.IsZero() || summary.CollectedAt.After(now) {
		return
	}
	if !summary.AssetCurrent {
		summary.Freshness = "stale"
		return
	}
	age := now.Sub(summary.CollectedAt)
	limit := time.Duration(summary.FreshnessMaxAgeSeconds) * time.Second
	switch {
	case age >= limit:
		summary.Freshness = "stale"
	case age >= limit/2:
		summary.Freshness = "aging"
	default:
		summary.Freshness = "fresh"
	}
}
func bmcReadScopeTx(ctx context.Context, tx *sql.Tx, a *AuthSession, project, asset string) (int64, error) {
	if e := requireProjectMember(ctx, tx, a, project); e != nil {
		return 0, e
	}
	var exists bool
	e := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM project_assets WHERE id=$1 AND project_id=$2)`, asset, project).Scan(&exists)
	if e != nil {
		return 0, e
	}
	if !exists {
		return 0, ErrNotFound
	}
	values, e := catalogDefaultsTx(ctx, tx, project, "asset.freshness_max_age")
	if e != nil {
		return 0, e
	}
	maxAge := 24 * time.Hour
	if value, ok := values["asset.freshness_max_age"]; ok {
		maxAge, e = time.ParseDuration(value)
		if e != nil || maxAge < time.Second || maxAge > 365*24*time.Hour {
			return 0, apiError(409, "BMC_FRESHNESS_POLICY_INVALID", "asset.freshness_max_age must be a positive duration no greater than one year")
		}
	}
	return int64(maxAge / time.Second), nil
}

const bmcSummaryFields = `r.id,r.project_id,r.asset_id,r.machine_id,r.execution_id,r.level,r.origin,r.state,r.parser_version,r.collected_at,r.received_at,r.content_hash,coalesce((r.report->>'identity_matched')::boolean,false),(a.version=q.asset_version AND a.machine_id=r.machine_id AND EXISTS(SELECT 1 FROM machines machine WHERE machine.id=a.machine_id AND machine.serial_number=q.target->>'serial') AND (coalesce(q.target->>'bom_package_id','')='' OR q.target->>'bom_package_id'=(SELECT p.id FROM bom_item_revisions v JOIN bom_import_packages p ON p.id=v.package_id AND p.project_id=v.project_id AND p.state='committed' WHERE v.project_id=r.project_id AND v.item_uid=r.machine_id ORDER BY p.committed_at DESC,p.id DESC LIMIT 1))),jsonb_array_length(r.report->'resources'),jsonb_array_length(r.report->'failures')`
const bmcSummaryFrom = ` FROM bmc_collection_records r JOIN bmc_collection_requests q ON q.id=r.id JOIN project_assets a ON a.id=r.asset_id WHERE r.project_id=$1 AND r.asset_id=$2`

func scanBMCCollectionSummary(row scanner, maxAge int64) (*BMCCollectionSummary, error) {
	var v BMCCollectionSummary
	e := row.Scan(&v.ID, &v.ProjectID, &v.AssetID, &v.MachineID, &v.ExecutionID, &v.Level, &v.Origin, &v.State, &v.ParserVersion, &v.CollectedAt, &v.ReceivedAt, &v.ContentHash, &v.IdentityMatched, &v.AssetCurrent, &v.ResourceCount, &v.FailureCount)
	if e != nil {
		return nil, e
	}
	v.FreshnessMaxAgeSeconds = maxAge
	bmcFreshness(&v, time.Now().UTC())
	return &v, nil
}
func (s *Store) ListBMCCollections(ctx context.Context, a *AuthSession, p *ProjectSummary, asset string, page Pagination) (Page[BMCCollectionSummary], error) {
	out := NewPage[BMCCollectionSummary](page)
	if e := page.Validate(); e != nil {
		return out, e
	}
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	maxAge, e := bmcReadScopeTx(ctx, tx, a, p.ID, asset)
	if e != nil {
		return out, e
	}
	if e = tx.QueryRowContext(ctx, "SELECT count(*)"+bmcSummaryFrom, p.ID, asset).Scan(&out.Total); e != nil {
		return out, e
	}
	rows, e := tx.QueryContext(ctx, "SELECT "+bmcSummaryFields+bmcSummaryFrom+` ORDER BY r.received_at DESC,r.id DESC LIMIT $3 OFFSET $4`, p.ID, asset, page.PageSize, page.Offset())
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		v, err := scanBMCCollectionSummary(rows, maxAge)
		if err != nil {
			return out, err
		}
		out.Items = append(out.Items, *v)
	}
	return out, rows.Err()
}
func (s *Store) GetBMCCollection(ctx context.Context, a *AuthSession, p *ProjectSummary, asset, id string) (*BMCCollectionDetail, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	maxAge, e := bmcReadScopeTx(ctx, tx, a, p.ID, asset)
	if e != nil {
		return nil, e
	}
	summary, e := scanBMCCollectionSummary(tx.QueryRowContext(ctx, "SELECT "+bmcSummaryFields+bmcSummaryFrom+` AND r.id=$3`, p.ID, asset, id), maxAge)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if e != nil {
		return nil, e
	}
	var raw []byte
	if e = tx.QueryRowContext(ctx, `SELECT report FROM bmc_collection_records WHERE id=$1 AND project_id=$2 AND asset_id=$3`, id, p.ID, asset).Scan(&raw); e != nil {
		return nil, e
	}
	out := &BMCCollectionDetail{BMCCollectionSummary: *summary}
	if e = json.Unmarshal(raw, &out.Report); e != nil {
		return nil, e
	}
	return out, nil
}
func (s *Store) OpenBMCRaw(ctx context.Context, a *AuthSession, p *ProjectSummary, asset, id, path string) (io.ReadSeekCloser, error) {
	if _, e := bmc.InventoryPath(path); e != nil {
		return nil, apiError(400, "BMC_RESOURCE_PATH_INVALID", "Select an exact stored inventory resource path")
	}
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	if _, e = bmcReadScopeTx(ctx, tx, a, p.ID, asset); e != nil {
		tx.Rollback()
		return nil, e
	}
	var key, version, sha string
	var size int64
	e = tx.QueryRowContext(ctx, `SELECT d.raw_key,d.raw_version,d.raw_sha256,d.raw_size FROM bmc_collection_resources d JOIN bmc_collection_records r ON r.id=d.collection_id WHERE r.project_id=$1 AND r.asset_id=$2 AND r.id=$3 AND d.resource_path=$4`, p.ID, asset, id, path).Scan(&key, &version, &sha, &size)
	tx.Rollback()
	if errors.Is(e, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if e != nil {
		return nil, e
	}
	return s.OpenCapturedArtifact(ctx, key, version, sha, size)
}
