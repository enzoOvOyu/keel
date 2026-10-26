package platform

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"time"

	"github.com/enzoOvOyu/keel/internal/provisionseed"
	"github.com/enzoOvOyu/keel/internal/taskrunner"
)

type ProvisionBootPort struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	MAC  string `json:"mac"`
}
type ProvisionTarget struct {
	ProjectID        string              `json:"project_id"`
	AssetID          string              `json:"asset_id"`
	MachineID        string              `json:"machine_id"`
	AssetVersion     int64               `json:"asset_version"`
	Serial           string              `json:"serial_number"`
	Hostname         string              `json:"hostname"`
	State            string              `json:"state"`
	ModelVersionID   string              `json:"model_version_id"`
	ModelContentHash string              `json:"model_content_hash"`
	Arch             string              `json:"arch"`
	Ports            []ProvisionBootPort `json:"ports"`
}
type ProvisionPreviewInput struct {
	AssetVersion int64  `json:"asset_version"`
	BootPortID   string `json:"boot_port_id"`
	MediaID      string `json:"media_id"`
	DomainKeyID  string `json:"domain_key_id"`
}
type ProvisionPreview struct {
	Schema         string                       `json:"schema"`
	SnapshotSHA256 string                       `json:"snapshot_sha256"`
	Target         ProvisionTarget              `json:"target"`
	BootPort       ProvisionBootPort            `json:"boot_port"`
	DomainKey      taskrunner.InstallCredential `json:"domain_key"`
	Media          MediaBootPlan                `json:"media"`
	Seed           provisionseed.Seed           `json:"seed"`
}

func provisionTargetTx(ctx context.Context, tx *sql.Tx, a *AuthSession, project *ProjectSummary, asset string) (*ProvisionTarget, error) {
	if project == nil || !modelIDPattern.MatchString(asset) {
		return nil, ErrNotFound
	}
	if err := requireProvisionAuthority(ctx, tx, a, project.ID, false); err != nil {
		return nil, err
	}
	out := &ProvisionTarget{ProjectID: project.ID, Ports: []ProvisionBootPort{}}
	var spec []byte
	err := tx.QueryRowContext(ctx, `SELECT pa.id,pa.machine_id,pa.version,m.serial_number,pa.hostname,pa.state,v.id,v.content_hash,v.hw_spec
 FROM project_assets pa JOIN machines m ON m.id=pa.machine_id
 JOIN device_model_versions v ON v.id=pa.model_version_id AND v.model_id=pa.model_id
 JOIN device_models dm ON dm.id=v.model_id AND dm.kind IN('server','compute-tray') AND dm.scope_key IN('global',$3)
 WHERE pa.project_id=$1 AND pa.id=$2 FOR SHARE OF pa,m`, project.ID, asset, "project:"+project.ID).Scan(&out.AssetID, &out.MachineID, &out.AssetVersion, &out.Serial, &out.Hostname, &out.State, &out.ModelVersionID, &out.ModelContentHash, &spec)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apiError(409, "PROVISION_MODEL_REQUIRED", "A current project asset with a pinned published server model is required")
	}
	if err != nil {
		return nil, err
	}
	var hardware struct {
		CPUArch string `json:"cpu_arch"`
	}
	if err = json.Unmarshal(spec, &hardware); err != nil {
		return nil, err
	}
	out.Arch = hardware.CPUArch
	if (out.Arch != "amd64" && out.Arch != "arm64") || !provisionseed.ValidHostname(out.Hostname) || !snPattern.MatchString(out.Serial) {
		return nil, apiError(422, "PROVISION_BOM_INVALID", "BOM requires a 1-63 character DNS hostname, serial and concrete model CPU architecture")
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,port_name,mac_address FROM physical_ports WHERE machine_id=$1 ORDER BY port_name,id LIMIT 513 FOR SHARE`, out.MachineID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var p ProvisionBootPort
		if err = rows.Scan(&p.ID, &p.Name, &p.MAC); err != nil {
			return nil, err
		}
		out.Ports = append(out.Ports, p)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(out.Ports) == 0 || len(out.Ports) > 512 {
		return nil, apiError(422, "PROVISION_PORT_REQUIRED", "BOM requires 1-512 explicit physical boot port candidates")
	}
	return out, nil
}

func (s *Store) GetProvisionTarget(ctx context.Context, a *AuthSession, project *ProjectSummary, asset string) (*ProvisionTarget, error) {
	tx, err := s.beginPinnedRead(ctx, a)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	out, err := provisionTargetTx(ctx, tx, a, project, asset)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

// PreviewProvisionSeed binds only existing BOM identities and approved versions.
// No callback token, DHCP lease, execution, asset state or trust proof is issued.
func (s *Store) PreviewProvisionSeed(ctx context.Context, a *AuthSession, project *ProjectSummary, asset string, in ProvisionPreviewInput, trace string) (out *ProvisionPreview, resultErr error) {
	if in.AssetVersion < 1 || !modelIDPattern.MatchString(in.BootPortID) || !modelIDPattern.MatchString(in.MediaID) || !modelIDPattern.MatchString(in.DomainKeyID) {
		return nil, apiError(400, "VALIDATION_ERROR", "Exact asset version, boot port, OS media and project key IDs are required")
	}
	tx, err := s.beginPinnedRead(ctx, a)
	if err != nil {
		return nil, err
	}
	defer func() { tx.Rollback(); resultErr = s.RecordMediaIntegrityFailure(ctx, resultErr, a.User.ID, trace) }()
	out, err = s.FreezeProvisionTx(ctx, tx, a, project, asset, in, "")
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

// FreezeProvisionTx is shared by preview and durable request creation. All
// facts and rendered bytes are checked in the caller's transaction. A nonempty
// instanceID must be the fresh durable request ID, never a cached preview ID.
func (s *Store) FreezeProvisionTx(ctx context.Context, tx *sql.Tx, a *AuthSession, project *ProjectSummary, asset string, in ProvisionPreviewInput, instanceID string) (*ProvisionPreview, error) {
	return s.freezeProvisionUntilTx(ctx, tx, a, project, asset, in, instanceID, time.Now().Add(time.Hour))
}
func (s *Store) freezeProvisionUntilTx(ctx context.Context, tx *sql.Tx, a *AuthSession, project *ProjectSummary, asset string, in ProvisionPreviewInput, instanceID string, until time.Time) (*ProvisionPreview, error) {
	if in.AssetVersion < 1 || !modelIDPattern.MatchString(in.BootPortID) || !modelIDPattern.MatchString(in.MediaID) || !modelIDPattern.MatchString(in.DomainKeyID) || (instanceID != "" && !modelIDPattern.MatchString(instanceID)) {
		return nil, apiError(400, "VALIDATION_ERROR", "Exact provision identities required")
	}
	target, err := provisionTargetTx(ctx, tx, a, project, asset)
	if err != nil {
		return nil, err
	}
	if target.AssetVersion != in.AssetVersion {
		return nil, errVersion
	}
	var port ProvisionBootPort
	for _, p := range target.Ports {
		if p.ID == in.BootPortID {
			port = p
			break
		}
	}
	mac, e := net.ParseMAC(port.MAC)
	if e != nil || len(mac) != 6 || mac[0]&1 != 0 || port.MAC == "00:00:00:00:00:00" {
		return nil, apiError(422, "PROVISION_BOOT_PORT_INVALID", "Select an existing unicast BOM port belonging to this machine")
	}
	plan, err := s.resolveMediaBootPlanTx(ctx, tx, project, in.MediaID)
	if err != nil {
		return nil, err
	}
	if plan.Artifacts[0].Arch != target.Arch {
		return nil, apiError(422, "PROVISION_ARCH_MISMATCH", "OS boot architecture differs from the bound model version")
	}
	key, err := ReadSSHInstallCredential(ctx, tx, project.ID, taskrunner.InstallCredential{ID: in.DomainKeyID}, until)
	if err != nil {
		return nil, err
	}
	template, err := mediaDependencyRecord(ctx, tx, plan.Profile.TemplateMediaID)
	if err != nil {
		return nil, err
	}
	if template.Size > provisionseed.MaxTemplateBytes {
		return nil, apiError(422, "PROVISION_TEMPLATE_INVALID", "Installer template exceeds 64 KiB")
	}
	file, err := s.checkedMediaFile(ctx, tx, &template)
	if err != nil {
		return nil, err
	}
	defer file.close()
	raw, err := io.ReadAll(io.LimitReader(file.file, provisionseed.MaxTemplateBytes+1))
	if err != nil {
		return nil, err
	}
	// The preview instance ID is deterministic; an actual reinstall must use a
	// fresh durable provisioning request ID instead of reusing this preview ID.
	snapshot, err := json.Marshal(struct {
		Target *ProvisionTarget
		Port   ProvisionBootPort
		Plan   *MediaBootPlan
		Key    *taskrunner.InstallCredential
	}{target, port, plan, key})
	if err != nil {
		return nil, err
	}
	identityHash := sha256.Sum256(snapshot)
	if instanceID == "" {
		instanceID = hex.EncodeToString(identityHash[:16])
	}
	seed, err := provisionseed.Render(raw, provisionseed.Identity{ProjectID: project.ID, MachineID: target.MachineID, InstanceID: instanceID, Hostname: target.Hostname, PublicKey: key.PublicKey})
	if err != nil {
		return nil, apiError(422, "PROVISION_TEMPLATE_INVALID", err.Error())
	}
	out := &ProvisionPreview{Schema: "keel.provision-preview/v1", Target: *target, BootPort: port, DomainKey: *key, Media: *plan, Seed: *seed}
	encoded, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(encoded)
	out.SnapshotSHA256 = hex.EncodeToString(hash[:])
	return out, nil
}
