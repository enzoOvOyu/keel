package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"time"

	"github.com/enzoOvOyu/keel/internal/mediaboot"
	"github.com/enzoOvOyu/keel/internal/taskrunner"
)

var ErrProvisionCanaryRequired = apiError(409, "PROVISION_CANARY_REQUIRED", "The exact OS/model rollout requires all selected canaries to finish with verified SSH and settled native success")

type ProvisionRollout struct {
	Mode           string   `json:"mode"`
	IdentitySHA256 string   `json:"identity_sha256"`
	CanaryIDs      []string `json:"canary_ids"`
}
type ProvisionCanary struct {
	InstallationID string `json:"installation_id"`
	MachineID      string `json:"machine_id"`
	Hostname       string `json:"hostname"`
	Serial         string `json:"serial_number"`
	State          string `json:"state"`
	Verified       bool   `json:"verified"`
}
type ProvisionRolloutPreview struct {
	IdentitySHA256           string            `json:"identity_sha256"`
	CanarySelectionAvailable bool              `json:"canary_selection_available"`
	ReleaseReady             bool              `json:"release_ready"`
	Canaries                 []ProvisionCanary `json:"canaries"`
}

// CanonicalProvisionRollout requires explicit canary or proven rollout intent.
// Missing evidence is never silently interpreted as a successful canary.
func CanonicalProvisionRollout(mode string, ids []string) ([]string, error) {
	if mode != "canary" && mode != "validated" {
		return nil, apiError(400, "VALIDATION_ERROR", "Explicit canary or validated rollout mode required")
	}
	if (mode == "canary" && len(ids) != 0) || (mode == "validated" && (len(ids) < 1 || len(ids) > 2)) {
		return nil, apiError(400, "VALIDATION_ERROR", "Canary mode takes no proofs; validated mode requires 1-2 exact canary installations")
	}
	out := append([]string{}, ids...)
	sort.Strings(out)
	for i, id := range out {
		if !modelIDPattern.MatchString(id) || (i > 0 && id == out[i-1]) {
			return nil, apiError(400, "VALIDATION_ERROR", "Unique exact canary installation IDs required")
		}
	}
	return out, nil
}

// Include the complete frozen role closure and target/model contract. Per-node
// serial, address and domain secret are intentionally not an OS release key.
func provisionRolloutIdentity(request *ProvisionRequest) (string, error) {
	if request == nil || !ProvisionSnapshotValid(request) || request.Snapshot.Media.ValidateInstallable() != nil || !modelIDPattern.MatchString(request.Snapshot.Target.ModelVersionID) || !sha256Pattern.MatchString(request.Snapshot.Target.ModelContentHash) || (request.Snapshot.Target.Arch != "amd64" && request.Snapshot.Target.Arch != "arm64") {
		return "", ErrProvisionCanaryRequired
	}
	plan := request.Snapshot.Media
	plan.Artifacts = append([]mediaboot.Artifact(nil), plan.Artifacts...)
	sort.Slice(plan.Artifacts, func(i, j int) bool { return plan.Artifacts[i].Role < plan.Artifacts[j].Role })
	identity := struct {
		Schema      string         `json:"schema"`
		Project     string         `json:"project_id"`
		Model       string         `json:"model_version_id"`
		ModelSHA256 string         `json:"model_sha256"`
		Arch        string         `json:"arch"`
		Media       mediaboot.Plan `json:"media"`
	}{"keel.provision-rollout/v1", request.ProjectID, request.Snapshot.Target.ModelVersionID, request.Snapshot.Target.ModelContentHash, request.Snapshot.Target.Arch, plan}
	raw, err := json.Marshal(identity)
	if err != nil {
		return "", err
	}
	return digest(string(raw)), nil
}

func readProvisionRolloutTx(ctx context.Context, tx *sql.Tx, id string) (*ProvisionRollout, error) {
	out := &ProvisionRollout{}
	var raw []byte
	err := tx.QueryRowContext(ctx, `SELECT mode,identity_sha256,canary_ids FROM provision_installation_rollouts WHERE installation_id=$1`, id).Scan(&out.Mode, &out.IdentitySHA256, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrProvisionCanaryRequired
	}
	if err != nil {
		return nil, err
	}
	if json.Unmarshal(raw, &out.CanaryIDs) != nil {
		return nil, ErrProvisionCanaryRequired
	}
	ids, err := CanonicalProvisionRollout(out.Mode, out.CanaryIDs)
	if err != nil {
		return nil, ErrProvisionCanaryRequired
	}
	out.CanaryIDs = ids
	return out, nil
}

// Latest attempt per explicitly selected canary machine. A failed/unknown latest
// attempt cannot be hidden by selecting an older successful one. Once rollout
// begins, the group is frozen against further canary admissions.
func provisionCanariesTx(ctx context.Context, tx *sql.Tx, project, identity string) ([]ProvisionCanary, error) {
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT ON(x.machine_id) x.id,x.machine_id,p.snapshot->'target'->>'hostname',p.snapshot->'target'->>'serial_number',x.state
 FROM provision_installation_rollouts r JOIN provision_installations x ON x.id=r.installation_id AND x.project_id=r.project_id JOIN provision_requests p ON p.id=x.request_id
 WHERE r.project_id=$1 AND r.identity_sha256=$2 AND r.mode='canary'
 ORDER BY x.machine_id,r.created_at DESC,r.installation_id DESC`, project, identity)
	if err != nil {
		return nil, err
	}
	out := []ProvisionCanary{}
	for rows.Next() {
		var item ProvisionCanary
		if err = rows.Scan(&item.InstallationID, &item.MachineID, &item.Hostname, &item.Serial, &item.State); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(out) > 2 {
		return nil, ErrProvisionCanaryRequired
	}
	for i := range out {
		if out[i].State != "registered" {
			continue
		}
		err = validateProvisionCanaryProofTx(ctx, tx, project, out[i].InstallationID, identity)
		if errors.Is(err, ErrProvisionCanaryRequired) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out[i].Verified = true
	}
	return out, nil
}

// Verify immutable committed proof, rather than trusting a green status string.
// Current install authority/media approval is checked separately for each target.
func validateProvisionCanaryProofTx(ctx context.Context, tx *sql.Tx, project, id, identity string) error {
	x, err := LoadProvisionInstallationBindingTx(ctx, tx, project, id)
	if err != nil {
		return err
	}
	if x.State != "registered" || x.AttemptID == nil || x.CommandID == nil || x.FencingToken == nil {
		return ErrProvisionCanaryRequired
	}
	selection, err := readProvisionRolloutTx(ctx, tx, id)
	if err != nil {
		return err
	}
	if selection.Mode != "canary" || selection.IdentitySHA256 != identity {
		return ErrProvisionCanaryRequired
	}
	request, err := scanProvisionRequest(tx.QueryRowContext(ctx, "SELECT "+provisionRequestColumns+" FROM provision_requests WHERE project_id=$1 AND id=$2", project, x.RequestID))
	if err != nil {
		return err
	}
	actual, err := provisionRolloutIdentity(request)
	if err != nil {
		return err
	}
	if actual != identity {
		return ErrProvisionCanaryRequired
	}
	var receiptRaw, verifiedRaw []byte
	err = tx.QueryRowContext(ctx, `SELECT r.payload FROM exec_runner_receipts r JOIN exec_task_attempts a ON a.id=r.attempt_id JOIN exec_executions e ON e.id=a.execution_id JOIN exec_resource_leases claim ON claim.attempt_id=a.id AND claim.resource_id=$5
 WHERE a.id=$1 AND a.command_id=$2 AND a.fencing_token=$3 AND a.execution_id=$4 AND a.runner='provision' AND a.task_id='provision-install' AND a.state='success'
 AND e.project_id=$6 AND e.mode='provision' AND e.state='success' AND r.command_id=a.command_id AND r.fencing_token=a.fencing_token AND r.result='success'
 AND claim.state='released' AND claim.mode='exclusive' AND claim.fencing_token=a.fencing_token`, *x.AttemptID, *x.CommandID, *x.FencingToken, x.ExecutionID, "device:machine:"+x.MachineID, project).Scan(&receiptRaw)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrProvisionCanaryRequired
	}
	if err != nil {
		return err
	}
	err = tx.QueryRowContext(ctx, `SELECT payload FROM provision_installation_events WHERE installation_id=$1 AND event_type='first_boot.ssh_verified' ORDER BY id LIMIT 1`, id).Scan(&verifiedRaw)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrProvisionCanaryRequired
	}
	if err != nil {
		return err
	}
	var receipt provisionRegisteredReceipt
	var verified struct {
		NativeCommandID string                       `json:"native_command_id"`
		Command         taskrunner.TaskCommand       `json:"command"`
		Observation     taskrunner.Observation       `json:"observation"`
		HostFingerprint string                       `json:"host_fingerprint"`
		Credential      taskrunner.InstallCredential `json:"domain_credential"`
		Transport       string                       `json:"verified_transport"`
		AssetVersion    int64                        `json:"asset_version"`
		BindingSHA256   string                       `json:"binding_sha256"`
		VerificationID  string                       `json:"verification_id"`
	}
	if json.Unmarshal(receiptRaw, &receipt) != nil || json.Unmarshal(verifiedRaw, &verified) != nil || verified.Transport != "ssh" || verified.NativeCommandID != *x.CommandID || verified.Command.ProjectID != project || verified.Command.MachineID != x.MachineID || verified.Command.AttemptID != *x.AttemptID || verified.Command.FencingToken != *x.FencingToken || verified.Command.Operation != "host.identity" || verified.Command.TimeoutMS != 5000 || verified.Command.WaitMS != 0 || verified.Observation.Validate(verified.Command) != nil || verified.Observation.State != "success" || verified.Observation.Output["hostname"] != request.Snapshot.Target.Hostname || verified.Observation.Output["os"] != "linux" || verified.Observation.Output["arch"] != request.Snapshot.Target.Arch || !taskrunner.IsValidHostKey(verified.HostFingerprint) || !sha256Pattern.MatchString(verified.BindingSHA256) || verified.AssetVersion != request.Snapshot.Target.AssetVersion+2 {
		return ErrProvisionCanaryRequired
	}
	expected := request.Snapshot.DomainKey
	if verified.Credential.ID != expected.ID || verified.Credential.Ref != expected.Ref || verified.Credential.Version != expected.Version || verified.Credential.PublicKey != expected.PublicKey || !verified.Credential.ExpiresAt.Equal(expected.ExpiresAt) {
		return ErrProvisionCanaryRequired
	}
	if receipt.CommandID != *x.CommandID || receipt.AttemptID != *x.AttemptID || receipt.FencingToken != *x.FencingToken || receipt.Result != "success" || receipt.Output["schema"] != "keel.provision-installation-proof/v1" || receipt.Output["installation_performed"] != true || receipt.Output["installation_id"] != id || receipt.Output["request_id"] != x.RequestID || receipt.Output["snapshot_sha256"] != x.SnapshotSHA256 || receipt.Output["first_ssh_proof_sha256"] != ProvisionFirstSSHTranscriptSHA256(verifiedRaw) {
		return ErrProvisionCanaryRequired
	}
	if verified.Command.CommandID != *x.CommandID {
		var bound bool
		err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM provision_installation_events WHERE installation_id=$1 AND event_type='first_boot.ssh_verification_consumed' AND payload->>'native_command_id'=$2 AND payload->>'verification_command_id'=$3 AND payload->>'verification_id'=$4 AND payload->>'attempt_id'=$5 AND payload->>'fencing_token'=$6)`, id, *x.CommandID, verified.Command.CommandID, verified.VerificationID, *x.AttemptID, strconv.FormatInt(*x.FencingToken, 10)).Scan(&bound)
		if err != nil {
			return err
		}
		if !bound {
			return ErrProvisionCanaryRequired
		}
	}
	return nil
}

// RecordProvisionRolloutTx is called within installation creation, after current
// actor/source approval and the machine-admission mutex. It performs no IO or
// writes to a device and cannot issue a write permit.
func RecordProvisionRolloutTx(ctx context.Context, tx *sql.Tx, installationID string, request *ProvisionRequest, mode string, ids []string) error {
	canonical, err := CanonicalProvisionRollout(mode, ids)
	if err != nil {
		return err
	}
	identity, err := provisionRolloutIdentity(request)
	if err != nil {
		return err
	}
	if err = lockKey(ctx, tx, "provision.rollout:"+request.ProjectID+":"+identity); err != nil {
		return err
	}
	canaries, err := provisionCanariesTx(ctx, tx, request.ProjectID, identity)
	if err != nil {
		return err
	}
	if mode == "canary" {
		var released bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM provision_installation_rollouts WHERE project_id=$1 AND identity_sha256=$2 AND mode='validated')`, request.ProjectID, identity).Scan(&released); err != nil {
			return err
		}
		if released {
			return apiError(409, "PROVISION_CANARY_SET_FROZEN", "Rollout already admitted; its canary set is immutable")
		}
		selected := false
		for _, c := range canaries {
			if c.MachineID == request.MachineID {
				selected = true
			}
		}
		if !selected && len(canaries) >= 2 {
			return apiError(409, "PROVISION_CANARY_LIMIT", "This exact OS/model rollout already selected two canary machines")
		}
	} else {
		if err = matchProvisionCanaries(canaries, canonical); err != nil {
			return err
		}
	}
	raw, _ := json.Marshal(canonical)
	_, err = tx.ExecContext(ctx, `INSERT INTO provision_installation_rollouts(installation_id,project_id,identity_sha256,mode,canary_ids,created_at) VALUES($1,$2,$3,$4,$5,$6)`, installationID, request.ProjectID, identity, mode, raw, time.Now().UTC())
	if err != nil {
		return err
	}
	return appendProvisionEventTx(ctx, tx, &ProvisionInstallation{ID: installationID}, "installation.rollout_selected", map[string]any{"mode": mode, "identity_sha256": identity, "canary_ids": canonical})
}

func matchProvisionCanaries(canaries []ProvisionCanary, ids []string) error {
	if len(canaries) < 1 || len(canaries) > 2 || len(canaries) != len(ids) {
		return ErrProvisionCanaryRequired
	}
	want := make([]string, 0, len(canaries))
	for _, c := range canaries {
		if !c.Verified {
			return ErrProvisionCanaryRequired
		}
		want = append(want, c.InstallationID)
	}
	sort.Strings(want)
	for i, id := range want {
		if id != ids[i] {
			return ErrProvisionCanaryRequired
		}
	}
	return nil
}

// ValidateProvisionRolloutTx rechecks the immutable source and proof at dispatch
// and before write authorization; a missing legacy selection grants no release.
func ValidateProvisionRolloutTx(ctx context.Context, tx *sql.Tx, x *ProvisionInstallation, request *ProvisionRequest) error {
	selected, err := readProvisionRolloutTx(ctx, tx, x.ID)
	if err != nil {
		return err
	}
	identity, err := provisionRolloutIdentity(request)
	if err != nil {
		return err
	}
	if selected.IdentitySHA256 != identity {
		return ErrProvisionCanaryRequired
	}
	if selected.Mode == "validated" {
		canaries, e := provisionCanariesTx(ctx, tx, x.ProjectID, identity)
		if e != nil {
			return e
		}
		return matchProvisionCanaries(canaries, selected.CanaryIDs)
	}
	return nil
}

func (s *Store) PreviewProvisionRollout(ctx context.Context, a *AuthSession, project, requestID string) (*ProvisionRolloutPreview, error) {
	tx, err := s.beginPinnedRead(ctx, a)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = requireProvisionReadAuthority(ctx, tx, a, project, false); err != nil {
		return nil, err
	}
	request, err := scanProvisionRequest(tx.QueryRowContext(ctx, "SELECT "+provisionRequestColumns+" FROM provision_requests WHERE project_id=$1 AND id=$2", project, requestID))
	if err != nil {
		return nil, err
	}
	identity, err := provisionRolloutIdentity(request)
	if err != nil {
		return nil, err
	}
	canaries, err := provisionCanariesTx(ctx, tx, project, identity)
	if err != nil {
		return nil, err
	}
	var released bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM provision_installation_rollouts WHERE project_id=$1 AND identity_sha256=$2 AND mode='validated')`, project, identity).Scan(&released); err != nil {
		return nil, err
	}
	out := &ProvisionRolloutPreview{IdentitySHA256: identity, Canaries: canaries, CanarySelectionAvailable: !released && len(canaries) < 2, ReleaseReady: len(canaries) > 0}
	for _, c := range canaries {
		out.ReleaseReady = out.ReleaseReady && c.Verified
		if !released && c.MachineID == request.MachineID {
			out.CanarySelectionAvailable = true
		}
	}
	return out, tx.Commit()
}
