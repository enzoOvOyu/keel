package platform

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/enzoOvOyu/keel/internal/mediaboot"
)

type MediaBootArtifact struct {
	Role          string `json:"role"`
	MediaID       string `json:"media_id"`
	ScopeKey      string `json:"scope_key"`
	Name          string `json:"name"`
	Version       string `json:"version"`
	SHA256        string `json:"sha256"`
	Size          int64  `json:"size"`
	Arch          string `json:"arch"`
	ObjectVersion string `json:"object_version"`
}
type MediaBootPlan struct {
	RootID    string              `json:"root_id"`
	Profile   mediaboot.Profile   `json:"profile"`
	Artifacts []MediaBootArtifact `json:"artifacts"`
}

// ResolveMediaBootPlan resolves approved OS roles and verifies actual bytes of
// the entire pinned closure. It grants no download capability and starts no
// provisioning. A later provisioning transaction must revalidate admission.
func (s *Store) ResolveMediaBootPlan(ctx context.Context, a *AuthSession, project *ProjectSummary, id, trace string) (out *MediaBootPlan, resultErr error) {
	tx, err := s.beginPinnedRead(ctx, a)
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
	out, err = s.resolveMediaBootPlanTx(ctx, tx, project, id)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) resolveMediaBootPlanTx(ctx context.Context, tx *sql.Tx, project *ProjectSummary, id string) (*MediaBootPlan, error) {
	root, err := mediaForScopeLocked(ctx, tx, id, s.scopeKeyFor(project), true)
	if err != nil {
		return nil, err
	}
	var document catalogDependencies
	if len(root.Deps) > 0 {
		if err = json.Unmarshal(root.Deps, &document); err != nil {
			return nil, apiError(422, "MEDIA_BOOT_INVALID", "Invalid OS boot metadata")
		}
	}
	if document.OSBoot == nil {
		return nil, apiError(422, "MEDIA_BOOT_UNDEFINED", "This media has no OS boot profile")
	}
	if root.State != "approved" {
		return nil, apiError(409, "MEDIA_NOT_APPROVED", "OS boot media must be approved")
	}
	file, err := s.checkedMediaFile(ctx, tx, root)
	if err != nil {
		return nil, err
	}
	file.close()
	records, err := mediaDependencyClosure(ctx, tx, root, s)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]MediaItemSummary, len(records))
	for _, record := range records {
		byID[record.ID] = record
	}
	out := &MediaBootPlan{RootID: root.ID, Profile: *document.OSBoot, Artifacts: []MediaBootArtifact{}}
	roles := []struct{ role, id string }{{"rootfs", root.ID}, {"kernel", document.OSBoot.KernelMediaID}, {"initrd", document.OSBoot.InitrdMediaID}, {"template", document.OSBoot.TemplateMediaID}}
	for _, role := range roles {
		record := byID[role.id]
		var version string
		if err = tx.QueryRowContext(ctx, "SELECT object_version FROM media_artifacts WHERE media_id=$1", record.ID).Scan(&version); err != nil {
			return nil, err
		}
		out.Artifacts = append(out.Artifacts, MediaBootArtifact{Role: role.role, MediaID: record.ID, ScopeKey: record.ScopeKey, Name: record.Name, Version: record.Version, SHA256: record.Sha256, Size: record.Size, Arch: record.Arch, ObjectVersion: version})
	}
	return out, nil
}
