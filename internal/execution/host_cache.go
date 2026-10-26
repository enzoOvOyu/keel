package execution

import (
	"context"
	"database/sql"
)

func (s *Service) cacheInventoryAdmission(operation string) error {
	if operation == "host.cache.inventory" && (s.MediaStore == nil || !s.MediaStore.TaskCacheInventoryEnabled) {
		return failure(409, "CACHE_INVENTORY_DISABLED", "Platform-owned media cache inventory is not enabled")
	}
	if operation == "host.artifact.inventory" && (s.MediaStore == nil || !s.MediaStore.ArtifactTransfersEnabled) {
		return failure(409, "ARTIFACT_TRANSFERS_DISABLED", "Artifact inventory is not enabled")
	}
	return nil
}

func (s *Service) hostTaskFrozenTx(ctx context.Context, tx *sql.Tx, project, mode string, task Task) error {
	if err := s.cacheInventoryAdmission(task.Type); err != nil {
		return err
	}
	if err := s.artifactFrozenTx(ctx, tx, project, task); err != nil {
		return err
	}
	return s.scriptFrozenTx(ctx, tx, project, mode, task)
}
