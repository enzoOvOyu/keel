package platform

import (
	"context"
	"encoding/json"
)

type ProvisionOSDriver struct {
	MediaArtifactDependency
	Compatibility *Compatibility `json:"compatibility,omitempty"`
}
type ProvisionOSDependencies struct {
	MediaID       string              `json:"media_id"`
	SHA256        string              `json:"sha256"`
	Arch          string              `json:"arch"`
	Drivers       []ProvisionOSDriver `json:"drivers"`
	Compatibility *Compatibility      `json:"compatibility,omitempty"`
}

// Selection hints read approved immutable registry relationships only. They do
// not stream/hash multi-GiB images or assert that a package is preinstalled.
// Preparation and dispatch retain the original full integrity/admission gates.
func (s *Store) ProvisionOSDependencies(ctx context.Context, a *AuthSession, project *ProjectSummary, id string) (*ProvisionOSDependencies, error) {
	if project == nil || !modelIDPattern.MatchString(id) {
		return nil, ErrNotFound
	}
	tx, err := s.beginPinnedRead(ctx, a)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = requireProvisionReadAuthority(ctx, tx, a, project.ID, false); err != nil {
		return nil, err
	}
	root, err := mediaForScopeLocked(ctx, tx, id, s.scopeKeyFor(project), true)
	if err != nil {
		return nil, err
	}
	if root.Type != "os" || root.State != "approved" || !root.ArtifactStored || root.ArtifactQuarantined {
		return nil, apiError(409, "PROVISION_OS_DEPENDENCIES_UNAVAILABLE", "Select an approved, stored, non-quarantined OS image")
	}
	var document catalogDependencies
	if json.Unmarshal(root.Deps, &document) != nil || document.OSBoot == nil {
		return nil, apiError(422, "MEDIA_BOOT_UNDEFINED", "OS image requires a registered boot profile")
	}
	compatibility, err := decodeCompatibility(root.Deps)
	if err != nil {
		return nil, apiError(422, "MEDIA_BOOT_INVALID", "OS image dependency metadata is invalid")
	}
	records, err := mediaDependencyClosure(ctx, tx, root, nil)
	if err != nil {
		return nil, err
	}
	out := &ProvisionOSDependencies{MediaID: root.ID, SHA256: root.Sha256, Arch: root.Arch, Drivers: []ProvisionOSDriver{}, Compatibility: compatibility}
	for _, record := range records {
		if record.Type != "driver" {
			continue
		}
		matrix, err := decodeCompatibility(record.Deps)
		if err != nil {
			return nil, apiError(422, "MEDIA_DEPENDENCY_INVALID", "Driver dependency metadata is invalid")
		}
		out.Drivers = append(out.Drivers, ProvisionOSDriver{MediaArtifactDependency: MediaArtifactDependency{MediaID: record.ID, SHA256: record.Sha256, Type: record.Type, Name: record.Name, Version: record.Version, Arch: record.Arch}, Compatibility: matrix})
	}
	return out, tx.Commit()
}
