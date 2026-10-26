package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"time"

	"github.com/enzoOvOyu/keel/internal/taskrunner"
)

// TaskScriptArtifact freezes public, approved metadata only. Script bytes are
// fetched from the exact object version immediately before dispatch, not stored
// in execution snapshots, HTTP request bodies or audit entries.
type TaskScriptArtifact struct {
	MediaID       string    `json:"media_id"`
	Name          string    `json:"name"`
	Version       string    `json:"version"`
	SHA256        string    `json:"sha256"`
	Size          int64     `json:"size"`
	ObjectVersion string    `json:"object_version"`
	Interpreter   string    `json:"interpreter"`
	ApprovedBy    string    `json:"approved_by"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (p TaskScriptArtifact) Command(c taskrunner.TaskCommand) taskrunner.TaskCommand {
	c.ScriptMediaID = p.MediaID
	c.ScriptSHA256 = p.SHA256
	c.ScriptSize = p.Size
	c.ScriptObjectVersion = p.ObjectVersion
	c.ScriptInterpreter = p.Interpreter
	return c
}

func (s *Store) ResolveTaskScriptTx(ctx context.Context, tx *sql.Tx, project, mediaID string, frozen *TaskScriptArtifact, withBytes bool) (*TaskScriptArtifact, []byte, error) {
	if s == nil || (!s.SSHScriptsEnabled && !s.AgentScriptsEnabled) {
		return nil, nil, apiError(409, "TASK_SCRIPTS_DISABLED", "Approved script execution is not enabled")
	}
	m, err := mediaForScopeLocked(ctx, tx, mediaID, s.scopeKeyFor(&ProjectSummary{ID: project}), true)
	if err != nil {
		return nil, nil, err
	}
	if m.State != "approved" || m.Type != "tool" || !m.ArtifactStored || m.ArtifactQuarantined || m.Arch != "noarch" || m.OsScope != "linux" || m.HwScope != "" || m.Size < 1 || m.Size > taskrunner.MaxScriptBytes || m.ApprovedBy == "" || m.UploadedBy == m.ApprovedBy {
		return nil, nil, apiError(409, "TASK_SCRIPT_NOT_APPROVED", "An independently approved, stored noarch Linux tool script of 1..8192 bytes is required")
	}
	var metadata catalogDependencies
	if json.Unmarshal(m.Deps, &metadata) != nil || metadata.TaskScript == nil || metadata.TaskScript.Validate() != nil {
		return nil, nil, apiError(409, "TASK_SCRIPT_CONTRACT_REQUIRED", "Approved script media must declare a fixed shell or python task_script profile")
	}
	interpreter := metadata.TaskScript.Interpreter
	if metadata.Compatibility != nil || metadata.OSBoot != nil {
		return nil, nil, apiError(409, "TASK_SCRIPT_DEPENDENCIES_UNSUPPORTED", "This script profile does not evaluate compatibility matrices or OS boot dependencies")
	}
	deps, err := mediaArtifactDependencies(m.Deps)
	if err != nil {
		return nil, nil, err
	}
	if len(deps) > 0 {
		return nil, nil, apiError(409, "TASK_SCRIPT_DEPENDENCIES_UNSUPPORTED", "Script dependency installation is not supported by this execution profile")
	}
	p := &TaskScriptArtifact{MediaID: m.ID, Name: m.Name, Version: m.Version, SHA256: m.Sha256, Size: m.Size, Interpreter: interpreter, ApprovedBy: m.ApprovedBy, UpdatedAt: m.UpdatedAt}
	var storedHash string
	var storedSize int64
	if err = tx.QueryRowContext(ctx, `SELECT object_version,sha256,size FROM media_artifacts WHERE media_id=$1 FOR SHARE`, m.ID).Scan(&p.ObjectVersion, &storedHash, &storedSize); err != nil {
		return nil, nil, err
	}
	if storedHash != p.SHA256 || storedSize != p.Size {
		return nil, nil, apiError(409, "TASK_SCRIPT_ARTIFACT_CHANGED", "Script artifact metadata differs from its approved registry identity")
	}
	check := p.Command(taskrunner.TaskCommand{APIVersion: taskrunner.APIVersion, ProjectID: project, MachineID: project, CommandID: project, AttemptID: project, FencingToken: 1, Operation: "script.run", TimeoutMS: 5000, AuthorizationExpiresAt: 1})
	if check.Validate() != nil {
		return nil, nil, apiError(409, "TASK_SCRIPT_INVALID", "Approved script has invalid immutable metadata")
	}
	if frozen != nil {
		normalized := *frozen
		normalized.UpdatedAt = p.UpdatedAt
		if normalized != *p || !frozen.UpdatedAt.Equal(p.UpdatedAt) {
			return nil, nil, apiError(409, "TASK_SCRIPT_CHANGED", "Script approval or immutable artifact changed after the execution was frozen")
		}
	}
	if err = mediaIntegrityGate(ctx, tx, m.ID, true); err != nil {
		return nil, nil, err
	}
	if !withBytes {
		return p, nil, nil
	}
	file, err := s.checkedMediaFile(ctx, tx, m)
	if err != nil {
		return nil, nil, err
	}
	defer file.close()
	body, err := io.ReadAll(io.LimitReader(file.file, taskrunner.MaxScriptBytes+1))
	if err != nil || int64(len(body)) != p.Size {
		return nil, nil, apiError(409, "TASK_SCRIPT_READ_FAILED", "Approved script could not be read completely")
	}
	return p, body, nil
}

// ListTaskScripts does not fetch artifact bytes or connect to a target.
func (s *Store) ListTaskScripts(ctx context.Context, a *AuthSession, project *ProjectSummary, page Pagination) (Page[TaskScriptArtifact], bool, error) {
	return s.ListTaskScriptsForTransport(ctx, a, project, page, "ssh")
}

func (s *Store) ListTaskScriptsForTransport(ctx context.Context, a *AuthSession, project *ProjectSummary, page Pagination, transport string) (Page[TaskScriptArtifact], bool, error) {
	out := NewPage[TaskScriptArtifact](page)
	if err := page.Validate(); err != nil {
		return out, false, err
	}
	tx, err := s.beginControlRead(ctx, a)
	if err != nil {
		return out, false, err
	}
	defer tx.Rollback()
	if err = requireProjectMember(ctx, tx, a, project.ID); err != nil {
		return out, false, err
	}
	if transport != "ssh" && transport != "agent" {
		return out, false, apiError(422, "INVALID_TRANSPORT", "Script transport must be ssh or agent")
	}
	if transport == "ssh" && !s.SSHScriptsEnabled || transport == "agent" && (!s.AgentScriptsEnabled || !s.AgentTasksEnabled) {
		return out, false, tx.Commit()
	}
	filter := `(m.scope_key=$1 OR m.scope_key='global') AND m.state='approved' AND m.type='tool' AND m.arch='noarch' AND m.os_scope='linux' AND m.hw_scope='' AND m.size BETWEEN 1 AND 8192 AND m.approved_by IS NOT NULL AND m.approved_by<>m.uploaded_by AND m.deps->'task_script'->>'version'='keel.task-script/v1' AND m.deps->'task_script'->>'interpreter' IN('shell','python') AND (m.deps->'artifacts' IS NULL OR m.deps->'artifacts'='[]'::jsonb) AND (m.deps->'compatibility' IS NULL OR m.deps->'compatibility'='null'::jsonb) AND (m.deps->'os_boot' IS NULL OR m.deps->'os_boot'='null'::jsonb) AND a.sha256=m.sha256 AND a.size=m.size AND NOT EXISTS(SELECT 1 FROM media_artifact_integrity i WHERE i.media_id=m.id AND i.quarantined_at IS NOT NULL)`
	from := ` FROM media_items m JOIN media_artifacts a ON a.media_id=m.id WHERE ` + filter
	scope := s.scopeKeyFor(project)
	if err = tx.QueryRowContext(ctx, `SELECT count(*)`+from, scope).Scan(&out.Total); err != nil {
		return out, true, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT m.id,m.name,m.version,m.sha256,m.size,a.object_version,m.deps->'task_script'->>'interpreter',m.approved_by,m.updated_at`+from+` ORDER BY m.name,m.version DESC,m.id LIMIT $2 OFFSET $3`, scope, page.PageSize, page.Offset())
	if err != nil {
		return out, true, err
	}
	for rows.Next() {
		var item TaskScriptArtifact
		if err = rows.Scan(&item.MediaID, &item.Name, &item.Version, &item.SHA256, &item.Size, &item.ObjectVersion, &item.Interpreter, &item.ApprovedBy, &item.UpdatedAt); err != nil {
			rows.Close()
			return out, true, err
		}
		out.Items = append(out.Items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, true, err
	}
	return out, true, tx.Commit()
}
