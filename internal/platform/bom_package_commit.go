package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type BOMPackageCommitResult struct {
	PackageID   string    `json:"package_id"`
	BatchID     string    `json:"bom_import_batch_id"`
	ContentHash string    `json:"content_hash"`
	ItemCount   int       `json:"item_count"`
	Idempotent  bool      `json:"idempotent"`
	CommittedAt time.Time `json:"committed_at"`
}

type BOMBuildError struct {
	Issue BOMPackageIssue
	Cause error
}

func (e *BOMBuildError) Error() string { return e.Cause.Error() }
func wrapBOMBuildError(row BOMPackageRow, field string, err error) error {
	if err == nil {
		return nil
	}
	code := "reference_invalid"
	var apiErr *Error
	if errors.As(err, &apiErr) {
		code = strings.ToLower(apiErr.Code)
	}
	return &BOMBuildError{Issue: BOMPackageIssue{Severity: "BLOCK", File: row.File, BOMLineNo: row.Line, Field: field, Code: code}, Cause: err}
}

type bomMaterial struct {
	row                                                                        BOMPackageRow
	category, uid, serial, parentUID, machineUID, endpointA, endpointB, source string
	position                                                                   map[string]string
}

type bomResolver struct {
	ctx       context.Context
	tx        *sql.Tx
	projectID string
	lines     map[string]*bomMaterial
	business  map[string][]*bomMaterial
	byUID     map[string]*bomMaterial
}

func newBOMResolver(ctx context.Context, tx *sql.Tx, projectID string) *bomResolver {
	return &bomResolver{ctx: ctx, tx: tx, projectID: projectID, lines: map[string]*bomMaterial{}, business: map[string][]*bomMaterial{}, byUID: map[string]*bomMaterial{}}
}
func (r *bomResolver) register(m *bomMaterial) {
	r.lines[strconv.Itoa(m.row.Line)] = m
	r.byUID[m.uid] = m
	if m.serial != "" {
		r.business[m.serial] = append(r.business[m.serial], m)
	}
	if m.category == "port" {
		v := m.row.Values
		for _, token := range []string{v["port_name"], v["parent_ref"] + ":" + v["port_index"], v["parent_ref"] + ":" + v["port_name"]} {
			if token != "" && token != ":" {
				r.business[token] = append(r.business[token], m)
			}
		}
	}
}
func categoryAllowed(category string, allowed []string) bool {
	for _, want := range allowed {
		if category == want {
			return true
		}
	}
	return false
}
func (r *bomResolver) resolve(token string, samePackage bool, allowed ...string) (*bomMaterial, error) {
	if token == "" {
		return nil, apiError(409, "BOM_REFERENCE_MISSING", "Required BOM reference is empty")
	}
	if bomUIDPattern.MatchString(token) {
		if m := r.byUID[token]; m != nil {
			if !categoryAllowed(m.category, allowed) {
				return nil, apiError(409, "BOM_REFERENCE_TYPE", "BOM reference has the wrong material type")
			}
			return m, nil
		}
		if samePackage {
			return nil, apiError(409, "BOM_REFERENCE_OUTSIDE_PACKAGE", "Cable endpoint must be a port in this package")
		}
		var category, source string
		var positionJSON []byte
		var machineUID sql.NullString
		err := r.tx.QueryRowContext(r.ctx, `SELECT i.category,i.source_of_record,i.position,coalesce((SELECT c.machine_id FROM components c WHERE c.id=i.item_uid),(SELECT p.machine_id FROM physical_ports p WHERE p.id=i.item_uid),'') FROM bom_items i WHERE i.item_uid=$1 AND EXISTS(SELECT 1 FROM bom_item_revisions v WHERE v.item_uid=i.item_uid AND v.project_id=$2)`, token, r.projectID).Scan(&category, &source, &positionJSON, &machineUID)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apiError(409, "BOM_REFERENCE_NOT_FOUND", "BOM reference is not in this project")
		}
		if err != nil {
			return nil, err
		}
		if !categoryAllowed(category, allowed) {
			return nil, apiError(409, "BOM_REFERENCE_TYPE", "BOM reference has the wrong material type")
		}
		m := &bomMaterial{uid: token, category: category, source: source, machineUID: machineUID.String, position: map[string]string{}}
		_ = json.Unmarshal(positionJSON, &m.position)
		if category == "server" {
			m.machineUID = token
		}
		return m, nil
	}
	if m := r.lines[token]; m != nil {
		if !categoryAllowed(m.category, allowed) {
			return nil, apiError(409, "BOM_REFERENCE_TYPE", "BOM line reference has the wrong material type")
		}
		return m, nil
	}
	candidates := map[string]*bomMaterial{}
	for _, m := range r.business[token] {
		if categoryAllowed(m.category, allowed) {
			candidates[m.uid] = m
		}
	}
	if len(candidates) == 1 {
		for _, m := range candidates {
			return m, nil
		}
	}
	if len(candidates) > 1 {
		return nil, apiError(409, "BOM_REFERENCE_AMBIGUOUS", "BOM business reference resolves to multiple items")
	}
	if samePackage {
		return nil, apiError(409, "BOM_REFERENCE_NOT_FOUND", "Cable endpoint must be a port in this package")
	}
	// External business references are limited to serialized project materials.
	rows, err := r.tx.QueryContext(r.ctx, `SELECT DISTINCT i.item_uid,i.category,i.source_of_record,i.position,coalesce((SELECT c.machine_id FROM components c WHERE c.id=i.item_uid),(SELECT p.machine_id FROM physical_ports p WHERE p.id=i.item_uid),'') FROM bom_items i JOIN bom_item_revisions v ON v.item_uid=i.item_uid WHERE v.project_id=$1 AND i.serial=$2`, r.projectID, token)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		m := &bomMaterial{position: map[string]string{}}
		var positionJSON []byte
		if err = rows.Scan(&m.uid, &m.category, &m.source, &positionJSON, &m.machineUID); err != nil {
			return nil, err
		}
		if !categoryAllowed(m.category, allowed) {
			continue
		}
		_ = json.Unmarshal(positionJSON, &m.position)
		if m.category == "server" {
			m.machineUID = m.uid
		}

		candidates[m.uid] = m
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(candidates) == 1 {
		for _, m := range candidates {
			return m, nil
		}
	}
	if len(candidates) > 1 {
		return nil, apiError(409, "BOM_REFERENCE_AMBIGUOUS", "BOM business reference resolves to multiple items")
	}
	return nil, apiError(409, "BOM_REFERENCE_NOT_FOUND", "BOM business reference not found in this project")
}

func selectMaterialUID(ctx context.Context, tx *sql.Tx, category, serial, parentUID, portIndex, a, b string) (string, error) {
	var uid string
	var err error
	switch category {
	case "port":
		err = tx.QueryRowContext(ctx, `SELECT item_uid FROM bom_items WHERE category='port' AND parent_uid=$1 AND port_index=$2`, parentUID, portIndex).Scan(&uid)
	case "cable":
		err = tx.QueryRowContext(ctx, `SELECT item_uid FROM bom_items WHERE category='cable' AND endpoint_a_uid=$1 AND endpoint_b_uid=$2`, a, b).Scan(&uid)
	default:
		if serial == "" {
			return "", apiError(409, "BOM_IDENTITY_MISSING", "A serialized material needs a serial to retain its identity")
		}
		err = tx.QueryRowContext(ctx, `SELECT item_uid FROM bom_items WHERE category=$1 AND serial=$2`, category, serial).Scan(&uid)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return uid, err
}

func buildBOMMaterials(ctx context.Context, tx *sql.Tx, projectID string, pkg *BOMPackage) ([]*bomMaterial, error) {
	r := newBOMResolver(ctx, tx, projectID)
	ordered := []*bomMaterial{}
	files := []string{"server/servers.csv", "fabric/switches.csv", "server/server_components.csv", "server/server_ports.csv", "fabric/switch_ports.csv", "fabric/optic_modules.csv", "fabric/cables.csv"}
	for _, file := range files {
		for _, row := range pkg.Rows[file] {
			v := row.Values
			if (file == "server/servers.csv" || file == "fabric/switches.csv") && v["credential_batch_ref"] == "" && pkg.Manifest.CredentialBatchRef != "" {
				v["credential_batch_ref"] = pkg.Manifest.CredentialBatchRef
			}
			m := &bomMaterial{row: row, position: map[string]string{}}
			switch file {
			case "server/servers.csv":
				m.category = "server"
				m.serial = v["sn"]
				for _, key := range []string{"dc", "pod", "rack", "u"} {
					if v[key] != "" {
						m.position[key] = v[key]
					}
				}
			case "fabric/switches.csv":
				m.category = "switch"
				m.serial = v["sn"]
				for _, key := range []string{"dc", "pod", "role", "rack", "idx"} {
					if v[key] != "" {
						m.position[key] = v[key]
					}
				}
			case "server/server_components.csv":
				m.category = v["category"]
				m.serial = v["serial"]
			case "server/server_ports.csv", "fabric/switch_ports.csv":
				m.category = "port"
			case "fabric/optic_modules.csv":
				m.category = "optic_module"
				m.serial = v["serial"]
			case "fabric/cables.csv":
				m.category = "cable"
			}
			switch file {
			case "server/server_components.csv":
				parent, err := r.resolve(v["parent_sn"], false, "server")
				if err != nil {
					return nil, wrapBOMBuildError(row, "parent_sn", err)
				}
				m.parentUID = parent.uid
				m.machineUID = parent.machineUID
				m.position = cloneBOMPosition(parent.position)
				m.source = parent.source
				m.position["slot"] = v["slot"]
			case "server/server_ports.csv":
				parent, err := r.resolve(v["parent_ref"], false, "server", "nic", "gpu", "psu", "bmc")
				if err != nil {
					return nil, wrapBOMBuildError(row, "parent_ref", err)
				}
				m.parentUID = parent.uid
				m.machineUID = parent.machineUID
				m.position = cloneBOMPosition(parent.position)
				m.source = parent.source
				if m.machineUID == "" {
					return nil, wrapBOMBuildError(row, "parent_ref", apiError(409, "BOM_REFERENCE_TYPE", "Server port parent has no machine identity"))
				}
			case "fabric/switch_ports.csv":
				parent, err := r.resolve(v["parent_ref"], false, "switch")
				if err != nil {
					return nil, wrapBOMBuildError(row, "parent_ref", err)
				}
				m.parentUID = parent.uid
				m.position = cloneBOMPosition(parent.position)
				m.source = parent.source
			case "fabric/optic_modules.csv":
				parent, err := r.resolve(v["parent_ref"], false, "port")
				if err != nil {
					return nil, wrapBOMBuildError(row, "parent_ref", err)
				}
				if parent.machineUID != "" {
					return nil, wrapBOMBuildError(row, "parent_ref", apiError(409, "BOM_REFERENCE_TYPE", "Fabric optic must belong to a switch port"))
				}
				m.parentUID = parent.uid
				m.position = cloneBOMPosition(parent.position)
				m.source = parent.source
			case "fabric/cables.csv":
				a, err := r.resolve(v["endpoint_a"], true, "port")
				if err != nil {
					return nil, wrapBOMBuildError(row, "endpoint_a", err)
				}
				b, err := r.resolve(v["endpoint_b"], true, "port")
				if err != nil {
					return nil, wrapBOMBuildError(row, "endpoint_b", err)
				}
				if a.uid == b.uid {
					return nil, wrapBOMBuildError(row, "endpoint_b", apiError(409, "BOM_CABLE_ENDPOINTS", "Cable endpoints must be different"))
				}
				m.endpointA, m.endpointB = a.uid, b.uid
				if m.endpointA > m.endpointB {
					m.endpointA, m.endpointB = m.endpointB, m.endpointA
				}
			}
			if m.category == "server" {
				m.machineUID = m.uid
			}
			uid, err := selectMaterialUID(ctx, tx, m.category, m.serial, m.parentUID, v["port_index"], m.endpointA, m.endpointB)
			if err != nil {
				return nil, wrapBOMBuildError(row, "serial", err)
			}
			// A wide-sheet NIC has only a permanent MAC. When the standard
			// package later supplies its real serial, adopt that provisional
			// identity. Multiple provisional matches need explicit reconciliation.
			if uid == "" && m.category == "nic" && v["mac"] != "" {
				seen := map[string]bool{}
				for _, rawMAC := range strings.Split(v["mac"], ",") {
					mac, ok := normalizeMAC(rawMAC)
					if !ok {
						continue
					}
					var candidate, machine string
					err = tx.QueryRowContext(ctx, `SELECT i.item_uid,c.machine_id FROM bom_items i JOIN components c ON c.id=i.item_uid WHERE i.category='nic' AND i.serial LIKE 'LEGACY-MAC-%' AND i.mac_list @> $1::jsonb`, `["`+mac+`"]`).Scan(&candidate, &machine)
					if err != nil && !errors.Is(err, sql.ErrNoRows) {
						return nil, err
					}
					if err == nil {
						if machine != m.machineUID {
							return nil, wrapBOMBuildError(row, "mac", apiError(409, "BOM_MAC_CONFLICT", "Provisional NIC MAC belongs to another server"))
						}
						seen[candidate] = true
					}
				}
				if len(seen) > 1 {
					return nil, wrapBOMBuildError(row, "mac", apiError(409, "BOM_NIC_IDENTITY_AMBIGUOUS", "Multiple provisional NICs match one serialized NIC"))
				}
				for candidate := range seen {
					uid = candidate
				}
			}
			if uid == "" && m.category == "server" {
				err = tx.QueryRowContext(ctx, `SELECT id FROM machines WHERE serial_number=$1`, m.serial).Scan(&uid)
				if err != nil && !errors.Is(err, sql.ErrNoRows) {
					return nil, err
				}
			}
			if m.category == "port" && m.machineUID != "" && v["mac"] != "" {
				var existingID string
				var existingMachine, existingParent, existingIndex sql.NullString
				err = tx.QueryRowContext(ctx, `SELECT id,machine_id,parent_uid,port_index FROM physical_ports WHERE mac_address=$1`, v["mac"]).Scan(&existingID, &existingMachine, &existingParent, &existingIndex)
				if err != nil && !errors.Is(err, sql.ErrNoRows) {
					return nil, err
				}
				if err == nil {
					provisionalIndexChange := false
					if existingParent.Valid && existingParent.String == m.parentUID && existingIndex.String != v["port_index"] {
						var provisional bool
						checkErr := tx.QueryRowContext(ctx, `SELECT serial LIKE 'LEGACY-MAC-%' FROM bom_items WHERE item_uid=$1 AND category='nic'`, m.parentUID).Scan(&provisional)
						if checkErr != nil && !errors.Is(checkErr, sql.ErrNoRows) {
							return nil, checkErr
						}
						provisionalIndexChange = checkErr == nil && provisional
					}
					if existingMachine.String != m.machineUID || existingParent.Valid && (existingParent.String != m.parentUID || existingIndex.String != v["port_index"] && !provisionalIndexChange) || uid != "" && uid != existingID {
						return nil, wrapBOMBuildError(row, "mac", apiError(409, "BOM_MAC_CONFLICT", "Permanent MAC belongs to a different physical port"))
					}
					if uid == "" {
						uid = existingID
					}
				}
			}
			if uid == "" {
				uid = RandomID()
			}
			m.uid = uid
			if m.category == "server" {
				m.machineUID = uid
			}
			if v["source_of_record"] != "" {
				m.source = v["source_of_record"]
			}
			if m.source == "" {
				m.source = "design_bom"
			}
			r.register(m)
			ordered = append(ordered, m)
		}
	}
	return ordered, nil
}
func cloneBOMPosition(src map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range src {
		out[k] = v
	}
	return out
}

// CommitBOMPackage records planned physical material and its immutable batch
// fact. It does not claim N-S5 verification, PortModel confirmation or NP-3:
// only G2's later three-way comparison can produce that evidence.
func (s *Store) CommitBOMPackage(ctx context.Context, a *AuthSession, project *ProjectSummary, files map[string][]byte, previewToken, trace string) (*BOMPackageCommitResult, error) {
	if !validBOMPreviewToken(a, project.ID, files, previewToken) {
		return nil, apiError(409, "BOM_PREVIEW_REQUIRED", "Run a successful read-only preview for this exact package before commit")
	}
	tx, err := s.beginControl(ctx, a)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = requireProjectAdmin(ctx, tx, a, project.ID); err != nil {
		return nil, err
	}
	if err = lockKey(ctx, tx, "bom-package:"+project.ID); err != nil {
		return nil, err
	}
	pkg, preview, err := analyzeBOMPackageTx(ctx, tx, project, files)
	if err != nil {
		return nil, err
	}
	if preview.Idempotent {
		result := &BOMPackageCommitResult{BatchID: pkg.Manifest.BatchID, ContentHash: pkg.Manifest.ContentHash, Idempotent: true}
		if err = tx.QueryRowContext(ctx, `SELECT id,committed_at,(SELECT count(*) FROM bom_item_revisions WHERE package_id=p.id) FROM bom_import_packages p WHERE project_id=$1 AND bom_import_batch_id=$2`, project.ID, pkg.Manifest.BatchID).Scan(&result.PackageID, &result.CommittedAt, &result.ItemCount); err != nil {
			return nil, err
		}
		return result, tx.Commit()
	}
	if !preview.CanCommit {
		return nil, apiError(409, "BOM_PRECHECK_BLOCKED", "BOM package has blocking issues; run preview and correct them")
	}
	materials, err := buildBOMMaterials(ctx, tx, project.ID, pkg)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	result := &BOMPackageCommitResult{PackageID: RandomID(), BatchID: pkg.Manifest.BatchID, ContentHash: pkg.Manifest.ContentHash, ItemCount: len(materials), CommittedAt: now}
	manifestJSON, err := json.Marshal(pkg.Manifest)
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO bom_import_packages(id,project_id,bom_import_batch_id,schema_version,profile_version_ref,content_hash,declared_by,manifest,row_count,state,uploaded_by,committed_at,network_baseline_ref,credential_batch_ref) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'committed',$10,$11,$12,$13)`, result.PackageID, project.ID, pkg.Manifest.BatchID, pkg.Manifest.SchemaVersion, pkg.Manifest.ProfileVersionRef, pkg.Manifest.ContentHash, pkg.Manifest.DeclaredBy, manifestJSON, pkg.Manifest.RowCount, a.User.ID, now, nullBOMString(pkg.Manifest.NetworkBaselineRef), nullBOMString(pkg.Manifest.CredentialBatchRef))
	if err != nil {
		return nil, err
	}
	for _, decl := range pkg.Manifest.DomainFiles {
		cols, _ := json.Marshal(decl.Columns)
		hash := pkg.FileHashes[decl.File]
		if hash == "" {
			return nil, fmt.Errorf("missing hash for declared file %s", decl.File)
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO bom_import_manifest_files(package_id,file_path,domain,entity,columns,row_count,file_hash) VALUES($1,$2,$3,$4,$5,$6,$7)`, result.PackageID, decl.File, decl.Domain, decl.Entity, cols, len(pkg.Rows[decl.File]), hash)
		if err != nil {
			return nil, err
		}
	}
	for _, issue := range pkg.Issues {
		if issue.Severity == "BLOCK" {
			return nil, apiError(409, "BOM_PRECHECK_BLOCKED", "BOM package has blocking issues")
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO bom_import_issues(id,package_id,severity,file_path,bom_line_no,field,code,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, RandomID(), result.PackageID, issue.Severity, issue.File, nullBOMInt(issue.BOMLineNo), nullBOMString(issue.Field), issue.Code, now)
		if err != nil {
			return nil, err
		}
	}
	for _, row := range pkg.Rows["credential_batches.csv"] {
		v := row.Values
		_, err = tx.ExecContext(ctx, `INSERT INTO bom_credential_batches(project_id,credential_batch_ref,scope,rotation_policy,vault_path,owner,package_id,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)
ON CONFLICT(project_id,credential_batch_ref) DO UPDATE SET scope=excluded.scope,rotation_policy=excluded.rotation_policy,vault_path=excluded.vault_path,owner=excluded.owner,package_id=excluded.package_id,updated_at=excluded.updated_at`, project.ID, v["credential_batch_ref"], v["scope"], v["rotation_policy"], v["vault_path"], v["owner"], result.PackageID, now)
		if err != nil {
			return nil, err
		}
	}
	for _, m := range materials {
		if err = writeBOMMaterial(ctx, tx, project.ID, result.PackageID, m, now); err != nil {
			return nil, err
		}
	}
	if err = AppendBMCInventoryEventTx(ctx, tx, project.ID, "bom.committed", result.PackageID, "", ""); err != nil {
		return nil, err
	}
	if err = appendAudit(ctx, tx, a.User.ID, project.ID, "bom.package.commit", result.PackageID, trace, map[string]any{"batch_id": result.BatchID, "content_hash": result.ContentHash, "items": result.ItemCount}); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}
func nullBOMString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func nullBOMInt(n int) any {
	if n == 0 {
		return nil
	}
	return n
}
