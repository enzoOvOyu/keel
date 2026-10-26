package platform

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/netip"
	"sync"
	"time"

	"github.com/enzoOvOyu/keel/internal/provisionboot"
)

// ProvisionBootAuthority bridges isolated transports to current database
// authority. It opens no listener and never derives a callback secret from MAC.
type ProvisionBootAuthority struct{ Store *Store }

var _ provisionboot.BootAuthority = (*ProvisionBootAuthority)(nil)
var _ provisionboot.FileAuthority = (*ProvisionBootAuthority)(nil)
var _ provisionboot.CallbackAuthority = (*ProvisionBootAuthority)(nil)

type provisionBootRead struct {
	installation *ProvisionInstallation
	request      *ProvisionRequest
	grant        provisionboot.Grant
}

func (a *ProvisionBootAuthority) liveTx(ctx context.Context, tx *sql.Tx, scope provisionboot.Scope, id string, peer netip.Addr) (*provisionBootRead, error) {
	if a == nil || a.Store == nil || !a.Store.ProvisionInstallEnabled || scope.Validate() != nil || !modelIDPattern.MatchString(id) || (peer.IsValid() && !scope.CheckPeer(peer)) {
		return nil, ErrProvisionBootDenied
	}
	x, err := scanProvisionInstallationBinding(tx.QueryRowContext(ctx, "SELECT "+provisionInstallationBindingColumns+" FROM provision_installations WHERE grant_id=$1 AND project_id=$2 AND scope_id=$3", id, scope.ProjectID, scope.ID))
	if err != nil {
		return nil, err
	}
	request, current, _, err := ValidateProvisionInstallationTx(ctx, tx, x, time.Now())
	if err != nil {
		return nil, err
	}
	// State is read under a domain lock only after the execution/attempt locks.
	if err = tx.QueryRowContext(ctx, "SELECT state FROM provision_installations WHERE id=$1 FOR SHARE", x.ID).Scan(&x.State); err != nil {
		return nil, err
	}
	switch x.State {
	case "booting", "preflight_ready", "awaiting_storage_review", "storage_approved":
	default:
		return nil, ErrProvisionBootDenied
	}
	raw, err := json.Marshal(scope)
	if err != nil || current.ScopeSHA256 != digest(string(raw)) || current.ID != scope.ID {
		return nil, ErrProvisionBootDenied
	}
	grant, err := x.BootGrant()
	if err != nil || grant.Validate(scope, time.Now()) != nil {
		return nil, ErrProvisionBootDenied
	}
	return &provisionBootRead{installation: x, request: request, grant: grant}, nil
}
func (a *ProvisionBootAuthority) live(ctx context.Context, scope provisionboot.Scope, id string, peer netip.Addr) (*provisionBootRead, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if a == nil || a.Store == nil || a.Store.DB == nil {
		return nil, ErrProvisionBootDenied
	}
	tx, err := a.Store.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	x, err := a.liveTx(ctx, tx, scope, id, peer)
	if err != nil {
		return nil, err
	}
	return x, tx.Commit()
}
func sameProvisionGrant(a, b provisionboot.Grant) bool {
	a.ExpiresAt = a.ExpiresAt.UTC()
	b.ExpiresAt = b.ExpiresAt.UTC()
	return a == b
}
func (a *ProvisionBootAuthority) LookupBoot(ctx context.Context, scope provisionboot.Scope, client provisionboot.Client) (provisionboot.Grant, error) {
	if a == nil || a.Store == nil || a.Store.DB == nil || !a.Store.ProvisionInstallEnabled || scope.Validate() != nil || client.MAC == "" || (client.Arch != "amd64" && client.Arch != "arm64") || !sha256Pattern.MatchString(client.FingerprintSHA256) {
		return provisionboot.Grant{}, ErrProvisionBootDenied
	}
	tx, err := a.Store.DB.BeginTx(ctx, nil)
	if err != nil {
		return provisionboot.Grant{}, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT grant_id FROM provision_installations WHERE project_id=$1 AND scope_id=$2 AND state='booting' AND expires_at>now() AND spec->'expected'->>'boot_mac'=$3 LIMIT 2`, scope.ProjectID, scope.ID, client.MAC)
	if err != nil {
		return provisionboot.Grant{}, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return provisionboot.Grant{}, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return provisionboot.Grant{}, err
	}
	if len(ids) != 1 {
		return provisionboot.Grant{}, ErrProvisionBootDenied
	}
	x, err := a.liveTx(ctx, tx, scope, ids[0], netip.Addr{})
	if err != nil {
		return provisionboot.Grant{}, err
	}
	if x.installation.State != "booting" || x.grant.MAC != client.MAC || x.grant.Arch != client.Arch || (client.Address.IsValid() && !client.Address.IsUnspecified() && !scope.CheckPeer(client.Address)) {
		return provisionboot.Grant{}, ErrProvisionBootDenied
	}
	// This is explicitly auxiliary network evidence, not authenticated identity.
	_, err = tx.ExecContext(ctx, `INSERT INTO provision_installation_events(installation_id,event_type,payload,created_at) SELECT $1,'boot.dhcp_observed',jsonb_build_object('mac',$2::text,'arch',$3::text,'fingerprint_sha256',$4::text,'auxiliary_only',true),now() WHERE NOT EXISTS(SELECT 1 FROM provision_installation_events WHERE installation_id=$1 AND event_type='boot.dhcp_observed' AND payload->>'fingerprint_sha256'=$4)`, x.installation.ID, client.MAC, client.Arch, client.FingerprintSHA256)
	if err != nil {
		return provisionboot.Grant{}, err
	}
	return x.grant, tx.Commit()
}
func (a *ProvisionBootAuthority) CheckBoot(ctx context.Context, scope provisionboot.Scope, grant provisionboot.Grant, client provisionboot.Client) error {
	x, err := a.live(ctx, scope, grant.ID, netip.Addr{})
	if err != nil {
		return err
	}
	if x.installation.State != "booting" || !sameProvisionGrant(x.grant, grant) || grant.MAC != client.MAC || grant.Arch != client.Arch || !sha256Pattern.MatchString(client.FingerprintSHA256) || (client.Address.IsValid() && !client.Address.IsUnspecified() && !scope.CheckPeer(client.Address)) {
		return ErrProvisionBootDenied
	}
	return nil
}
func (a *ProvisionBootAuthority) AuthorizeFile(ctx context.Context, scope provisionboot.Scope, id string, peer netip.Addr, resource string) (provisionboot.Grant, error) {
	if !scope.CheckPeer(peer) || !provisionResource(resource) {
		return provisionboot.Grant{}, ErrProvisionBootDenied
	}
	x, err := a.live(ctx, scope, id, peer)
	if err != nil {
		return provisionboot.Grant{}, err
	}
	if !provisionResourceExists(x, resource) {
		return provisionboot.Grant{}, ErrProvisionBootDenied
	}
	return x.grant, nil
}
func (a *ProvisionBootAuthority) CheckFile(ctx context.Context, scope provisionboot.Scope, grant provisionboot.Grant, peer netip.Addr, resource string) error {
	if !scope.CheckPeer(peer) || !provisionResource(resource) {
		return ErrProvisionBootDenied
	}
	x, err := a.live(ctx, scope, grant.ID, peer)
	if err != nil {
		return err
	}
	if !sameProvisionGrant(x.grant, grant) || !provisionResourceExists(x, resource) {
		return ErrProvisionBootDenied
	}
	return nil
}
func provisionResource(value string) bool {
	switch value {
	case "boot.ipxe", "manifest.json", "preflight.json", "user-data", "meta-data", "kernel", "initrd", "rootfs", "preflight", "ssh-helper", "ipxe.efi", "undionly.kpxe":
		return true
	}
	return false
}
func provisionMediaRole(resource string) string {
	switch resource {
	case "ipxe.efi":
		return "uefi"
	case "undionly.kpxe":
		return "legacy"
	case "kernel", "initrd", "rootfs", "preflight", "ssh-helper":
		return resource
	}
	return ""
}
func provisionResourceExists(x *provisionBootRead, resource string) bool {
	role := provisionMediaRole(resource)
	if role == "" {
		return provisionResource(resource)
	}
	for _, artifact := range x.request.Snapshot.Media.Artifacts {
		if artifact.Role == role {
			return true
		}
	}
	return false
}

type provisionMemoryFile struct{ *bytes.Reader }

func (provisionMemoryFile) Close() error { return nil }

type provisionStagedFile struct {
	stage *stagedMedia
	once  sync.Once
}

func (f *provisionStagedFile) Read(p []byte) (int, error) { return f.stage.file.Read(p) }
func (f *provisionStagedFile) Seek(offset int64, whence int) (int64, error) {
	return f.stage.file.Seek(offset, whence)
}
func (f *provisionStagedFile) Close() error { f.once.Do(f.stage.close); return nil }

func (a *ProvisionBootAuthority) OpenFile(ctx context.Context, scope provisionboot.Scope, grant provisionboot.Grant, resource string) (out provisionboot.Artifact, resultErr error) {
	if !provisionResource(resource) {
		return out, ErrProvisionBootDenied
	}
	x, err := a.live(ctx, scope, grant.ID, netip.Addr{})
	if err != nil {
		return out, err
	}
	if !sameProvisionGrant(x.grant, grant) || !provisionResourceExists(x, resource) {
		return out, ErrProvisionBootDenied
	}
	role := provisionMediaRole(resource)
	if role == "" {
		bundle, err := provisionboot.RenderBootBundle(scope, grant, x.request.Snapshot.Media, *x.installation.Spec, x.request.Snapshot.Seed, time.Now())
		if err != nil {
			return out, err
		}
		var raw []byte
		switch resource {
		case "boot.ipxe":
			raw = bundle.IPXE
		case "manifest.json":
			raw = bundle.Manifest
		case "preflight.json":
			raw = bundle.Preflight
		case "user-data":
			raw = []byte(x.request.Snapshot.Seed.UserData)
		case "meta-data":
			raw = []byte(x.request.Snapshot.Seed.MetaData)
		default:
			return out, ErrProvisionBootDenied
		}
		return provisionboot.Artifact{File: provisionMemoryFile{bytes.NewReader(raw)}, SHA256: digest(string(raw)), Size: int64(len(raw)), ObjectVersion: "rendered:" + digest(string(raw))}, nil
	}
	for _, artifact := range x.request.Snapshot.Media.Artifacts {
		if artifact.Role != role {
			continue
		}
		tx, err := a.Store.DB.BeginTx(ctx, nil)
		if err != nil {
			return out, err
		}
		defer func() { tx.Rollback(); resultErr = a.Store.RecordMediaIntegrityFailure(ctx, resultErr, "", "boot") }()
		media, err := mediaDependencyRecord(ctx, tx, artifact.MediaID)
		if err != nil {
			return out, err
		}
		var key, version, sha string
		var size int64
		if err = tx.QueryRowContext(ctx, "SELECT object_key,object_version,sha256,size FROM media_artifacts WHERE media_id=$1 FOR SHARE", media.ID).Scan(&key, &version, &sha, &size); err != nil {
			return out, err
		}
		if media.State != "approved" || media.ScopeKey != artifact.ScopeKey || media.Sha256 != artifact.SHA256 || media.Size != artifact.Size || media.Arch != artifact.Arch || version != artifact.ObjectVersion || sha != artifact.SHA256 || size != artifact.Size {
			return out, ErrProvisionBootDenied
		}
		if err = tx.Commit(); err != nil {
			return out, err
		}
		// Release the SQL connection too: concurrent large-object reads must not
		// exhaust the pool needed by heartbeat and current-authority checks.
		check := func() error {
			current, e := a.live(ctx, scope, grant.ID, netip.Addr{})
			if e != nil {
				return e
			}
			if !sameProvisionGrant(current.grant, grant) {
				return ErrProvisionBootDenied
			}
			return nil
		}
		stage, err := a.Store.stagePinnedMedia(ctx, &media, key, version, sha, size, check, check)
		if err != nil {
			return out, err
		}
		return provisionboot.Artifact{File: &provisionStagedFile{stage: stage}, SHA256: artifact.SHA256, Size: artifact.Size, ObjectVersion: version}, nil
	}
	return out, ErrProvisionBootDenied
}
func (a *ProvisionBootAuthority) EnrollBoot(ctx context.Context, scope provisionboot.Scope, grant string, peer netip.Addr, token string, in provisionboot.EnrollmentSubmission) (*provisionboot.BootAcknowledgment, error) {
	if a == nil || a.Store == nil || a.Store.DB == nil || scope.Validate() != nil || !scope.CheckPeer(peer) {
		return nil, ErrProvisionBootDenied
	}
	return a.Store.enrollProvisionBoot(ctx, scope.ProjectID, grant, token, in.Enrollment, in.Proof, &provisionCallbackPeer{scope: scope, peer: peer})
}
func (a *ProvisionBootAuthority) ReportPreflight(ctx context.Context, scope provisionboot.Scope, grant string, peer netip.Addr, in provisionboot.PreflightSubmission) (*provisionboot.BootAcknowledgment, error) {
	if a == nil || a.Store == nil || a.Store.DB == nil || scope.Validate() != nil || !scope.CheckPeer(peer) {
		return nil, ErrProvisionBootDenied
	}
	return a.Store.submitProvisionPreflight(ctx, scope.ProjectID, grant, in.Report, in.Proof, &provisionCallbackPeer{scope: scope, peer: peer})
}

// Revalidation is bounded by time, not file size. Even a tiny object is checked
// again by OpenFile after hashing, and Gateway checks during response transfer.
type provisionGuardedBody struct {
	io.Reader
	check   func() error
	checked time.Time
	ctx     context.Context
	failure error
}

func (r *provisionGuardedBody) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		r.failure = err
		return 0, err
	}
	if r.checked.IsZero() || time.Since(r.checked) >= 250*time.Millisecond {
		if err := r.check(); err != nil {
			r.failure = err
			return 0, err
		}
		r.checked = time.Now()
	}
	return r.Reader.Read(p)
}
