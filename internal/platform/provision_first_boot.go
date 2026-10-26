package platform

import (
	"context"
	"encoding/json"
	"net/netip"
	"time"

	"github.com/enzoOvOyu/keel/internal/provisionboot"
	"github.com/enzoOvOyu/keel/internal/provisionfirstboot"
)

var _ provisionboot.FirstBootAuthority = (*ProvisionBootAuthority)(nil)

// First boot has a separate key and transcript. It records an authenticated
// installed-system identity only; no machine key/trust or asset success is
// projected until the native execution performs a real pinned SSH handshake.
func (a *ProvisionBootAuthority) ReportFirstBoot(ctx context.Context, scope provisionboot.Scope, grant string, peer netip.Addr, in provisionfirstboot.Submission) (*provisionfirstboot.Acknowledgment, error) {
	if a == nil || a.Store == nil || a.Store.DB == nil || !a.Store.ProvisionInstallEnabled || !scope.CheckPeer(peer) || !modelIDPattern.MatchString(in.Report.InstallationID) {
		return nil, ErrProvisionBootDenied
	}
	tx, err := a.Store.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	x, request, current, _, err := installationMutationTx(ctx, tx, scope.ProjectID, in.Report.InstallationID)
	if err != nil {
		return nil, err
	}
	if checkProvisionCallbackPeer(&provisionCallbackPeer{scope: scope, peer: peer}, current) != nil || x.State != "awaiting_first_boot" || x.GrantID != grant || x.BootID == nil || in.Report.BootID != *x.BootID || x.PermitIssuedAt == nil {
		return nil, ErrProvisionBootDenied
	}
	binding, err := provisionFirstBootBindingTx(ctx, tx, x, request, scope)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if provisionfirstboot.VerifySignature(*binding, in, now) != nil {
		return nil, ErrProvisionBootDenied
	}
	if in.Report.Hostname != binding.Hostname {
		var already bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM provision_installation_events WHERE installation_id=$1 AND event_type='first_boot.hostname_mismatch')`, x.ID).Scan(&already); err != nil {
			return nil, err
		}
		if !already {
			if err = appendProvisionEventTx(ctx, tx, x, "first_boot.hostname_mismatch", map[string]any{"expected_hostname": binding.Hostname, "observed_hostname": in.Report.Hostname, "binding_sha256": binding.SHA256(), "registration_authorized": false}); err != nil {
				return nil, err
			}
			if err = appendAudit(ctx, tx, "", x.ProjectID, "provision.first_boot.hostname_mismatch", x.ID, "first-boot", map[string]any{"expected_hostname": binding.Hostname, "observed_hostname": in.Report.Hostname, "registration_authorized": false}); err != nil {
				return nil, err
			}
			if err = tx.Commit(); err != nil {
				return nil, err
			}
		}
		return nil, ErrProvisionBootDenied
	}
	if provisionfirstboot.VerifySubmission(*binding, in, now) != nil {
		return nil, ErrProvisionBootDenied
	}
	p, err := writePermitEventTx(ctx, tx, x)
	if err != nil {
		return nil, err
	}
	if p.FirstBootBindingSHA256 != binding.SHA256() || !p.ExpiresAt.After(now) {
		return nil, ErrProvisionBootDenied
	}
	if err = validateProvisionStorageApproverTx(ctx, tx, x); err != nil {
		return nil, err
	}
	if err = validateProvisionRuntimeInputsTx(ctx, tx, x, p.RuntimeInputSHA256); err != nil {
		return nil, err
	}
	var registered, dhcp bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM provision_installation_events WHERE installation_id=$1 AND event_type='first_boot.identity_reported'),EXISTS(SELECT 1 FROM provision_installation_events WHERE installation_id=$1 AND event_type='boot.dhcp_observed' AND payload->>'mac'=$2 AND payload->>'arch'=$3 AND payload->>'fingerprint_sha256' ~ '^[a-f0-9]{64}$')`, x.ID, binding.StoragePlan.Spec.Expected.BootMAC, binding.StoragePlan.Spec.Expected.Arch).Scan(&registered, &dhcp); err != nil {
		return nil, err
	}
	if registered || !dhcp {
		return nil, ErrProvisionBootDenied
	}
	raw, err := json.Marshal(in.Report)
	if err != nil {
		return nil, err
	}
	if err = appendProvisionEventTx(ctx, tx, x, "first_boot.identity_reported", map[string]any{"report": in.Report, "signature": in.Signature, "report_sha256": digest(string(raw)), "binding_sha256": binding.SHA256(), "peer": peer.String(), "scope_sha256": x.ScopeSHA256, "http_authenticated": true, "dhcp_auxiliary_matched": true, "ssh_trust_established": false}); err != nil {
		return nil, err
	}
	if err = appendAudit(ctx, tx, "", x.ProjectID, "provision.first_boot.identity_reported", x.ID, "first-boot", map[string]any{"binding_sha256": binding.SHA256(), "host_fingerprint": in.Report.HostFingerprint, "ssh_trust_established": false}); err != nil {
		return nil, err
	}
	out := &provisionfirstboot.Acknowledgment{InstallationID: x.ID, BootID: *x.BootID, BindingSHA256: binding.SHA256(), Nonce: in.Report.Nonce, State: "awaiting_first_boot", HostFingerprint: in.Report.HostFingerprint, ReceivedAt: now}
	return out, tx.Commit()
}
