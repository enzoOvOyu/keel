package platform

import (
	"context"
	"encoding/json"
	"github.com/enzoOvOyu/keel/internal/mediamanifest"
	"time"
)

// ExportMediaManifest attests to the approved registry and verified bytes at
// export time. It never grants future access or replaces vendor source signing.
func (s *Store) ExportMediaManifest(ctx context.Context, a *AuthSession, project *ProjectSummary, id, trace string) (out *mediamanifest.Envelope, resultErr error) {
	tx, err := s.beginControl(ctx, a)
	if err != nil {
		return nil, err
	}
	defer func() { tx.Rollback(); resultErr = s.RecordMediaIntegrityFailure(ctx, resultErr, a.User.ID, trace) }()
	if project == nil && !a.User.IsAdmin {
		return nil, ErrForbidden
	}
	if project != nil {
		if err = requireProjectMember(ctx, tx, a, project.ID); err != nil {
			return nil, err
		}
	}
	root, err := mediaForScope(ctx, tx, id, s.scopeKeyFor(project))
	if err != nil {
		return nil, err
	}
	if root.State != "approved" {
		return nil, apiError(409, "MEDIA_NOT_APPROVED", "Only approved media can be exported")
	}
	if s.MediaManifestSigner == nil {
		return nil, apiError(503, "MEDIA_SIGNING_UNAVAILABLE", "Platform manifest signing is not configured")
	}
	bytes, err := s.checkedMediaFile(ctx, tx, root)
	if err != nil {
		return nil, err
	}
	bytes.close()
	records, err := mediaDependencyClosure(ctx, tx, root, s)
	if err != nil {
		return nil, err
	}
	payload := mediamanifest.Payload{RootID: root.ID, ExportedAt: time.Now().UTC(), Artifacts: make([]mediamanifest.Artifact, 0, len(records))}
	for _, record := range records {
		var objectVersion string
		if err = tx.QueryRowContext(ctx, "SELECT object_version FROM media_artifacts WHERE media_id=$1", record.ID).Scan(&objectVersion); err != nil {
			return nil, err
		}
		refs, e := mediaArtifactDependencies(record.Deps)
		if e != nil {
			return nil, e
		}
		compatibility, e := decodeCompatibility(record.Deps)
		if e != nil {
			return nil, apiError(422, "MEDIA_DEPENDENCY_INVALID", "Stored compatibility metadata is invalid")
		}
		node := mediamanifest.Artifact{MediaID: record.ID, ScopeKey: record.ScopeKey, URI: mediaURI(record.Type, record.Name, record.Version), Type: record.Type, Name: record.Name, Version: record.Version, Arch: record.Arch, SHA256: record.Sha256, Size: record.Size, ObjectVersion: objectVersion, ApprovedBy: record.ApprovedBy, ApprovedAt: record.UpdatedAt, Dependencies: []mediamanifest.Dependency{}}
		for _, ref := range refs {
			node.Dependencies = append(node.Dependencies, mediamanifest.Dependency{MediaID: ref.MediaID, SHA256: ref.SHA256})
		}
		if compatibility != nil {
			node.Compatibility, e = json.Marshal(compatibility)
			if e != nil {
				return nil, e
			}
		}
		payload.Artifacts = append(payload.Artifacts, node)
	}
	mediamanifest.Sort(payload.Artifacts)
	manifest, err := s.MediaManifestSigner.Sign(payload)
	if err != nil {
		return nil, apiError(422, "MEDIA_MANIFEST_INVALID", "Media metadata cannot form a complete signed manifest")
	}
	if err = appendAudit(ctx, tx, a.User.ID, projectIDOf(project), "media.manifest.export", root.ID, trace, map[string]any{"key_id": manifest.Payload.KeyID, "artifact_count": len(records), "root_sha256": root.Sha256, "schema": mediamanifest.Schema}); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return manifest, nil
}
