package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/enzoOvOyu/keel/internal/fabricdelivery"
	"regexp"
	"time"
)

type NetworkDeliveryExportInput struct {
	Kind                  string `json:"kind"`
	ExpectedPreflightHash string `json:"expected_preflight_hash"`
}
type NetworkDeliveryExport struct {
	ID                   string    `json:"id"`
	ProjectID            string    `json:"project_id"`
	PackageID            string    `json:"package_id"`
	Revision             int       `json:"revision"`
	Kind                 string    `json:"kind"`
	SchemaVersion        string    `json:"schema_version"`
	ScopeHash            string    `json:"scope_hash"`
	PreflightHash        string    `json:"preflight_hash"`
	ContentHash          string    `json:"content_hash"`
	ConsistencyCheckRate float64   `json:"consistency_check_rate"`
	CreatedBy            string    `json:"created_by"`
	CreatedAt            time.Time `json:"created_at"`
	Size                 int       `json:"size"`
}
type NetworkDeliveryPreview struct {
	Preflight      fabricdelivery.Preflight   `json:"preflight"`
	References     []fabricdelivery.Reference `json:"references"`
	ObjectCount    int                        `json:"object_count"`
	ReferenceTotal int                        `json:"reference_total"`
}

var deliveryIdempotencyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9:._-]{0,159}$`)

func (s *Store) beginDeliveryRead(ctx context.Context, a *AuthSession, project string) (*sql.Tx, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	if e = requireFabricPermission(ctx, tx, a, project, "fabric:read"); e != nil {
		tx.Rollback()
		return nil, e
	}
	return tx, nil
}
func (s *Store) prepareNetworkDelivery(ctx context.Context, a *AuthSession, p *ProjectSummary) (*fabricdelivery.Facts, fabricdelivery.Review, error) {
	tx, e := s.beginDeliveryRead(ctx, a, p.ID)
	if e != nil {
		return nil, fabricdelivery.Review{}, e
	}
	facts, e := s.networkDeliveryFactsTx(ctx, tx, p.ID)
	tx.Rollback()
	if e != nil {
		return nil, fabricdelivery.Review{}, e
	}
	review, e := s.FabricProbes.ReadDeliveryReview(ctx, p.ID, fabricdelivery.ScopeHash(*facts))
	if e != nil {
		return nil, review, apiError(502, "NETWORK_DELIVERY_REVIEW_INVALID", e.Error())
	}
	return facts, review, nil
}
func (s *Store) PreviewNetworkDelivery(ctx context.Context, a *AuthSession, p *ProjectSummary) (*NetworkDeliveryPreview, error) {
	facts, review, e := s.prepareNetworkDelivery(ctx, a, p)
	if e != nil {
		return nil, e
	}
	out := &NetworkDeliveryPreview{Preflight: fabricdelivery.Evaluate(*facts, review, time.Now().UTC()), References: facts.References[:min(len(facts.References), 100)], ObjectCount: len(facts.Objects), ReferenceTotal: len(facts.References)}
	return out, nil
}
func (s *Store) CreateNetworkDeliveryExport(ctx context.Context, a *AuthSession, p *ProjectSummary, input NetworkDeliveryExportInput, key, trace string) (*NetworkDeliveryExport, error) {
	if (input.Kind != "draft" && input.Kind != "delivery") || !ztpSHA256Pattern.MatchString(input.ExpectedPreflightHash) || !deliveryIdempotencyPattern.MatchString(key) {
		return nil, apiError(400, "NETWORK_DELIVERY_INPUT_INVALID", "Choose draft/delivery, supply the reviewed preflight hash and an idempotency key")
	}
	ledgerID := digest("network-delivery:" + p.ID + ":" + a.User.ID + ":" + key)
	requestHash := fabricdelivery.Digest(input)
	// Idempotent replay remains project-authorized and returns the original immutable export.
	read, e := s.beginDeliveryRead(ctx, a, p.ID)
	if e != nil {
		return nil, e
	}
	var priorHash string
	var priorJSON []byte
	e = read.QueryRowContext(ctx, `SELECT request_hash,response FROM idempotencies WHERE id=$1 AND actor_id=$2`, ledgerID, a.User.ID).Scan(&priorHash, &priorJSON)
	read.Rollback()
	if e == nil {
		if priorHash != requestHash {
			return nil, apiError(409, "IDEMPOTENCY_CONFLICT", "The idempotency key belongs to another reviewed request")
		}
		var previous NetworkDeliveryExport
		if e = json.Unmarshal(priorJSON, &previous); e != nil {
			return nil, e
		}
		return &previous, nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return nil, e
	}
	facts, review, e := s.prepareNetworkDelivery(ctx, a, p)
	if e != nil {
		return nil, e
	}
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireFabricPermission(ctx, tx, a, p.ID, "fabric:read"); e != nil {
		return nil, e
	}
	// Concurrent retry may have committed while external read-only review ran.
	e = tx.QueryRowContext(ctx, `SELECT request_hash,response FROM idempotencies WHERE id=$1 AND actor_id=$2`, ledgerID, a.User.ID).Scan(&priorHash, &priorJSON)
	if e == nil {
		if priorHash != requestHash {
			return nil, apiError(409, "IDEMPOTENCY_CONFLICT", "The key belongs to another request")
		}
		var previous NetworkDeliveryExport
		if e = json.Unmarshal(priorJSON, &previous); e != nil {
			return nil, e
		}
		return &previous, tx.Commit()
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return nil, e
	}

	// Re-read the full reference graph under the existing serialized control lock.
	snapshot, e := s.beginDeliveryRead(ctx, a, p.ID)
	if e != nil {
		return nil, e
	}
	current, e := s.networkDeliveryFactsTx(ctx, snapshot, p.ID)
	snapshot.Rollback()
	if e != nil {
		return nil, e
	}
	if fabricdelivery.ScopeHash(*current) != fabricdelivery.ScopeHash(*facts) {
		return nil, apiError(409, "NETWORK_DELIVERY_SCOPE_CHANGED", "BOM, configuration, rules or live evidence changed; review the current preflight")
	}
	now := time.Now().UTC()
	preflight := fabricdelivery.Evaluate(*current, review, now)
	if preflight.PreflightHash != input.ExpectedPreflightHash {
		return nil, apiError(409, "NETWORK_DELIVERY_PREFLIGHT_STALE", "The reviewed evidence or acceptance changed; refresh the preflight")
	}
	if input.Kind == "delivery" && !preflight.DeliveryAllowed {
		return nil, apiError(409, "NETWORK_DELIVERY_BLOCKED", "N-S0 through N-S8 and exact reference integrity must pass or have valid scope-bound approved waivers; draft evidence remains available")
	}
	if current.PackageID == "" {
		return nil, apiError(409, "NETWORK_DELIVERY_BOM_REQUIRED", "Import a committed network BOM before creating an evidence archive")
	}
	var revision int
	if e = tx.QueryRowContext(ctx, `SELECT coalesce(max(revision),0)+1 FROM network_delivery_exports WHERE project_id=$1`, p.ID).Scan(&revision); e != nil {
		return nil, e
	}
	id := RandomID()
	_, content, e := fabricdelivery.Build(id, revision, input.Kind, *current, preflight, now)
	if e != nil {
		return nil, apiError(409, "NETWORK_DELIVERY_BUILD_BLOCKED", e.Error())
	}
	if len(content) > 32<<20 {
		return nil, apiError(409, "NETWORK_DELIVERY_ARCHIVE_LIMIT", "This scope requires a streaming archive adapter; the inline exporter is bounded to 32MiB")
	}
	out := &NetworkDeliveryExport{ID: id, ProjectID: p.ID, PackageID: current.PackageID, Revision: revision, Kind: input.Kind, SchemaVersion: fabricdelivery.Version, ScopeHash: preflight.ScopeHash, PreflightHash: preflight.PreflightHash, ContentHash: fabricdelivery.BytesDigest(content), ConsistencyCheckRate: preflight.ConsistencyCheckRate, CreatedBy: a.User.ID, CreatedAt: now, Size: len(content)}
	manifest, _ := json.Marshal(current.References)
	_, e = tx.ExecContext(ctx, `INSERT INTO network_delivery_exports(id,project_id,package_id,revision,kind,schema_version,scope_hash,preflight_hash,content_hash,consistency_check_rate,manifest,content,created_by,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`, out.ID, p.ID, current.PackageID, revision, input.Kind, out.SchemaVersion, out.ScopeHash, out.PreflightHash, out.ContentHash, out.ConsistencyCheckRate, manifest, content, a.User.ID, now)
	if e != nil {
		return nil, e
	}
	if e = appendAudit(ctx, tx, a.User.ID, p.ID, "fabric.delivery.export", id, trace, map[string]any{"kind": out.Kind, "revision": revision, "package_id": out.PackageID, "content_hash": out.ContentHash, "scope_hash": out.ScopeHash, "consistency_check_rate": out.ConsistencyCheckRate, "is_delivery": input.Kind == "delivery"}); e != nil {
		return nil, e
	}
	response, _ := json.Marshal(out)
	if _, e = tx.ExecContext(ctx, `INSERT INTO idempotencies(id,actor_id,request_hash,response,created_at) VALUES($1,$2,$3,$4,$5)`, ledgerID, a.User.ID, requestHash, response, now); e != nil {
		return nil, e
	}
	return out, tx.Commit()
}

const networkDeliveryFields = `id,project_id,package_id,revision,kind,schema_version,scope_hash,preflight_hash,content_hash,consistency_check_rate,created_by,created_at,octet_length(content)`

func scanNetworkDelivery(row scanner) (*NetworkDeliveryExport, error) {
	var r NetworkDeliveryExport
	e := row.Scan(&r.ID, &r.ProjectID, &r.PackageID, &r.Revision, &r.Kind, &r.SchemaVersion, &r.ScopeHash, &r.PreflightHash, &r.ContentHash, &r.ConsistencyCheckRate, &r.CreatedBy, &r.CreatedAt, &r.Size)
	return &r, e
}
func (s *Store) ListNetworkDeliveryExports(ctx context.Context, a *AuthSession, p *ProjectSummary) ([]NetworkDeliveryExport, error) {
	tx, e := s.beginDeliveryRead(ctx, a, p.ID)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	rows, e := tx.QueryContext(ctx, `SELECT `+networkDeliveryFields+` FROM network_delivery_exports WHERE project_id=$1 ORDER BY revision DESC LIMIT 100`, p.ID)
	if e != nil {
		return nil, e
	}
	out := []NetworkDeliveryExport{}
	for rows.Next() {
		r, e := scanNetworkDelivery(rows)
		if e != nil {
			rows.Close()
			return nil, e
		}
		out = append(out, *r)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	return out, tx.Commit()
}
func (s *Store) DownloadNetworkDeliveryExport(ctx context.Context, a *AuthSession, p *ProjectSummary, id, trace string) (*NetworkDeliveryExport, []byte, error) {
	if !bootProjectID.MatchString(id) {
		return nil, nil, ErrNotFound
	}
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, nil, e
	}
	defer tx.Rollback()
	if e = requireFabricPermission(ctx, tx, a, p.ID, "fabric:read"); e != nil {
		return nil, nil, e
	}
	r, e := scanNetworkDelivery(tx.QueryRowContext(ctx, `SELECT `+networkDeliveryFields+` FROM network_delivery_exports WHERE project_id=$1 AND id=$2`, p.ID, id))
	if errors.Is(e, sql.ErrNoRows) {
		return nil, nil, ErrNotFound
	}
	if e != nil {
		return nil, nil, e
	}
	var body []byte
	if e = tx.QueryRowContext(ctx, `SELECT content FROM network_delivery_exports WHERE project_id=$1 AND id=$2`, p.ID, id).Scan(&body); e != nil {
		return nil, nil, e
	}
	if fabricdelivery.BytesDigest(body) != r.ContentHash {
		return nil, nil, apiError(409, "NETWORK_DELIVERY_ARCHIVE_CORRUPT", "Archived bytes do not match the recorded content hash")
	}
	if e = appendAudit(ctx, tx, a.User.ID, p.ID, "fabric.delivery.download", id, trace, map[string]any{"kind": r.Kind, "revision": r.Revision, "content_hash": r.ContentHash}); e != nil {
		return nil, nil, e
	}
	return r, body, tx.Commit()
}

type NetworkDeliveryReferencePage struct {
	Items    []fabricdelivery.Reference `json:"items"`
	Total    int                        `json:"total"`
	Page     int                        `json:"page"`
	PageSize int                        `json:"page_size"`
}

func (s *Store) NetworkDeliveryReferences(ctx context.Context, a *AuthSession, p *ProjectSummary, page, size int) (*NetworkDeliveryReferencePage, error) {
	if page < 1 || page > 1000000 || size < 1 || size > 1000 {
		return nil, apiError(400, "NETWORK_DELIVERY_PAGE_INVALID", "Use page >=1 and page_size from 1 through 1000")
	}
	tx, e := s.beginDeliveryRead(ctx, a, p.ID)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	facts, e := s.networkDeliveryFactsTx(ctx, tx, p.ID)
	if e != nil {
		return nil, e
	}
	out := &NetworkDeliveryReferencePage{Items: []fabricdelivery.Reference{}, Total: len(facts.References), Page: page, PageSize: size}
	start := (page - 1) * size
	end := page * size
	if end > len(facts.References) {
		end = len(facts.References)
	}
	if start < len(facts.References) {
		out.Items = facts.References[start:end]
	}
	return out, tx.Commit()
}
