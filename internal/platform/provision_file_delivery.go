package platform

import (
	"context"
	"net/netip"
	"time"

	"github.com/enzoOvOyu/keel/internal/provisionboot"
)

var _ provisionboot.FileDeliveryAuthority = (*ProvisionBootAuthority)(nil)

// Shared deterministic rendering binds delivery evidence to the same frozen
// bytes used by OpenFile, without another object download or SQL-held hashing.
func provisionRenderedBytes(scope provisionboot.Scope, grant provisionboot.Grant, x *provisionBootRead, resource string) ([]byte, error) {
	if x == nil || x.request == nil || x.installation == nil || x.installation.Spec == nil {
		return nil, ErrProvisionBootDenied
	}
	bundle, err := provisionboot.RenderBootBundle(scope, grant, x.request.Snapshot.Media, *x.installation.Spec, x.request.Snapshot.Seed, time.Now())
	if err != nil {
		return nil, err
	}
	switch resource {
	case "boot.ipxe":
		return bundle.IPXE, nil
	case "manifest.json":
		return bundle.Manifest, nil
	case "preflight.json":
		return bundle.Preflight, nil
	case "user-data":
		return []byte(x.request.Snapshot.Seed.UserData), nil
	case "meta-data":
		return []byte(x.request.Snapshot.Seed.MetaData), nil
	}
	return nil, ErrProvisionBootDenied
}

// RecordFileServed is a server-side transport observation, not a callback and
// not installation authority. Repeated full downloads produce one fact per
// frozen resource; partial or failed downloads are never supplied by Gateway.
func (a *ProvisionBootAuthority) RecordFileServed(ctx context.Context, scope provisionboot.Scope, grant provisionboot.Grant, peer netip.Addr, delivered provisionboot.FileDelivery) error {
	if a == nil || a.Store == nil || a.Store.DB == nil || !scope.CheckPeer(peer) || !provisionResource(delivered.Resource) || delivered.Bytes < 1 || !sha256Pattern.MatchString(delivered.SHA256) || delivered.ObjectVersion == "" {
		return ErrProvisionBootDenied
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	tx, err := a.Store.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	current, err := a.liveTx(ctx, tx, scope, grant.ID, peer)
	if err != nil {
		return err
	}
	if !sameProvisionGrant(current.grant, grant) || !provisionResourceExists(current, delivered.Resource) {
		return ErrProvisionBootDenied
	}
	var sha, version string
	var size int64
	role := provisionMediaRole(delivered.Resource)
	if role == "" {
		raw, e := provisionRenderedBytes(scope, grant, current, delivered.Resource)
		if e != nil {
			return e
		}
		sha = digest(string(raw))
		version = "rendered:" + sha
		size = int64(len(raw))
	} else {
		for _, artifact := range current.request.Snapshot.Media.Artifacts {
			if artifact.Role == role {
				sha = artifact.SHA256
				version = artifact.ObjectVersion
				size = artifact.Size
				break
			}
		}
	}
	if sha != delivered.SHA256 || version != delivered.ObjectVersion || size != delivered.Bytes {
		return ErrProvisionBootDenied
	}
	// liveTx acquired execution/attempt and current domain locks first. Keep the
	// normal order and never upgrade the installation SHARE lock to UPDATE.
	if err = lockKey(ctx, tx, "provision.file-served:"+current.installation.ID+":"+delivered.Resource); err != nil {
		return err
	}
	if !grant.ExpiresAt.After(time.Now()) || !current.validUntil.After(time.Now()) || ctx.Err() != nil {
		return ErrProvisionBootDenied
	}
	// Current authority locks protect the short append transaction. The payload
	// deliberately contains no source URL, bootstrap secret, key or client report.
	_, err = tx.ExecContext(ctx, `INSERT INTO provision_installation_events(installation_id,event_type,payload,created_at)
 SELECT $1,'boot.file_served',jsonb_build_object('resource',$2::text,'sha256',$3::text,'bytes',$4::bigint,'full_response',true,'server_write_only',true),now()
 WHERE NOT EXISTS(SELECT 1 FROM provision_installation_events WHERE installation_id=$1 AND event_type='boot.file_served' AND payload->>'resource'=$2)`, current.installation.ID, delivered.Resource, sha, size)
	if err != nil {
		return err
	}
	return tx.Commit()
}
