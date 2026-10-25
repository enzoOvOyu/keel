package platform

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// BOM import (WP-1.7, FR-2.1). Two phases: Preview validates an entire batch
// and classifies conflicts without writing; Commit replays the same rows in
// one transaction, creating machines/ports and upserting project assets with
// model bindings. Machine identity is global; assets are per project and
// never overwrite a machine's historical project membership.

type BomPortRow struct {
	MacAddress string `json:"mac_address"`
	PortName   string `json:"port_name"`
	Rail       int    `json:"rail"`
}

type BomMachineRow struct {
	SerialNumber string        `json:"serial_number"`
	Rack         string        `json:"rack"`
	UnitPosition string        `json:"unit_position"`
	BmcAddress   string        `json:"bmc_address"`
	Hostname     string        `json:"hostname"`
	IpReference  string        `json:"ip_reference"`
	ModelName    string        `json:"model_name"`
	Ports        []BomPortRow  `json:"ports"`
}

type BomImportInput struct {
	FileVersion string          `json:"file_version"`
	Machines    []BomMachineRow `json:"machines"`
}

type BomPortIssue struct {
	MacAddress string `json:"mac_address"`
	Reason     string `json:"reason"`
}

type BomMachineIssue struct {
	SerialNumber string          `json:"serial_number"`
	Reason       string          `json:"reason"`
	Ports        []BomPortIssue  `json:"ports,omitempty"`
}

type BomPreview struct {
	Machines       int             `json:"machines"`
	Ports          int             `json:"ports"`
	NewMachines    int             `json:"new_machines"`
	ExistingMachines int          `json:"existing_machines"`
	NewAssets      int             `json:"new_assets"`
	UpdatedAssets  int             `json:"updated_assets"`
	Issues         []BomMachineIssue `json:"issues"`
	ModelNames     []string        `json:"model_names"`
}

type BomCommitResult struct {
	ImportID       string    `json:"import_id"`
	MachinesCreated int      `json:"machines_created"`
	PortsCreated   int       `json:"ports_created"`
	AssetsCreated  int       `json:"assets_created"`
	AssetsUpdated  int       `json:"assets_updated"`
	CommittedAt    time.Time `json:"committed_at"`
}

var (
	snPattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,119}$`)
	hostPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,251}[a-z0-9]$|^[a-z][a-z0-9]{0,1}$`)
)

func normalizeMAC(value string) (string, bool) {
	hw, e := net.ParseMAC(strings.TrimSpace(value))
	if e != nil {
		return "", false
	}
	return strings.ToLower(hw.String()), true
}

// validateBomRows checks every row and returns the classified issue list.
func validateBomRows(input BomImportInput) []BomMachineIssue {
	issues := []BomMachineIssue{}
	seenSN := map[string]bool{}
	seenMAC := map[string]string{}
	for _, machine := range input.Machines {
		issue := BomMachineIssue{SerialNumber: machine.SerialNumber, Ports: []BomPortIssue{}}
		if !snPattern.MatchString(machine.SerialNumber) {
			issue.Reason = "invalid_serial"
		} else if seenSN[machine.SerialNumber] {
			issue.Reason = "duplicate_serial_in_file"
		}
		seenSN[machine.SerialNumber] = true
		if machine.Hostname == "" || !hostPattern.MatchString(machine.Hostname) {
			if issue.Reason == "" {
				issue.Reason = "invalid_hostname"
			}
		}
		if len(machine.Ports) == 0 && issue.Reason == "" {
			issue.Reason = "no_ports"
		}
		for _, port := range machine.Ports {
			mac, ok := normalizeMAC(port.MacAddress)
			if !ok {
				issue.Ports = append(issue.Ports, BomPortIssue{MacAddress: port.MacAddress, Reason: "invalid_mac"})
				continue
			}
			if owner, dup := seenMAC[mac]; dup {
				reason := "duplicate_mac_in_file"
				if owner != machine.SerialNumber {
					reason = "mac_assigned_to_other_machine_in_file"
				}
				issue.Ports = append(issue.Ports, BomPortIssue{MacAddress: mac, Reason: reason})
				continue
			}
			seenMAC[mac] = machine.SerialNumber
		}
		if issue.Reason != "" || len(issue.Ports) > 0 {
			issues = append(issues, issue)
		}
	}
	return issues
}

// PreviewBomImport validates the batch against current database state without
// writing anything. Model names resolve per project scope so a typo surfaces
// as an issue instead of a silent unbound asset.
func (s *Store) PreviewBomImport(ctx context.Context, a *AuthSession, project *ProjectSummary, input BomImportInput) (*BomPreview, error) {
	scope := s.scopeKeyFor(project)
	if input.FileVersion == "" || len(input.FileVersion) > 120 {
		return nil, apiError(400, "VALIDATION_ERROR", "file_version is required")
	}
	if len(input.Machines) == 0 || len(input.Machines) > 2000 {
		return nil, apiError(400, "VALIDATION_ERROR", "machines must contain 1-2000 rows")
	}
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e := requireProjectAdmin(ctx, tx, a, project.ID); e != nil {
		return nil, e
	}
	preview := &BomPreview{Issues: []BomMachineIssue{}, ModelNames: []string{}}
	preview.Machines = len(input.Machines)
	macSet := map[string]bool{}
	for _, machine := range input.Machines {
		preview.Ports += len(machine.Ports)
		for _, port := range machine.Ports {
			if mac, ok := normalizeMAC(port.MacAddress); ok {
				macSet[mac] = true
			}
		}
	}
	// Machine existence by SN, batched.
	sns := make([]string, 0, len(input.Machines))
	for _, machine := range input.Machines {
		sns = append(sns, machine.SerialNumber)
	}
	rows, e := tx.QueryContext(ctx, "SELECT serial_number FROM machines WHERE serial_number = ANY($1)", sns)
	if e != nil {
		return nil, e
	}
	existing := map[string]bool{}
	for rows.Next() {
		var sn string
		if e = rows.Scan(&sn); e != nil {
			return nil, e
		}
		existing[sn] = true
	}
	rows.Close()
	// MAC conflicts against the database, batched.
	macs := make([]string, 0, len(macSet))
	for mac := range macSet {
		macs = append(macs, mac)
	}
	if len(macs) > 0 {
		rows, e = tx.QueryContext(ctx, `SELECT p.mac_address, m.serial_number FROM physical_ports p JOIN machines m ON m.id=p.machine_id WHERE p.mac_address = ANY($1)`, macs)
		if e != nil {
			return nil, e
		}
		for rows.Next() {
			var mac, sn string
			if e = rows.Scan(&mac, &sn); e != nil {
				return nil, e
			}
			for _, machine := range input.Machines {
				for _, port := range machine.Ports {
					normalized, ok := normalizeMAC(port.MacAddress)
					if ok && normalized == mac && sn != machine.SerialNumber {
						preview.Issues = append(preview.Issues, BomMachineIssue{SerialNumber: machine.SerialNumber, Reason: "", Ports: []BomPortIssue{{MacAddress: mac, Reason: "mac_exists_other_machine"}}, })
					}
				}
			}
		}
		rows.Close()
	}
	// Assets and hostname conflicts, batched.
	hostnames := make([]string, 0, len(input.Machines))
	for _, machine := range input.Machines {
		hostnames = append(hostnames, machine.Hostname)
	}
	rows, e = tx.QueryContext(ctx, "SELECT machine_id, hostname FROM project_assets WHERE project_id=$1 AND hostname = ANY($2)", project.ID, hostnames)
	if e != nil {
		return nil, e
	}
	for rows.Next() {
		var machineID, hostname string
		if e = rows.Scan(&machineID, &hostname); e != nil {
			return nil, e
		}
		_ = machineID
		preview.UpdatedAssets++
	}
	rows.Close()
	// Model name resolution per scope.
	modelSeen := map[string]bool{}
	for _, machine := range input.Machines {
		if machine.ModelName == "" || modelSeen[machine.ModelName] {
			continue
		}
		modelSeen[machine.ModelName] = true
		var count int
		if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM device_models WHERE scope_key=$1 AND name=$2 AND state='active'", scope, machine.ModelName).Scan(&count); e != nil {
			return nil, e
		}
		if count == 0 {
			preview.Issues = append(preview.Issues, BomMachineIssue{SerialNumber: machine.SerialNumber, Reason: "unknown_model:" + machine.ModelName})
			continue
		}
		preview.ModelNames = append(preview.ModelNames, machine.ModelName)
	}
	// File version idempotency.
	var versionCount int
	if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM bom_imports WHERE project_id=$1 AND file_version=$2", project.ID, input.FileVersion).Scan(&versionCount); e != nil {
		return nil, e
	}
	if versionCount > 0 {
		return nil, apiError(409, "BOM_VERSION_EXISTS", "This BOM file version was already imported")
	}
	for _, machine := range input.Machines {
		if existing[machine.SerialNumber] {
			preview.ExistingMachines++
		} else {
			preview.NewMachines++
		}
	}
	preview.NewAssets = len(input.Machines) - preview.UpdatedAssets
	if preview.NewAssets < 0 {
		preview.NewAssets = 0
	}
	// Merge file-level issues last so both classes appear together.
	fileIssues := validateBomRows(input)
	for _, db := range preview.Issues {
		fileIssues = append(fileIssues, db)
	}
	preview.Issues = fileIssues
	return preview, nil
}

// CommitBomImport applies a validated batch in one transaction. Rows that
// failed preview raise VALIDATION_ERROR; nothing is partially written.
func (s *Store) CommitBomImport(ctx context.Context, a *AuthSession, project *ProjectSummary, input BomImportInput, trace string) (*BomCommitResult, error) {
	scope := s.scopeKeyFor(project)
	if e := validateBomShape(input); e != nil {
		return nil, e
	}
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e := requireProjectAdmin(ctx, tx, a, project.ID); e != nil {
		return nil, e
	}
	if issues := validateBomRows(input); len(issues) > 0 {
		return nil, apiError(409, "BOM_VALIDATION_FAILED", "The batch contains conflicting rows; run a preview first")
	}
	// File version idempotency (re-checked under the write lock).
	var versionCount int
	if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM bom_imports WHERE project_id=$1 AND file_version=$2", project.ID, input.FileVersion).Scan(&versionCount); e != nil {
		return nil, e
	}
	if versionCount > 0 {
		return nil, apiError(409, "BOM_VERSION_EXISTS", "This BOM file version was already imported")
	}
	result := &BomCommitResult{ImportID: RandomID(), CommittedAt: time.Now().UTC()}
	for _, machine := range input.Machines {
		machineID, created, e := upsertMachine(ctx, tx, machine)
		if e != nil {
			return nil, e
		}
		if created {
			result.MachinesCreated++
		}
		for _, port := range machine.Ports {
			mac, ok := normalizeMAC(port.MacAddress)
			if !ok {
				return nil, apiError(409, "BOM_VALIDATION_FAILED", "invalid MAC "+port.MacAddress)
			}
			// A MAC may only (re)bind to the machine it already belongs to; a
			// silent DO NOTHING would hide the very conflict FR-2.1 rejects.
			var ownerID string
			e := tx.QueryRowContext(ctx, "SELECT machine_id FROM physical_ports WHERE mac_address=$1", mac).Scan(&ownerID)
			switch {
			case e == nil && ownerID != machineID:
				return nil, apiError(409, "BOM_VALIDATION_FAILED", "MAC "+mac+" already belongs to another machine")
			case e != nil && !errors.Is(e, sql.ErrNoRows):
				return nil, e
			}
			if ownerID == "" {
				if _, e = tx.ExecContext(ctx, `INSERT INTO physical_ports(id,machine_id,mac_address,port_name,rail,created_at)
					VALUES($1,$2,$3,$4,$5,$6)`,
					RandomID(), machineID, mac, port.PortName, port.Rail, result.CommittedAt); e != nil {
					var pg *pgconn.PgError
					if errors.As(e, &pg) && pg.Code == "23505" {
						return nil, apiError(409, "BOM_VALIDATION_FAILED", "MAC conflict on "+mac)
					}
					return nil, e
				}
				result.PortsCreated++
			}
		}
		var modelID any
		if machine.ModelName != "" {
			var id string
			if e = tx.QueryRowContext(ctx, "SELECT id FROM device_models WHERE scope_key=$1 AND name=$2 AND state='active'", scope, machine.ModelName).Scan(&id); e != nil {
				if errors.Is(e, sql.ErrNoRows) {
					return nil, apiError(409, "BOM_VALIDATION_FAILED", "unknown model "+machine.ModelName)
				}
				return nil, e
			}
			modelID = id
		}
		var version int64 = 1
		if e = tx.QueryRowContext(ctx, `INSERT INTO project_assets(id,project_id,machine_id,model_id,hostname,ip_reference,state,version,created_at,updated_at)
			VALUES($1,$2,$3,$4,$5,$6,'planned',1,$7,$7)
			ON CONFLICT(project_id,machine_id) DO UPDATE SET hostname=EXCLUDED.hostname, ip_reference=EXCLUDED.ip_reference, model_id=EXCLUDED.model_id, updated_at=EXCLUDED.updated_at, version=project_assets.version+1
			RETURNING version`,
			RandomID(), project.ID, machineID, modelID, machine.Hostname, machine.IpReference, result.CommittedAt).Scan(&version); e != nil {
			var pg *pgconn.PgError
			if errors.As(e, &pg) && pg.Code == "23505" {
				return nil, apiError(409, "BOM_VALIDATION_FAILED", "hostname conflict on "+machine.Hostname)
			}
			return nil, e
		}
		if version == 1 {
			result.AssetsCreated++
		} else {
			result.AssetsUpdated++
		}
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO bom_imports(id,project_id,file_version,imported_by,machines_created,ports_created,assets_created,assets_updated,committed_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		result.ImportID, project.ID, input.FileVersion, a.User.ID, result.MachinesCreated, result.PortsCreated, result.AssetsCreated, result.AssetsUpdated, result.CommittedAt); e != nil {
		var pg *pgconn.PgError
		if errors.As(e, &pg) && pg.Code == "23505" {
			return nil, apiError(409, "BOM_VERSION_EXISTS", "This BOM file version was already imported")
		}
		return nil, e
	}
	if e = appendAudit(ctx, tx, a.User.ID, project.ID, "bom.import", result.ImportID, trace, map[string]any{
		"file_version": input.FileVersion, "machines": len(input.Machines),
		"machines_created": result.MachinesCreated, "ports_created": result.PortsCreated,
		"assets_created": result.AssetsCreated, "assets_updated": result.AssetsUpdated,
	}); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return result, nil
}

func validateBomShape(input BomImportInput) error {
	if input.FileVersion == "" || len(input.FileVersion) > 120 {
		return apiError(400, "VALIDATION_ERROR", "file_version is required")
	}
	if len(input.Machines) == 0 || len(input.Machines) > 2000 {
		return apiError(400, "VALIDATION_ERROR", "machines must contain 1-2000 rows")
	}
	return nil
}

func upsertMachine(ctx context.Context, tx *sql.Tx, machine BomMachineRow) (string, bool, error) {
	var id string
	if e := tx.QueryRowContext(ctx, "SELECT id FROM machines WHERE serial_number=$1", machine.SerialNumber).Scan(&id); e == nil {
		if _, e = tx.ExecContext(ctx, "UPDATE machines SET rack=$2, unit_position=$3, bmc_address=$4, updated_at=$5 WHERE id=$1",
			id, machine.Rack, machine.UnitPosition, machine.BmcAddress, time.Now().UTC()); e != nil {
			return "", false, e
		}
		return id, false, nil
	} else if !errors.Is(e, sql.ErrNoRows) {
		return "", false, e
	}
	id = RandomID()
	if _, e := tx.ExecContext(ctx, `INSERT INTO machines(id,serial_number,rack,unit_position,bmc_address,created_at,updated_at)
		VALUES($1,$2,$3,$4,$5,$6,$6)`,
		id, machine.SerialNumber, machine.Rack, machine.UnitPosition, machine.BmcAddress, time.Now().UTC()); e != nil {
		return "", false, e
	}
	return id, true, nil
}

// ListProjectAssets returns the project's machines with model bindings.
func (s *Store) ListProjectAssets(ctx context.Context, a *AuthSession, project *ProjectSummary, p Pagination) (Page[ProjectAssetSummary], error) {
	out := NewPage[ProjectAssetSummary](p)
	if e := p.Validate(); e != nil {
		return out, e
	}
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	if e := requireProjectMember(ctx, tx, a, project.ID); e != nil {
		return out, e
	}
	if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM project_assets WHERE project_id=$1`, project.ID).Scan(&out.Total); e != nil {
		return out, e
	}
	rows, e := tx.QueryContext(ctx, `SELECT pa.id,pa.machine_id,m.serial_number,pa.hostname,pa.ip_reference,pa.state,pa.version,
			COALESCE(dm.name,''), m.rack, m.unit_position, m.ssh_trust, COALESCE(dm.id,''), COALESCE(mv.id,''), COALESCE(mv.version,0)
		FROM project_assets pa
		JOIN machines m ON m.id=pa.machine_id
		LEFT JOIN device_models dm ON dm.id=pa.model_id AND (dm.scope_key='global' OR dm.scope_key='project:'||pa.project_id)
		LEFT JOIN device_model_versions mv ON mv.id=pa.model_version_id AND mv.model_id=dm.id
		WHERE pa.project_id=$1
		ORDER BY pa.hostname LIMIT $2 OFFSET $3`, project.ID, p.PageSize, p.Offset())
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var item ProjectAssetSummary
		if e = rows.Scan(&item.ID, &item.MachineID, &item.SerialNumber, &item.Hostname, &item.IpReference, &item.State, &item.Version, &item.ModelName, &item.Rack, &item.UnitPosition, &item.SshTrust, &item.ModelID, &item.ModelVersionID, &item.ModelVersion); e != nil {
			return out, e
		}
		out.Items = append(out.Items, item)
	}
	if e = rows.Err(); e != nil {
		return out, e
	}
	if out.Items == nil {
		out.Items = []ProjectAssetSummary{}
	}
	return out, nil
}

type ProjectAssetSummary struct {
	ID             string `json:"id"`
	MachineID      string `json:"machine_id"`
	SerialNumber   string `json:"serial_number"`
	Hostname       string `json:"hostname"`
	IpReference    string `json:"ip_reference"`
	State          string `json:"state"`
	Version        int64  `json:"version"`
	ModelName      string `json:"model_name"`
	ModelID        string `json:"model_id"`
	ModelVersionID string `json:"model_version_id"`
	ModelVersion   int64  `json:"model_version"`
	Rack           string `json:"rack"`
	UnitPosition   string `json:"unit_position"`
	SshTrust       string `json:"ssh_trust"`
}
