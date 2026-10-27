package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/enzoOvOyu/keel/internal/bmc"
)

type BMCCollectionManifest struct {
	BMCCollectionSummary
	Source             string            `json:"source"`
	StartedAt          time.Time         `json:"started_at"`
	CredentialBatchRef string            `json:"credential_batch_ref"`
	CredentialVersion  string            `json:"credential_version"`
	AdapterBindingHash string            `json:"adapter_binding_hash"`
	Capabilities       map[string]string `json:"capabilities"`
	Failures           []bmc.Failure     `json:"failures"`
	Degraded           bool              `json:"degraded"`
}
type BMCResourceSummary struct {
	Path       string           `json:"path"`
	Type       string           `json:"type"`
	Source     string           `json:"source"`
	ReceivedAt time.Time        `json:"received_at"`
	DataSHA256 string           `json:"data_sha256"`
	Raw        bmc.RawReference `json:"raw"`
}

func (s *Store) GetBMCCollectionManifest(ctx context.Context, a *AuthSession, p *ProjectSummary, asset, id string) (*BMCCollectionManifest, error) {
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
	out := &BMCCollectionManifest{BMCCollectionSummary: *summary}
	var capabilities, failures []byte
	e = tx.QueryRowContext(ctx, `SELECT report->>'source',(report->>'started_at')::timestamptz,report#>>'{target,credential_batch_ref}',report#>>'{target,credential_version}',coalesce(report#>>'{target,adapter_binding_hash}',''),report->'capabilities',report->'failures',(report->>'degraded')::boolean FROM bmc_collection_records WHERE id=$1 AND project_id=$2 AND asset_id=$3`, id, p.ID, asset).Scan(&out.Source, &out.StartedAt, &out.CredentialBatchRef, &out.CredentialVersion, &out.AdapterBindingHash, &capabilities, &failures, &out.Degraded)
	if e != nil {
		return nil, e
	}
	if e = json.Unmarshal(capabilities, &out.Capabilities); e != nil {
		return nil, e
	}
	if e = json.Unmarshal(failures, &out.Failures); e != nil {
		return nil, e
	}
	return out, nil
}
func (s *Store) ListBMCResources(ctx context.Context, a *AuthSession, p *ProjectSummary, asset, id string, page Pagination) (Page[BMCResourceSummary], error) {
	out := NewPage[BMCResourceSummary](page)
	if e := page.Validate(); e != nil {
		return out, e
	}
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	if _, e = bmcReadScopeTx(ctx, tx, a, p.ID, asset); e != nil {
		return out, e
	}
	var exists bool
	e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM bmc_collection_records WHERE id=$1 AND project_id=$2 AND asset_id=$3)`, id, p.ID, asset).Scan(&exists)
	if e != nil {
		return out, e
	}
	if !exists {
		return out, ErrNotFound
	}
	if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM bmc_collection_resources WHERE collection_id=$1`, id).Scan(&out.Total); e != nil {
		return out, e
	}
	rows, e := tx.QueryContext(ctx, `SELECT resource_path,resource_type,received_at,data_sha256,raw_key,raw_version,raw_sha256,raw_size FROM bmc_collection_resources WHERE collection_id=$1 ORDER BY resource_path LIMIT $2 OFFSET $3`, id, page.PageSize, page.Offset())
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		v := BMCResourceSummary{Source: "bmc"}
		if e = rows.Scan(&v.Path, &v.Type, &v.ReceivedAt, &v.DataSHA256, &v.Raw.Key, &v.Raw.Version, &v.Raw.SHA256, &v.Raw.Size); e != nil {
			return out, e
		}
		out.Items = append(out.Items, v)
	}
	return out, rows.Err()
}
func (s *Store) GetBMCResource(ctx context.Context, a *AuthSession, p *ProjectSummary, asset, id, path string) (*bmc.Resource, error) {
	canonical, e := bmc.CanonicalPath("/redfish/v1/", path)
	if e != nil || canonical != path {
		return nil, apiError(400, "BMC_RESOURCE_PATH_INVALID", "Select an exact stored resource path")
	}
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if _, e = bmcReadScopeTx(ctx, tx, a, p.ID, asset); e != nil {
		return nil, e
	}
	out := &bmc.Resource{Source: "bmc"}
	var data []byte
	e = tx.QueryRowContext(ctx, `SELECT v.resource_path,v.resource_type,v.received_at,v.data_sha256,v.raw_key,v.raw_version,v.raw_sha256,v.raw_size,v.structured FROM bmc_collection_resources v JOIN bmc_collection_records r ON r.id=v.collection_id WHERE r.id=$1 AND r.project_id=$2 AND r.asset_id=$3 AND v.resource_path=$4`, id, p.ID, asset, path).Scan(&out.Path, &out.Type, &out.ReceivedAt, &out.DataSHA256, &out.Raw.Key, &out.Raw.Version, &out.Raw.SHA256, &out.Raw.Size, &data)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if e != nil {
		return nil, e
	}
	out.Data = data
	return out, nil
}
