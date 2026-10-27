package platform

import (
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/enzoOvOyu/keel/internal/assetview"
)

type AssetViewData struct {
	View              *AssetView      `json:"view"`
	Items             []assetview.Row `json:"items"`
	Total             int             `json:"total"`
	Page              int             `json:"page"`
	PageSize          int             `json:"page_size"`
	EvaluatedAt       time.Time       `json:"evaluated_at"`
	SourceFingerprint string          `json:"source_fingerprint"`
}

// Full bounded scope is evaluated before sorting/grouping/pagination. More than
// 200 devices rejects as a whole; no silent first-page export or local-only sort.
func (s *Store) AssetViewData(ctx context.Context, a *AuthSession, project, id string, version int64, page Pagination, export bool) (*AssetViewData, error) {
	if e := page.Validate(); e != nil {
		return nil, e
	}
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireProjectMember(ctx, tx, a, project); e != nil {
		return nil, e
	}
	view, e := scanAssetView(tx.QueryRowContext(ctx, `SELECT `+assetViewFields+assetViewFrom+` AND v.id=$3`, project, a.User.ID, id))
	if errors.Is(e, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if e != nil {
		return nil, e
	}
	if version != view.Version {
		return nil, apiError(409, "ASSET_VIEW_VERSION_CONFLICT", "Evaluate the exact currently selected view revision")
	}
	baselines := map[string]*assetview.Baseline{}
	rows, e := tx.QueryContext(ctx, `SELECT id,key,updated_at,default_value,state FROM param_keys p WHERE scope_key IN('global',$1) AND NOT EXISTS(SELECT 1 FROM param_keys override WHERE override.key=p.key AND override.scope_key=$1 AND p.scope_key='global')`, "project:"+project)
	if e != nil {
		return nil, e
	}
	for rows.Next() {
		var b assetview.Baseline
		var at time.Time
		if e = rows.Scan(&b.ID, &b.Key, &at, &b.Value, &b.State); e != nil {
			rows.Close()
			return nil, e
		}
		b.Version = at.UTC().Format(time.RFC3339Nano)
		if len(b.Value) > 4096 {
			b.Value = ""
			b.State = "source_limit"
		}
		baselines[b.Key] = &b
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	ttlValues, e := catalogDefaultsTx(ctx, tx, project, "asset.freshness_max_age")
	if e != nil {
		return nil, e
	}
	ttl := 24 * time.Hour
	if v, ok := ttlValues["asset.freshness_max_age"]; ok {
		ttl, e = time.ParseDuration(v)
		if e != nil || ttl < time.Second || ttl > 365*24*time.Hour {
			return nil, apiError(409, "ASSET_VIEW_POLICY_INVALID", "Inventory freshness policy is invalid")
		}
	}
	const query = `WITH targets AS (
 SELECT a.id,a.machine_id device_id,'server'::text kind,a.hostname name,m.serial_number serial,a.version asset_version,jsonb_build_object('hostname',a.hostname,'serial',m.serial_number,'rack',m.rack,'state',a.state,'model_version_id',coalesce(a.model_version_id,''),'kind','server') metadata FROM project_assets a JOIN machines m ON m.id=a.machine_id WHERE a.project_id=$1
 UNION ALL
 SELECT s.id,s.id,'switch',s.hostname,coalesce(b.serial,''),0,jsonb_build_object('hostname',s.hostname,'serial',b.serial,'rack',b.position->>'rack','role',b.role,'kind','switch') FROM switches s JOIN bom_items b ON b.item_uid=s.id AND b.status<>'retired' WHERE EXISTS(SELECT 1 FROM bom_item_revisions r WHERE r.project_id=$1 AND r.item_uid=s.id)
 ) SELECT t.id,t.device_id,t.kind,t.name,t.serial,t.metadata,coalesce(b.id,''),coalesce(b.content_hash,''),coalesce(b.origin,''),coalesce(b.state,''),b.collected_at,
 (b.report->>'identity_matched'='true' AND b.report#>>'{target,asset_version}'=t.asset_version::text AND b.report#>>'{target,serial}'=t.serial AND coalesce(b.report#>>'{target,bom_package_id}','')=coalesce((SELECT p.id FROM bom_item_revisions v JOIN bom_import_packages p ON p.id=v.package_id AND p.project_id=v.project_id AND p.state='committed' WHERE v.project_id=$1 AND v.item_uid=t.device_id ORDER BY p.committed_at DESC,p.id DESC LIMIT 1),'')),
 CASE WHEN octet_length(b.report::text)<=2097152 THEN b.report ELSE NULL END,coalesce(n.run_id,''),coalesce(n.content_hash,''),n.collected_at,n.valid_until,coalesce(n.evidence_source,''),coalesce(n.package_id,''),CASE WHEN octet_length(n.sample::text)<=2097152 THEN n.sample ELSE NULL END
 FROM targets t LEFT JOIN LATERAL (SELECT * FROM bmc_collection_records b WHERE b.project_id=$1 AND b.asset_id=t.id AND t.kind='server' ORDER BY b.received_at DESC,b.id DESC LIMIT 1)b ON true
 LEFT JOIN LATERAL (SELECT sample.*,run.collected_at,run.valid_until,run.report->>'evidence_source' evidence_source,run.package_id FROM fabric_switch_samples sample JOIN fabric_collection_runs run ON run.id=sample.run_id WHERE run.project_id=$1 AND sample.switch_id=t.device_id AND t.kind='switch' ORDER BY run.collected_at DESC,run.id DESC LIMIT 1)n ON true ORDER BY t.kind,t.id LIMIT 201`
	var networkPackage string
	// Keep the network package pin independent of whichever server BOM is newer.
	e = tx.QueryRowContext(ctx, `SELECT p.id FROM bom_import_packages p WHERE p.project_id=$1 AND p.state='committed' AND EXISTS(SELECT 1 FROM bom_item_revisions r JOIN bom_items b ON b.item_uid=r.item_uid AND b.category='switch' WHERE r.package_id=p.id) ORDER BY p.committed_at DESC,p.id DESC LIMIT 1`, project).Scan(&networkPackage)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return nil, e
	}
	rows, e = tx.QueryContext(ctx, query, project)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	now := time.Now().UTC()
	out := &AssetViewData{View: view, Items: []assetview.Row{}, Page: page.Page, PageSize: page.PageSize, EvaluatedAt: now}
	for rows.Next() {
		var row assetview.Row
		var metadata, bmcRaw, networkRaw []byte
		var bID, bHash, bOrigin, bState, nID, nHash, nSource, nPackage string
		var bAt, nAt, nUntil *time.Time
		var bCurrent sql.NullBool
		if e = rows.Scan(&row.ID, &row.DeviceID, &row.Kind, &row.Name, &row.Serial, &metadata, &bID, &bHash, &bOrigin, &bState, &bAt, &bCurrent, &bmcRaw, &nID, &nHash, &nAt, &nUntil, &nSource, &nPackage, &networkRaw); e != nil {
			return nil, e
		}
		roots := map[string]any{}
		roots["asset"], e = assetview.Decode(metadata)
		if e != nil {
			return nil, e
		}
		row.Cells = map[string]assetview.Cell{}
		row.Sources = map[string]assetview.Source{"asset": {ID: row.ID, Origin: "registry", State: "current"}}
		bSource := assetview.Source{ID: bID, Hash: bHash, Origin: bOrigin, State: "missing"}
		if bAt != nil {
			bSource.CollectedAt = bAt.UTC().Format(time.RFC3339Nano)
			bSource.ValidUntil = bAt.Add(ttl).UTC().Format(time.RFC3339Nano)
			bSource.State = "stale"
			if bCurrent.Valid && bCurrent.Bool && !bAt.After(now) && now.Sub(*bAt) < ttl && bState != "failed" {
				bSource.State = "current"
			}
			if bState == "partial" && bSource.State == "current" {
				bSource.State = "partial"
			}
			if bState == "failed" {
				bSource.State = "failed"
			}
			if bOrigin == "sim" && bSource.State == "current" {
				bSource.State = "simulated"
			}
			if len(bmcRaw) == 0 {
				bSource.State = "source_limit"
			} else {
				roots["redfish"], e = assetview.Decode(bmcRaw)
				if e != nil {
					return nil, e
				}
			}
		}
		nOrigin := ""
		nState := "missing"
		if nAt != nil {
			nState = "stale"
			if nUntil != nil && nUntil.After(now) && !nAt.After(now) && nPackage == networkPackage {
				nState = "current"
			}
			if nSource == "protocol_fixture" || nSource == "sim" {
				nOrigin = "sim"
				nState = "simulated"
			} else if nSource == "device" {
				nOrigin = "real"
			} else {
				nOrigin = "unknown"
				nState = "source_unknown"
			}
			if len(networkRaw) == 0 {
				nState = "source_limit"
			} else {
				roots["network"], e = assetview.Decode(networkRaw)
				if e != nil {
					return nil, e
				}
			}
		}
		nTime := ""
		nExpiry := ""
		if nUntil != nil {
			nExpiry = nUntil.UTC().Format(time.RFC3339Nano)
		}
		if nAt != nil {
			nTime = nAt.UTC().Format(time.RFC3339Nano)
		}
		row.Sources["redfish"] = bSource
		row.Sources["network"] = assetview.Source{ID: nID, Hash: nHash, Origin: nOrigin, State: nState, CollectedAt: nTime, ValidUntil: nExpiry}
		for _, column := range view.Definition.Columns {
			var baseline *assetview.Baseline
			if column.Color != nil {
				baseline = baselines[column.Color.BaselineKey]
			}
			row.Cells[column.ID] = assetview.Evaluate(column, roots[column.Source], row.Sources[column.Source].State, baseline)
		}
		out.Items = append(out.Items, row)
		if len(out.Items) > 200 {
			return nil, apiError(409, "ASSET_VIEW_SCOPE_LIMIT", "View and export support at most 200 devices; no partial scope returned")
		}
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	assetview.Sort(out.Items, view.Definition)
	out.Total = len(out.Items)
	out.SourceFingerprint = digest(string(networkJSON(map[string]any{"view": view.ContentHash, "rows": out.Items})))
	if !export {
		start := min(page.Offset(), out.Total)
		end := min(start+page.PageSize, out.Total)
		out.Items = out.Items[start:end]
	}
	return out, nil
}

// Prefix dangerous spreadsheet formula cells, including leading whitespace.
func assetViewCSVCell(v string) string {
	test := strings.TrimLeft(v, " \t\r\n")
	if test != "" && strings.ContainsRune("=+-@", rune(test[0])) {
		return "'" + v
	}
	return v
}
func AssetViewCSV(data *AssetViewData) (string, error) {
	var out strings.Builder
	writer := csv.NewWriter(&out)
	headers := []string{"asset_id", "device_id", "kind", "name", "serial", "view_version", "source_fingerprint", "sources"}
	for _, column := range data.View.Definition.Columns {
		headers = append(headers, column.Title, column.Title+" [state]", column.Title+" [tone]", column.Title+" [baseline]")
	}
	for i := range headers {
		headers[i] = assetViewCSVCell(headers[i])
	}
	if e := writer.Write(headers); e != nil {
		return "", e
	}
	for _, row := range data.Items {
		values := []string{row.ID, row.DeviceID, row.Kind, row.Name, row.Serial, strconv.FormatInt(data.View.Version, 10), data.SourceFingerprint, string(networkJSON(row.Sources))}
		for _, column := range data.View.Definition.Columns {
			cell := row.Cells[column.ID]
			baseline := ""
			if cell.Baseline != nil {
				baseline = string(networkJSON(cell.Baseline))
			}
			values = append(values, assetview.Text(cell), cell.State, cell.Tone, baseline)
		}
		for i := range values {
			values[i] = assetViewCSVCell(values[i])
		}
		if e := writer.Write(values); e != nil {
			return "", e
		}
	}
	writer.Flush()
	return out.String(), writer.Error()
}
