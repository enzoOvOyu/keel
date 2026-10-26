package platform

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/netip"
	"strings"
	"time"

	"github.com/enzoOvOyu/keel/internal/provisionboot"
	"github.com/enzoOvOyu/keel/internal/provisionfirstboot"
	"github.com/enzoOvOyu/keel/internal/provisionimage"
	"github.com/enzoOvOyu/keel/internal/provisionstorage"
)

var _ provisionboot.WriteAuthority = (*ProvisionBootAuthority)(nil)

type provisionRuntimeEvidence struct {
	Schema    string                     `json:"runtime_schema"`
	ConfigID  string                     `json:"runtime_config_id"`
	ConfigSHA string                     `json:"runtime_input_sha256"`
	Inputs    []provisionimage.Component `json:"runtime_inputs"`
	Writer    bool                       `json:"writer_tool_bytes_verified"`
}

func provisionRuntimeEvidenceTx(ctx context.Context, tx *sql.Tx, id string) (*provisionRuntimeEvidence, error) {
	var raw []byte
	if err := tx.QueryRowContext(ctx, `SELECT payload FROM provision_installation_events WHERE installation_id=$1 AND event_type='boot.activated' ORDER BY id DESC LIMIT 1`, id).Scan(&raw); err != nil {
		return nil, err
	}
	var e provisionRuntimeEvidence
	if json.Unmarshal(raw, &e) != nil || !sha256Pattern.MatchString(e.ConfigSHA) {
		return nil, ErrProvisionBootDenied
	}
	return &e, nil
}

// Recheck all independently approved inputs of the authenticated actual initrd.
// Historical approval in an embedded manifest does not override current revoke,
// quarantine or the reviewer's loss of authority. No media bytes are rehashed
// during each short liveness query.
func validateProvisionRuntimeInputsTx(ctx context.Context, tx *sql.Tx, x *ProvisionInstallation, expectedSHA string) error {
	e, err := provisionRuntimeEvidenceTx(ctx, tx, x.ID)
	if err != nil {
		return err
	}
	if e.Schema != provisionimage.WriterInitrdSchema || !e.Writer || e.ConfigSHA != expectedSHA || !modelIDPattern.MatchString(e.ConfigID) || len(e.Inputs) != 8 {
		return ErrProvisionBootDenied
	}
	type binding struct {
		id, sha, path, typ, arch string
		size                     int64
	}
	wants := []binding{{id: e.ConfigID, sha: e.ConfigSHA, typ: "config", arch: x.Spec.Expected.Arch}}
	paths := []string{provisionimage.InitPath, provisionimage.PreflightPath, provisionimage.SSHHelperPath, provisionimage.SMARTPath, provisionimage.BootCAPath, provisionimage.UnsquashFSPath, provisionimage.MkfsExt4Path, provisionimage.MkfsVFATPath}
	seen := map[string]bool{e.ConfigID: true}
	for i, c := range e.Inputs {
		if !modelIDPattern.MatchString(c.MediaID) || !sha256Pattern.MatchString(c.SHA256) || c.Path != paths[i] || c.Size < 1 || seen[c.MediaID] {
			return ErrProvisionBootDenied
		}
		seen[c.MediaID] = true
		typ, arch := "os", x.Spec.Expected.Arch
		if i == 4 {
			typ, arch = "config", "noarch"
		} else if i >= 5 {
			typ = "tool"
		}
		wants = append(wants, binding{id: c.MediaID, sha: c.SHA256, typ: typ, arch: arch, size: c.Size})
	}
	for _, w := range wants {
		m, err := mediaDependencyRecord(ctx, tx, w.id)
		if err != nil {
			return err
		}
		if m.State != "approved" || m.Sha256 != w.sha || m.Type != w.typ || m.Arch != w.arch || !m.ArtifactStored || m.ArtifactQuarantined || (w.size > 0 && m.Size != w.size) || (m.ScopeKey != "global" && m.ScopeKey != "project:"+x.ProjectID) || m.ApprovedBy == "" {
			return ErrProvisionBootDenied
		}
		var enabled, admin bool
		if err = tx.QueryRowContext(ctx, "SELECT enabled,is_admin FROM users WHERE id=$1 FOR SHARE", m.ApprovedBy).Scan(&enabled, &admin); err != nil {
			return err
		}
		if !enabled {
			return ErrProvisionBootDenied
		}
		actor := &AuthSession{User: UserSummary{ID: m.ApprovedBy, IsAdmin: admin}}
		var project *ProjectSummary
		if strings.HasPrefix(m.ScopeKey, "project:") {
			project = &ProjectSummary{ID: x.ProjectID}
		}
		if requireMediaManager(ctx, tx, actor, project) != nil {
			return ErrProvisionBootDenied
		}
		if err = mediaIntegrityGate(ctx, tx, m.ID, true); err != nil {
			return err
		}
	}
	return nil
}

func validateProvisionStorageApproverTx(ctx context.Context, tx *sql.Tx, x *ProvisionInstallation) error {
	if x.StorageApprovedBy == nil || x.StorageRequestedBy == nil || *x.StorageApprovedBy == x.CreatedBy || *x.StorageApprovedBy == *x.StorageRequestedBy {
		return ErrProvisionBootDenied
	}
	var enabled, admin bool
	if err := tx.QueryRowContext(ctx, "SELECT enabled,is_admin FROM users WHERE id=$1 FOR SHARE", *x.StorageApprovedBy).Scan(&enabled, &admin); err != nil {
		return err
	}
	if !enabled {
		return ErrProvisionBootDenied
	}
	if err := requireProvisionAuthority(ctx, tx, &AuthSession{User: UserSummary{ID: *x.StorageApprovedBy, IsAdmin: admin}}, x.ProjectID, true); err != nil {
		return ErrProvisionBootDenied
	}
	return nil
}

func writePermitEventTx(ctx context.Context, tx *sql.Tx, x *ProvisionInstallation) (*provisionboot.WritePermit, error) {
	var raw []byte
	if err := tx.QueryRowContext(ctx, `SELECT payload->'permit' FROM provision_installation_events WHERE installation_id=$1 AND event_type='write.permit_issued' ORDER BY id DESC LIMIT 1`, x.ID).Scan(&raw); err != nil {
		return nil, err
	}
	var p provisionboot.WritePermit
	if json.Unmarshal(raw, &p) != nil || p.Schema != provisionboot.WritePermitSchema || p.InstallationID != x.ID || x.BootID == nil || p.BootID != *x.BootID || x.PermitIssuedAt == nil || !p.IssuedAt.Equal(*x.PermitIssuedAt) || p.Sequence > x.CallbackSequence {
		return nil, ErrProvisionBootDenied
	}
	return &p, nil
}

func (a *ProvisionBootAuthority) IssueWritePermit(ctx context.Context, scope provisionboot.Scope, grant string, peer netip.Addr, in provisionboot.WritePermitSubmission) (*provisionboot.WritePermit, error) {
	if a == nil || a.Store == nil || a.Store.DB == nil || !a.Store.ProvisionInstallEnabled || !scope.CheckPeer(peer) {
		return nil, ErrProvisionBootDenied
	}
	payload, err := provisionboot.WritePermitPayload(in)
	if err != nil {
		return nil, ErrProvisionBootDenied
	}
	tx, err := a.Store.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	x, request, current, _, err := installationMutationTx(ctx, tx, scope.ProjectID, in.Proof.InstallationID)
	if err != nil {
		return nil, err
	}
	if checkProvisionCallbackPeer(&provisionCallbackPeer{scope: scope, peer: peer}, current) != nil || x.GrantID != grant || x.BootID == nil || x.State != "storage_approved" || x.PermitIssuedAt != nil || x.StoragePlan == nil || x.StoragePlanSHA256 == nil || x.StoragePlan.SHA256 != *x.StoragePlanSHA256 || in.Preparation.StoragePlanSHA256 != *x.StoragePlanSHA256 || request.Snapshot.Media.ValidateWritable() != nil {
		return nil, ErrProvisionBootDenied
	}
	if provisionboot.VerifyBootProof(x.BootPublicKey, x.ID, *x.BootID, x.CallbackSequence+1, "write-permit", payload, in.Proof) != nil {
		return nil, ErrProvisionBootDenied
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if provisionstorage.VerifyCurrent(*x.StoragePlan, in.Report, now) != nil {
		return nil, ErrProvisionBootDenied
	}
	prepared, err := provisionstorage.PrepareGPT(*x.StoragePlan, in.Preparation.GPTIdentity, now)
	if err != nil || prepared.SHA256() != in.Preparation.GPTSHA256 {
		return nil, ErrProvisionBootDenied
	}
	if err = validateProvisionStorageApproverTx(ctx, tx, x); err != nil {
		return nil, err
	}
	if err = validateProvisionRuntimeInputsTx(ctx, tx, x, in.Preparation.RuntimeInputSHA256); err != nil {
		return nil, err
	}
	if in.Preparation.RootFSSHA256 != provisionRoleSHA(request, "rootfs") || in.Preparation.TargetUKISHA256 != request.Snapshot.Media.Profile.Recipe.TargetBoot.SHA256 {
		return nil, ErrProvisionBootDenied
	}
	out := &provisionboot.WritePermit{Schema: provisionboot.WritePermitSchema, ID: RandomID(), InstallationID: x.ID, BootID: *x.BootID, Sequence: in.Proof.Sequence, Nonce: in.Preparation.Nonce, StoragePlanSHA256: *x.StoragePlanSHA256, GPTSHA256: prepared.SHA256(), RootFSSHA256: in.Preparation.RootFSSHA256, TargetUKISHA256: in.Preparation.TargetUKISHA256, RuntimeInputSHA256: in.Preparation.RuntimeInputSHA256, IssuedAt: now, ExpiresAt: x.ExpiresAt}
	if in.Preparation.FirstBootBinding != nil {
		if err = validateProvisionFirstBootBindingTx(ctx, tx, x, request, scope, in.Preparation, now); err != nil {
			return nil, err
		}
		out.FirstBootBindingSHA256 = in.Preparation.FirstBootBinding.SHA256()
	}
	if provisionboot.ValidateWritePermit(out, in.Proof, in.Preparation, now) != nil {
		return nil, ErrProvisionBootDenied
	}
	assetEvidence, err := ActivateProvisionAssetForWriteTx(ctx, tx, x, request)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE provision_installations SET state='installing',state_version=state_version+1,callback_sequence=callback_sequence+1,permit_issued_at=$2,updated_at=$2 WHERE id=$1`, x.ID, now); err != nil {
		return nil, err
	}
	var firstBoot *provisionFirstBootCommitment
	if b := in.Preparation.FirstBootBinding; b != nil {
		firstBoot = &provisionFirstBootCommitment{SHA256: b.SHA256(), PublicKey: b.FirstBootPublicKey, InstallerKernelBootID: b.InstallerKernelBootID, CreatedAt: b.CreatedAt}
	}
	evidence := map[string]any{"permit": out, "gpt_identity": in.Preparation.GPTIdentity, "fresh_report_sha256": digest(string(in.Report)), "storage_approved_by": *x.StorageApprovedBy, "first_boot_binding": firstBoot}
	for key, value := range assetEvidence {
		evidence[key] = value
	}
	if err = appendProvisionEventTx(ctx, tx, x, "write.permit_issued", evidence); err != nil {
		return nil, err
	}
	if err = appendAudit(ctx, tx, "", x.ProjectID, "provision.write_permit.issued", x.ID, "boot", map[string]any{"permit_id": out.ID, "storage_plan_sha256": out.StoragePlanSHA256, "gpt_sha256": out.GPTSHA256, "attempt_id": *x.AttemptID, "fencing_token": *x.FencingToken}); err != nil {
		return nil, err
	}
	return out, tx.Commit()
}

func validateProvisionFirstBootBindingTx(ctx context.Context, tx *sql.Tx, x *ProvisionInstallation, request *ProvisionRequest, scope provisionboot.Scope, p provisionboot.WritePreparation, now time.Time) error {
	b := p.FirstBootBinding
	if b == nil || b.Validate(now) != nil || b.InstallationID != x.ID || x.BootID == nil || b.BootID != *x.BootID || b.GrantID != x.GrantID || b.GPTIdentity != p.GPTIdentity || b.Hostname != request.Snapshot.Target.Hostname || b.Origin != scope.Origin || b.Server != scope.Server || b.Clients != scope.Clients || b.TargetUKISHA256 != p.TargetUKISHA256 || b.RuntimeInputSHA256 != p.RuntimeInputSHA256 || b.FirstBootPublicKey == base64.StdEncoding.EncodeToString(x.BootPublicKey) {
		return ErrProvisionBootDenied
	}
	want, _ := json.Marshal(x.StoragePlan)
	given, _ := json.Marshal(b.StoragePlan)
	if !bytes.Equal(want, given) {
		return ErrProvisionBootDenied
	}
	e, err := provisionRuntimeEvidenceTx(ctx, tx, x.ID)
	if err != nil {
		return err
	}
	if len(e.Inputs) != 8 || b.BootCASHA256 != e.Inputs[4].SHA256 {
		return ErrProvisionBootDenied
	}
	return nil
}

type provisionFirstBootCommitment struct {
	SHA256                string    `json:"sha256"`
	PublicKey             string    `json:"public_key"`
	InstallerKernelBootID string    `json:"installer_kernel_boot_id"`
	CreatedAt             time.Time `json:"created_at"`
}

func provisionFirstBootBindingTx(ctx context.Context, tx *sql.Tx, x *ProvisionInstallation, request *ProvisionRequest, scope provisionboot.Scope) (*provisionfirstboot.Binding, error) {
	var raw []byte
	if err := tx.QueryRowContext(ctx, `SELECT payload FROM provision_installation_events WHERE installation_id=$1 AND event_type='write.permit_issued' ORDER BY id DESC LIMIT 1`, x.ID).Scan(&raw); err != nil {
		return nil, err
	}
	var saved struct {
		Binding *provisionFirstBootCommitment `json:"first_boot_binding"`
		GPT     provisionstorage.GPTIdentity  `json:"gpt_identity"`
	}
	if json.Unmarshal(raw, &saved) != nil || saved.Binding == nil || x.StoragePlan == nil || x.BootID == nil || request.Snapshot.Media.ValidateWritable() != nil {
		return nil, ErrProvisionBootDenied
	}
	e, err := provisionRuntimeEvidenceTx(ctx, tx, x.ID)
	if err != nil {
		return nil, err
	}
	if len(e.Inputs) != 8 {
		return nil, ErrProvisionBootDenied
	}
	b := &provisionfirstboot.Binding{Schema: provisionfirstboot.BindingSchema, InstallationID: x.ID, BootID: *x.BootID, GrantID: x.GrantID, StoragePlan: *x.StoragePlan, GPTIdentity: saved.GPT, Hostname: request.Snapshot.Target.Hostname, Origin: scope.Origin, Server: scope.Server, Clients: scope.Clients, BootCASHA256: e.Inputs[4].SHA256, TargetUKISHA256: request.Snapshot.Media.Profile.Recipe.TargetBoot.SHA256, RuntimeInputSHA256: e.ConfigSHA, FirstBootPublicKey: saved.Binding.PublicKey, InstallerKernelBootID: saved.Binding.InstallerKernelBootID, CreatedAt: saved.Binding.CreatedAt}
	if b.SHA256() != saved.Binding.SHA256 {
		return nil, ErrProvisionBootDenied
	}
	return b, nil
}

func provisionRoleSHA(request *ProvisionRequest, role string) string {
	for _, a := range request.Snapshot.Media.Artifacts {
		if a.Role == role {
			return a.SHA256
		}
	}
	return ""
}

// The check deliberately avoids loading the old multi-MiB report or writing a
// version/event. It holds current authority locks only until this short read
// completes; this is not a substitute for the writer's continuous cancellation.
func (a *ProvisionBootAuthority) CheckWritePermit(ctx context.Context, scope provisionboot.Scope, grant string, peer netip.Addr, in provisionboot.WriteCheckSubmission) (*provisionboot.WriteAuthorization, error) {
	if a == nil || a.Store == nil || a.Store.DB == nil || !a.Store.ProvisionInstallEnabled || !scope.CheckPeer(peer) || !modelIDPattern.MatchString(in.Query.Nonce) || !provisionboot.ValidWriteStage(in.Query.Stage) {
		return nil, ErrProvisionBootDenied
	}
	tx, err := a.Store.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	x, err := scanProvisionInstallationBinding(tx.QueryRowContext(ctx, "SELECT "+provisionInstallationBindingColumns+" FROM provision_installations WHERE id=$1 AND project_id=$2", in.Proof.InstallationID, scope.ProjectID))
	if err != nil {
		return nil, err
	}
	request, current, until, err := ValidateProvisionInstallationTx(ctx, tx, x, time.Now())
	if err != nil {
		return nil, err
	}
	if checkProvisionCallbackPeer(&provisionCallbackPeer{scope: scope, peer: peer}, current) != nil || x.GrantID != grant || request.Snapshot.Media.ValidateWritable() != nil {
		return nil, ErrProvisionBootDenied
	}
	if err = tx.QueryRowContext(ctx, `SELECT state,boot_id,boot_public_key,callback_sequence,permit_issued_at,storage_plan_sha256,storage_requested_by,storage_approved_by FROM provision_installations WHERE id=$1 FOR SHARE`, x.ID).Scan(&x.State, &x.BootID, &x.BootPublicKey, &x.CallbackSequence, &x.PermitIssuedAt, &x.StoragePlanSHA256, &x.StorageRequestedBy, &x.StorageApprovedBy); err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(in.Query)
	if x.State != "installing" || x.BootID == nil || provisionboot.VerifyBootProof(x.BootPublicKey, x.ID, *x.BootID, x.CallbackSequence, "write-check", payload, in.Proof) != nil {
		return nil, ErrProvisionBootDenied
	}
	p, err := writePermitEventTx(ctx, tx, x)
	if err != nil {
		return nil, err
	}
	if in.Query.PermitID != p.ID || p.Sequence != x.CallbackSequence || !p.ExpiresAt.After(time.Now()) {
		return nil, ErrProvisionBootDenied
	}
	if err = validateProvisionStorageApproverTx(ctx, tx, x); err != nil {
		return nil, err
	}
	if err = validateProvisionRuntimeInputsTx(ctx, tx, x, p.RuntimeInputSHA256); err != nil {
		return nil, err
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	valid := now.Add(2 * time.Second)
	if until.Before(valid) {
		valid = until
	}
	if p.ExpiresAt.Before(valid) {
		valid = p.ExpiresAt
	}
	out := &provisionboot.WriteAuthorization{PermitID: p.ID, InstallationID: p.InstallationID, BootID: p.BootID, Sequence: p.Sequence, Nonce: in.Query.Nonce, Stage: in.Query.Stage, StoragePlanSHA256: p.StoragePlanSHA256, GPTSHA256: p.GPTSHA256, CheckedAt: now, ValidUntil: valid}
	if provisionboot.ValidateWriteAuthorization(out, p, in.Query, now) != nil {
		return nil, ErrProvisionBootDenied
	}
	return out, tx.Commit()
}

func (a *ProvisionBootAuthority) ReportWriteReceipt(ctx context.Context, scope provisionboot.Scope, grant string, peer netip.Addr, in provisionboot.WriteReceiptSubmission) (*provisionboot.BootAcknowledgment, error) {
	if a == nil || a.Store == nil || a.Store.DB == nil || !a.Store.ProvisionInstallEnabled || !scope.CheckPeer(peer) {
		return nil, ErrProvisionBootDenied
	}
	tx, err := a.Store.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	x, request, current, leaseUntil, err := installationMutationTx(ctx, tx, scope.ProjectID, in.Proof.InstallationID)
	if err != nil {
		return nil, err
	}
	if checkProvisionCallbackPeer(&provisionCallbackPeer{scope: scope, peer: peer}, current) != nil || x.GrantID != grant || x.State != "installing" || x.BootID == nil || x.PermitIssuedAt == nil {
		return nil, ErrProvisionBootDenied
	}
	payload, _ := json.Marshal(in.Receipt)
	if provisionboot.VerifyBootProof(x.BootPublicKey, x.ID, *x.BootID, x.CallbackSequence+1, "install-stopped", payload, in.Proof) != nil {
		return nil, ErrProvisionBootDenied
	}
	p, err := writePermitEventTx(ctx, tx, x)
	if err != nil {
		return nil, err
	}
	if provisionboot.ValidateWriteReceipt(in.Receipt, p) != nil {
		return nil, ErrProvisionBootDenied
	}
	if err = validateProvisionStorageApproverTx(ctx, tx, x); err != nil {
		return nil, err
	}
	if err = validateProvisionRuntimeInputsTx(ctx, tx, x, p.RuntimeInputSHA256); err != nil {
		return nil, err
	}
	state := "unknown"
	if in.Receipt.Outcome == "written" {
		if request.Snapshot.Media.ValidateWritable() != nil {
			return nil, ErrProvisionBootDenied
		}
		expected := provisionboot.WriteReadback{GPTSHA256: p.GPTSHA256, RootFSSHA256: p.RootFSSHA256, TargetUKISHA256: p.TargetUKISHA256, SSHHelperSHA256: provisionRoleSHA(request, "ssh-helper"), UserDataSHA256: digest(request.Snapshot.Seed.UserData), MetaDataSHA256: digest(request.Snapshot.Seed.MetaData)}
		if expected.SHA256() == "" || expected.SHA256() != in.Receipt.ReadbackSHA256 {
			return nil, ErrProvisionBootDenied
		}
		state = "awaiting_first_boot"
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if _, err = tx.ExecContext(ctx, `UPDATE provision_installations SET state=$2,state_version=state_version+1,callback_sequence=callback_sequence+1,updated_at=$3 WHERE id=$1`, x.ID, state, now); err != nil {
		return nil, err
	}
	if err = appendProvisionEventTx(ctx, tx, x, "write.receipt_reported", map[string]any{"receipt": in.Receipt, "receipt_sha256": provisionboot.WriteReceiptSHA256(in.Receipt), "sequence": in.Proof.Sequence, "authenticated_claim_only": true, "first_boot_verified": false, "ssh_trust_established": false}); err != nil {
		return nil, err
	}
	out := &provisionboot.BootAcknowledgment{InstallationID: x.ID, BootID: *x.BootID, Sequence: in.Proof.Sequence, State: state, ExpiresAt: x.ExpiresAt}
	if state == "awaiting_first_boot" {
		deadline := now.Add(2 * time.Second)
		if leaseUntil.Before(deadline) {
			deadline = leaseUntil
		}
		if x.ExpiresAt.Before(deadline) {
			deadline = x.ExpiresAt
		}
		if !deadline.After(time.Now()) {
			return nil, ErrProvisionBootDenied
		}
		out.RebootBefore = &deadline
	}
	return out, tx.Commit()
}
