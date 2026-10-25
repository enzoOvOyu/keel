package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// A dependency pins an immutable registry identity and digest. Display metadata
// is resolved by the server when registering the parent, never trusted input.
type MediaArtifactDependency struct {
	MediaID string `json:"media_id"`
	SHA256  string `json:"sha256"`
	Type    string `json:"type,omitempty"`
	Name    string `json:"name,omitempty"`
	Version string `json:"version,omitempty"`
	Arch    string `json:"arch,omitempty"`
}

func mediaArtifactDependencies(raw json.RawMessage) ([]MediaArtifactDependency, error) {
	var document catalogDependencies
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, apiError(400, "VALIDATION_ERROR", "Invalid media dependency document")
	}
	if len(document.Artifacts) > 32 {
		return nil, apiError(400, "VALIDATION_ERROR", "At most 32 direct artifact dependencies are supported")
	}
	seen := map[string]bool{}
	for _, ref := range document.Artifacts {
		if !modelIDPattern.MatchString(ref.MediaID) || !sha256Pattern.MatchString(ref.SHA256) || seen[ref.MediaID] {
			return nil, apiError(400, "VALIDATION_ERROR", "Artifact dependencies require unique media IDs and lowercase SHA256 digests")
		}
		seen[ref.MediaID] = true
	}
	return document.Artifacts, nil
}

func mediaDependencyRecord(ctx context.Context, tx *sql.Tx, id string) (MediaItemSummary, error) {
	var out MediaItemSummary
	err := tx.QueryRowContext(ctx, `SELECT id,scope_key,type,name,version,sha256,size,arch,deps,state,
 EXISTS(SELECT 1 FROM media_artifacts a WHERE a.media_id=m.id)
 FROM media_items m WHERE id=$1 FOR SHARE`, id).Scan(&out.ID, &out.ScopeKey, &out.Type, &out.Name, &out.Version, &out.Sha256, &out.Size, &out.Arch, &out.Deps, &out.State, &out.ArtifactStored)
	if errors.Is(err, sql.ErrNoRows) {
		return out, apiError(422, "MEDIA_DEPENDENCY_UNAVAILABLE", "A pinned media dependency is unavailable")
	}
	return out, err
}

// The legitimate registry is a DAG: immutable metadata can only point to
// already registered, approved identities. Bounds/cycle checks also reject
// malformed historical data without recursively trusting it.
func validateMediaDependencyTree(ctx context.Context, tx *sql.Tx, root *MediaItemSummary, reader *Store) error {
	records := map[string]MediaItemSummary{root.ID: *root}
	visiting := map[string]bool{}
	complete := map[string]bool{}
	heights := map[string]int{}
	var visit func(*MediaItemSummary, int) error
	visit = func(parent *MediaItemSummary, depth int) error {
		if depth > 16 {
			return apiError(422, "MEDIA_DEPENDENCY_LIMIT", "Artifact dependency depth exceeds 16")
		}
		if visiting[parent.ID] {
			return apiError(422, "MEDIA_DEPENDENCY_CYCLE", "Artifact dependencies contain a cycle")
		}
		if complete[parent.ID] {
			if depth+heights[parent.ID] > 16 {
				return apiError(422, "MEDIA_DEPENDENCY_LIMIT", "Artifact dependency depth exceeds 16")
			}
			return nil
		}
		visiting[parent.ID] = true
		refs, err := mediaArtifactDependencies(parent.Deps)
		if err != nil {
			return err
		}
		for _, ref := range refs {
			child, known := records[ref.MediaID]
			if !known {
				if len(records) >= 129 {
					return apiError(422, "MEDIA_DEPENDENCY_LIMIT", "Artifact dependency closure exceeds 128 dependencies")
				}
				child, err = mediaDependencyRecord(ctx, tx, ref.MediaID)
				if err != nil {
					return err
				}
				records[ref.MediaID] = child
			}
			if child.ScopeKey != parent.ScopeKey && child.ScopeKey != "global" {
				return apiError(422, "MEDIA_DEPENDENCY_UNAVAILABLE", "A pinned media dependency is unavailable")
			}
			if child.State != "approved" || !child.ArtifactStored {
				return apiError(422, "MEDIA_DEPENDENCY_UNAVAILABLE", "A pinned media dependency is not approved and stored")
			}
			if child.Sha256 != ref.SHA256 {
				return apiError(422, "MEDIA_DEPENDENCY_IDENTITY_MISMATCH", "A pinned dependency digest differs from its registered identity")
			}
			if child.Arch != "noarch" && child.Arch != parent.Arch {
				return apiError(422, "MEDIA_DEPENDENCY_ARCH_MISMATCH", "A pinned dependency has an incompatible CPU architecture")
			}
			if !complete[child.ID] && reader != nil {
				file, e := reader.checkedMediaFile(ctx, tx, &child)
				if e != nil {
					return e
				}
				file.close()
			}
			if err = visit(&child, depth+1); err != nil {
				return err
			}
		}
		for _, ref := range refs {
			if height := heights[ref.MediaID] + 1; height > heights[parent.ID] {
				heights[parent.ID] = height
			}
		}
		delete(visiting, parent.ID)
		complete[parent.ID] = true
		return nil
	}
	return visit(root, 0)
}

func checkPinnedMediaDependencies(ctx context.Context, tx *sql.Tx, id string, reader *Store) error {
	root, err := mediaDependencyRecord(ctx, tx, id)
	if err != nil {
		return err
	}
	return validateMediaDependencyTree(ctx, tx, &root, reader)
}

func canonicalMediaDependencies(ctx context.Context, tx *sql.Tx, m *MediaItemSummary) error {
	if err := validateMediaDependencyTree(ctx, tx, m, nil); err != nil {
		return err
	}
	refs, err := mediaArtifactDependencies(m.Deps)
	if err != nil || len(refs) == 0 {
		return err
	}
	for i, ref := range refs {
		child, e := mediaDependencyRecord(ctx, tx, ref.MediaID)
		if e != nil {
			return e
		}
		refs[i] = MediaArtifactDependency{MediaID: child.ID, SHA256: child.Sha256, Type: child.Type, Name: child.Name, Version: child.Version, Arch: child.Arch}
	}
	var document map[string]any
	if err = json.Unmarshal(m.Deps, &document); err != nil {
		return err
	}
	document["artifacts"] = refs
	m.Deps, err = json.Marshal(document)
	return err
}
