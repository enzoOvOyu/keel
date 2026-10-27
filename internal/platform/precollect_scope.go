package platform

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"time"

	"github.com/enzoOvOyu/keel/internal/precollect"
	"github.com/enzoOvOyu/keel/internal/provisionpreflight"
)

type PrecollectBMCProof struct {
	ID          string    `json:"id"`
	ContentHash string    `json:"content_hash"`
	Origin      string    `json:"origin"`
	BindingHash string    `json:"binding_hash"`
	CollectedAt time.Time `json:"collected_at"`
	Address     string    `json:"address"`
}
type PrecollectComponent struct {
	ID           string `json:"id"`
	Category     string `json:"category"`
	Serial       string `json:"serial"`
	Slot         string `json:"slot"`
	ModelVersion string `json:"model_version_id"`
}
type PrecollectScope struct {
	SSHHostKey         string                 `json:"ssh_host_key"`
	SSHBindingHash     string                 `json:"ssh_binding_hash"`
	ProjectID          string                 `json:"project_id"`
	AssetID            string                 `json:"asset_id"`
	MachineID          string                 `json:"machine_id"`
	AssetVersion       int64                  `json:"asset_version"`
	ModelVersion       string                 `json:"model_version_id"`
	ModelContentHash   string                 `json:"model_content_hash"`
	BOMPackageID       string                 `json:"bom_package_id"`
	CollectorVersion   string                 `json:"collector_version"`
	RuleVersion        string                 `json:"rule_version"`
	CatalogFingerprint string                 `json:"catalog_fingerprint"`
	GateTTLSeconds     int64                  `json:"gate_ttl_seconds"`
	AssetTTLSeconds    int64                  `json:"asset_ttl_seconds"`
	BMC                *PrecollectBMCProof    `json:"bmc"`
	Expectation        precollect.Expectation `json:"expectation"`
	Components         []PrecollectComponent  `json:"components"`
	ScopeHash          string                 `json:"scope_hash"`
	Ready              bool                   `json:"ready"`
	Code               string                 `json:"code"`
}

func PrecollectAssetState(state string) bool {
	switch state {
	case "provisioned", "trust_established", "collected", "lldp", "mapped", "configured", "netverified", "tested":
		return true
	}
	return false
}

// original pins the historical BMC source; a newer inventory capture does not
// invalidate unchanged, still-fresh evidence merely by existing.
func (s *Store) PreparePrecollectScopeTx(ctx context.Context, tx *sql.Tx, project, asset string, original *PrecollectScope, lock bool) (*PrecollectScope, error) {
	out := &PrecollectScope{CollectorVersion: precollect.Version, ProjectID: project, AssetID: asset, RuleVersion: precollect.RuleVersion, GateTTLSeconds: 86400, AssetTTLSeconds: 86400, Components: []PrecollectComponent{}, Expectation: precollect.Expectation{Ports: []precollect.ExpectedPort{}}, Ready: true, Code: "PRECOLLECT_READY"}
	block := func(code string) {
		out.Ready = false
		if out.Code == "PRECOLLECT_READY" {
			out.Code = code
		}
	}
	var state, address, trust, trustSource string
	queryAsset := `SELECT a.machine_id,a.version,coalesce(a.model_version_id,''),a.state,m.serial_number,m.bmc_address,m.ssh_trust,m.ssh_trust_source,m.host_key_fingerprint FROM project_assets a JOIN machines m ON m.id=a.machine_id WHERE a.id=$1 AND a.project_id=$2`
	if lock {
		queryAsset += ` FOR SHARE OF a,m`
	}
	e := tx.QueryRowContext(ctx, queryAsset, asset, project).Scan(&out.MachineID, &out.AssetVersion, &out.ModelVersion, &state, &out.Expectation.Serial, &address, &trust, &trustSource, &out.SSHHostKey)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if e != nil {
		return nil, e
	}
	if !PrecollectAssetState(state) {
		block("PRECOLLECT_ASSET_STATE_BLOCKED")
	}
	if trust != "established" || trustSource != "ssh" {
		block("PRECOLLECT_SSH_TRUST_REQUIRED")
	}
	if s.SSHRunner == nil {
		block("PRECOLLECT_SSH_BINDING_REQUIRED")
	} else if binding, e := s.SSHRunner.Binding(project, out.MachineID); e != nil || binding.HostKey != out.SSHHostKey {
		block("PRECOLLECT_SSH_BINDING_CHANGED")
	} else {
		out.SSHBindingHash = binding.ConfigHash
	}
	var hardware, bom []byte
	e = tx.QueryRowContext(ctx, `SELECT v.content_hash,v.hw_spec,v.bom_expect FROM device_model_versions v JOIN device_models m ON m.id=v.model_id WHERE v.id=$1 AND m.kind IN('server','compute-tray') AND m.scope_key IN('global',$2)`, out.ModelVersion, "project:"+project).Scan(&out.ModelContentHash, &hardware, &bom)
	if errors.Is(e, sql.ErrNoRows) {
		block("PRECOLLECT_MODEL_REQUIRED")
	} else if e != nil {
		return nil, e
	} else {
		expected, e := provisionpreflight.ExpectationFromModel(out.Expectation.Serial, "", hardware, bom)
		if e != nil || expected.GPUCount == nil || expected.NICCount == nil {
			block("PRECOLLECT_MODEL_EXPECTATION_REQUIRED")
		} else {
			out.Expectation.Arch = expected.Arch
			out.Expectation.GPUCount = expected.GPUCount
			out.Expectation.NICCount = expected.NICCount
		}
	}
	var bomModel string
	e = tx.QueryRowContext(ctx, `SELECT p.id,coalesce(v.row_snapshot#>>'{values,model_ref}','') FROM bom_item_revisions v JOIN bom_import_packages p ON p.id=v.package_id AND p.project_id=v.project_id AND p.state='committed' JOIN bom_items item ON item.item_uid=v.item_uid AND item.status<>'retired' WHERE v.project_id=$1 AND v.item_uid=$2 ORDER BY p.committed_at DESC,p.id DESC LIMIT 1`, project, out.MachineID).Scan(&out.BOMPackageID, &bomModel)
	if errors.Is(e, sql.ErrNoRows) {
		block("PRECOLLECT_BOM_REQUIRED")
	} else if e != nil {
		return nil, e
	} else if bomModel != out.ModelVersion {
		block("PRECOLLECT_BOM_MODEL_CHANGED")
	}
	rows, e := tx.QueryContext(ctx, `SELECT c.id,c.category,coalesce(c.serial_number,''),coalesce(c.slot,''),coalesce(r.row_snapshot#>>'{values,model_ref}','') FROM components c JOIN bom_items item ON item.item_uid=c.id AND item.status<>'retired' JOIN bom_item_revisions r ON r.item_uid=c.id AND r.project_id=$1 AND r.package_id=$2 WHERE c.machine_id=$3 ORDER BY c.id`, project, out.BOMPackageID, out.MachineID)
	if e != nil {
		return nil, e
	}
	for rows.Next() {
		var c PrecollectComponent
		if e = rows.Scan(&c.ID, &c.Category, &c.Serial, &c.Slot, &c.ModelVersion); e != nil {
			rows.Close()
			return nil, e
		}
		out.Components = append(out.Components, c)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	rows, e = tx.QueryContext(ctx, `SELECT p.id,p.parent_uid,coalesce(p.mac_address,''),coalesce(p.port_index,'') FROM physical_ports p JOIN components c ON c.id=p.parent_uid AND c.category='nic' JOIN bom_items item ON item.item_uid=p.id AND item.status<>'retired' JOIN bom_items parent ON parent.item_uid=c.id AND parent.status<>'retired' JOIN bom_item_revisions r ON r.item_uid=p.id AND r.project_id=$1 AND r.package_id=$2 WHERE p.machine_id=$3 ORDER BY p.id`, project, out.BOMPackageID, out.MachineID)
	if e != nil {
		return nil, e
	}
	for rows.Next() {
		var p precollect.ExpectedPort
		if e = rows.Scan(&p.ID, &p.ComponentID, &p.MAC, &p.PortIndex); e != nil {
			rows.Close()
			return nil, e
		}
		out.Expectation.Ports = append(out.Expectation.Ports, p)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	if len(out.Expectation.Ports) == 0 || len(out.Expectation.Ports) > 128 {
		block("PRECOLLECT_BOM_PORTS_REQUIRED")
	}
	if out.Expectation.GPUCount != nil {
		count := 0
		for _, c := range out.Components {
			if c.Category == "gpu" {
				count++
			}
		}
		if count != *out.Expectation.GPUCount {
			block("PRECOLLECT_BOM_MODEL_GPU_MISMATCH")
		}
	}
	values, e := catalogDefaultsTx(ctx, tx, project, "gate.evidence_ttl", "asset.freshness_max_age")
	if e != nil {
		return nil, e
	}
	for key, dst := range map[string]*int64{"gate.evidence_ttl": &out.GateTTLSeconds, "asset.freshness_max_age": &out.AssetTTLSeconds} {
		if value, ok := values[key]; ok {
			duration, e := time.ParseDuration(value)
			max := 24 * time.Hour
			if key == "asset.freshness_max_age" {
				max = 365 * 24 * time.Hour
			}
			if e != nil || duration < time.Second || duration > max {
				return nil, apiError(409, "PRECOLLECT_POLICY_INVALID", "G1 TTL must be at most 24 hours; inventory TTL at most one year; both must be positive")
			}
			*dst = int64(duration / time.Second)
		}
	}
	var catalog []byte
	e = tx.QueryRowContext(ctx, `SELECT coalesce(jsonb_agg(jsonb_build_object('id',p.id,'key',p.key,'updated_at',p.updated_at,'value',p.default_value) ORDER BY p.key),'[]'::jsonb) FROM param_keys p WHERE p.scope_key IN('global',$1) AND p.key IN('gate.evidence_ttl','asset.freshness_max_age') AND NOT EXISTS(SELECT 1 FROM param_keys override WHERE override.key=p.key AND override.scope_key=$1 AND p.scope_key='global')`, "project:"+project).Scan(&catalog)
	if e != nil {
		return nil, e
	}
	out.CatalogFingerprint = digest(string(catalog))
	proof := &PrecollectBMCProof{}
	query := `SELECT r.id,r.content_hash,r.origin,coalesce(r.report#>>'{target,adapter_binding_hash}',''),r.collected_at FROM bmc_collection_records r WHERE r.project_id=$1 AND r.asset_id=$2 AND r.machine_id=$3 AND r.state<>'failed' AND r.report->>'identity_matched'='true' AND (r.report#>>'{target,asset_version}')::bigint=$4`
	args := []any{project, asset, out.MachineID, out.AssetVersion}
	if original != nil && original.BMC != nil {
		query += ` AND r.id=$5`
		args = append(args, original.BMC.ID)
	} else {
		query += ` ORDER BY r.received_at DESC,r.id DESC LIMIT 1`
	}
	e = tx.QueryRowContext(ctx, query, args...).Scan(&proof.ID, &proof.ContentHash, &proof.Origin, &proof.BindingHash, &proof.CollectedAt)
	if errors.Is(e, sql.ErrNoRows) {
		block("PRECOLLECT_BMC_EVIDENCE_REQUIRED")
	} else if e != nil {
		return nil, e
	} else {
		out.BMC = proof
		now := time.Now()
		if proof.CollectedAt.After(now) || now.Sub(proof.CollectedAt) >= time.Duration(out.AssetTTLSeconds)*time.Second {
			block("PRECOLLECT_BMC_EVIDENCE_STALE")
		}
		if s.BMCInventory == nil {
			block("PRECOLLECT_BMC_BINDING_REQUIRED")
		} else if selection, e := s.BMCInventory.Selection(project, out.MachineID); e != nil || selection.BindingHash != proof.BindingHash || selection.Origin != proof.Origin {
			block("PRECOLLECT_BMC_BINDING_CHANGED")
		}
		ip := net.ParseIP(address)
		if ip == nil {
			block("PRECOLLECT_BMC_ADDRESS_REQUIRED")
		} else {
			var found bool
			e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM bmc_collection_resources r CROSS JOIN LATERAL jsonb_array_elements((CASE WHEN jsonb_typeof(r.structured->'IPv4Addresses')='array' THEN r.structured->'IPv4Addresses' ELSE '[]'::jsonb END)||(CASE WHEN jsonb_typeof(r.structured->'IPv6Addresses')='array' THEN r.structured->'IPv6Addresses' ELSE '[]'::jsonb END)) addr WHERE r.collection_id=$1 AND r.resource_type LIKE '#EthernetInterface.%' AND r.resource_path LIKE '/redfish/v1/Managers/%' AND addr->>'Address'=$2)`, proof.ID, ip.String()).Scan(&found)
			if e != nil {
				return nil, e
			}
			if !found {
				block("PRECOLLECT_BMC_ADDRESS_UNCONFIRMED")
			} else {
				proof.Address = ip.String()
			}
		}
	}
	if !s.PrecollectEnabled || s.MediaObjects == nil {
		block("PRECOLLECT_DISABLED")
	}
	// Hash all independent versions and stable identities; readiness is included.
	out.ScopeHash = precollect.Hash(networkJSON(out))
	return out, nil
}
func (s *Store) PreviewPrecollect(ctx context.Context, a *AuthSession, project, asset string) (*PrecollectScope, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireProjectMember(ctx, tx, a, project); e != nil {
		return nil, e
	}
	return s.PreparePrecollectScopeTx(ctx, tx, project, asset, nil, false)
}
