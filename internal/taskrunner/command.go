// Package taskrunner defines the transport-independent, bounded task protocol.
// Operations are an allowlist; script bodies are digest-bound media artifacts,
// never request-supplied command lines, interpreter paths or arguments.
package taskrunner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/enzoOvOyu/keel/internal/monitor"
	"github.com/enzoOvOyu/keel/internal/precollect"
	"regexp"
	"time"
)

var ErrRejected = errors.New("task runner request rejected")
var ErrUnknown = errors.New("task runner outcome requires reconciliation")
var idPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

const APIVersion = "keel.task/v1"

type TaskCommand struct {
	MonitorNetworkOnly     bool   `json:"monitor_network_only,omitempty"`
	ArtifactMediaID        string `json:"artifact_media_id,omitempty"`
	ArtifactObjectVersion  string `json:"artifact_object_version,omitempty"`
	ArtifactSHA256         string `json:"artifact_sha256,omitempty"`
	ArtifactSize           int64  `json:"artifact_size,omitempty"`
	ScriptCacheAllowed     bool   `json:"script_cache_allowed,omitempty"`
	CacheCursor            string `json:"cache_cursor,omitempty"`
	CacheManifestSHA256    string `json:"cache_manifest_sha256,omitempty"`
	ScriptResultFormat     string `json:"script_result_format,omitempty"`
	ScriptMediaID          string `json:"script_media_id,omitempty"`
	ScriptObjectVersion    string `json:"script_object_version,omitempty"`
	ScriptSHA256           string `json:"script_sha256,omitempty"`
	ScriptSize             int64  `json:"script_size,omitempty"`
	ScriptInterpreter      string `json:"script_interpreter,omitempty"`
	APIVersion             string `json:"api_version"`
	ProjectID              string `json:"project_id"`
	MachineID              string `json:"machine_id"`
	CommandID              string `json:"command_id"`
	AttemptID              string `json:"attempt_id"`
	FencingToken           int64  `json:"fencing_token"`
	Operation              string `json:"operation"`
	TimeoutMS              int    `json:"timeout_ms"`
	WaitMS                 int    `json:"wait_ms"`
	PublicKey              string `json:"public_key,omitempty"`
	AuthorizationExpiresAt int64  `json:"authorization_expires_at,omitempty"`
}

func (c TaskCommand) Validate() error {
	if c.APIVersion != APIVersion || !idPattern.MatchString(c.ProjectID) || !idPattern.MatchString(c.MachineID) || !idPattern.MatchString(c.CommandID) || !idPattern.MatchString(c.AttemptID) || c.FencingToken < 1 || c.TimeoutMS < 100 || c.TimeoutMS > 30000 {
		return ErrRejected
	}
	if c.Operation != "host.monitor" && c.Operation != "host.precollect" && c.Operation != "host.identity" && c.Operation != "host.wait" && c.Operation != "trust.verify" && c.Operation != "trust.install" && c.Operation != "script.run" && c.Operation != "host.cache.inventory" && c.Operation != "host.artifact.inventory" && c.Operation != "media.stage" && c.Operation != "media.fetch" {
		return ErrRejected
	}
	if c.Operation == "media.stage" || c.Operation == "media.fetch" {
		if validateArtifactCommand(c) != nil {
			return ErrRejected
		}
	} else if c.ArtifactMediaID != "" || c.ArtifactObjectVersion != "" || c.ArtifactSHA256 != "" || c.ArtifactSize != 0 {
		return ErrRejected
	}
	if c.Operation == "host.cache.inventory" || c.Operation == "host.artifact.inventory" {
		if c.CacheCursor != "" && (!scriptDigestPattern.MatchString(c.CacheCursor) || !scriptDigestPattern.MatchString(c.CacheManifestSHA256)) || c.CacheManifestSHA256 != "" && !scriptDigestPattern.MatchString(c.CacheManifestSHA256) {
			return ErrRejected
		}
	} else if c.CacheCursor != "" || c.CacheManifestSHA256 != "" {
		return ErrRejected
	}
	if c.Operation == "script.run" {
		if (c.ScriptResultFormat != "" && c.ScriptResultFormat != "json") || !idPattern.MatchString(c.ScriptMediaID) || !scriptDigestPattern.MatchString(c.ScriptSHA256) || c.ScriptSize < 1 || c.ScriptSize > MaxScriptBytes || c.AuthorizationExpiresAt < 1 || !validScriptVersion(c.ScriptObjectVersion) || (c.ScriptInterpreter != "shell" && c.ScriptInterpreter != "python") {
			return ErrRejected
		}
	} else if c.ScriptCacheAllowed || c.ScriptResultFormat != "" || c.ScriptMediaID != "" || c.ScriptObjectVersion != "" || c.ScriptSHA256 != "" || c.ScriptSize != 0 || c.ScriptInterpreter != "" {
		return ErrRejected
	}
	if c.Operation == "trust.install" {
		if _, err := installPublicIdentity(c.PublicKey, c.ProjectID); err != nil || c.AuthorizationExpiresAt < 1 {
			return ErrRejected
		}
	} else if c.PublicKey != "" || c.Operation != "host.monitor" && c.Operation != "host.precollect" && c.Operation != "script.run" && c.Operation != "media.stage" && c.Operation != "media.fetch" && c.AuthorizationExpiresAt != 0 {
		return ErrRejected
	}
	if (c.Operation == "host.precollect" || c.Operation == "host.monitor") && c.AuthorizationExpiresAt < 1 {
		return ErrRejected
	}
	if c.MonitorNetworkOnly && c.Operation != "host.monitor" {
		return ErrRejected
	}
	if (c.Operation != "host.wait" && c.WaitMS != 0) || c.WaitMS < 0 || c.WaitMS > 30000 {
		return ErrRejected
	}
	return nil
}

func (c TaskCommand) Digest() string {
	b, _ := json.Marshal(c)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

type Request struct {
	ChunkOffset     int64       `json:"chunk_offset,omitempty"`
	ChunkSHA256     string      `json:"chunk_sha256,omitempty"`
	Chunk           []byte      `json:"chunk,omitempty"`
	UseCachedScript bool        `json:"use_cached_script,omitempty"`
	Action          string      `json:"action"`
	Command         TaskCommand `json:"command"`
	Script          []byte      `json:"script,omitempty"`
}

// Validate binds the optional body to the immutable command before transport or
// receipt state can be consumed. Query/cancel/cache/settle never carry executable bytes.
// Cached runs carry only frozen metadata; the target verifies actual owned bytes.
func (r Request) Validate() error {
	if r.Command.Validate() != nil || (r.Action != "run" && r.Action != "query" && r.Action != "cancel" && r.Action != "cache" && r.Action != "settle" && r.Action != "append" && r.Action != "commit" && r.Action != "read") {
		return ErrRejected
	}
	if r.Action == "read" {
		if r.Command.Operation == "host.precollect" || r.Command.Operation == "host.monitor" {
			limit := int64(precollect.MaxBundleBytes)
			if r.Command.Operation == "host.monitor" {
				limit = monitor.MaxHostSnapshotBytes
			}
			if len(r.Script) != 0 || r.UseCachedScript || len(r.Chunk) != 0 || !scriptDigestPattern.MatchString(r.ChunkSHA256) || r.ChunkOffset < 0 || r.ChunkOffset >= limit || r.ChunkOffset%precollect.ChunkBytes != 0 {
				return ErrRejected
			}
			return nil
		}
		if r.Command.Operation != "media.fetch" || len(r.Script) != 0 || r.UseCachedScript || len(r.Chunk) != 0 || r.ChunkSHA256 != "" || r.ChunkOffset < 0 || r.ChunkOffset >= r.Command.ArtifactSize || r.ChunkOffset%ArtifactChunkBytes != 0 {
			return ErrRejected
		}
		return nil
	}
	if r.Action == "append" {
		if r.Command.Operation != "media.stage" || len(r.Script) != 0 || r.UseCachedScript || validateArtifactChunk(r) != nil {
			return ErrRejected
		}
		return nil
	}
	if len(r.Chunk) != 0 || r.ChunkOffset != 0 || r.ChunkSHA256 != "" || r.Action == "commit" && r.Command.Operation != "media.stage" && r.Command.Operation != "media.fetch" {
		return ErrRejected
	}
	if r.Action == "settle" && r.Command.Operation != "script.run" && r.Command.Operation != "media.stage" && r.Command.Operation != "media.fetch" {
		return ErrRejected
	}
	if r.Action == "cache" {
		if r.Command.Operation != "script.run" || !r.Command.ScriptCacheAllowed || r.UseCachedScript || len(r.Script) != 0 {
			return ErrRejected
		}
		return nil
	}
	if r.Command.Operation == "script.run" && r.Action == "run" {
		if r.UseCachedScript {
			if !r.Command.ScriptCacheAllowed || len(r.Script) != 0 {
				return ErrRejected
			}
			return nil
		}
		return validateScriptBody(r.Command, r.Script)
	}
	if len(r.Script) != 0 || r.UseCachedScript {
		return ErrRejected
	}
	return nil
}

type Observation struct {
	Command    TaskCommand    `json:"command"`
	State      string         `json:"state"` // accepted/running/success/failed/cancelled
	StartedAt  time.Time      `json:"started_at"`
	FinishedAt time.Time      `json:"finished_at"`
	Output     map[string]any `json:"output"`
}

func (o Observation) Terminal() bool {
	return o.State == "success" || o.State == "failed" || o.State == "cancelled"
}

func (o Observation) Validate(c TaskCommand) error {
	if o.Command != c || (o.State != "accepted" && o.State != "running" && !o.Terminal()) {
		return ErrRejected
	}
	if c.Operation == "host.monitor" && o.Terminal() && validateMonitorObservation(o) != nil {
		return ErrRejected
	}
	if c.Operation == "host.precollect" && o.Terminal() && validatePrecollectObservation(o) != nil {
		return ErrRejected
	}
	if c.Operation == "script.run" && o.Terminal() && validateScriptObservation(o) != nil {
		return ErrRejected
	}
	if (c.Operation == "media.stage" || c.Operation == "media.fetch") && o.Terminal() && validateArtifactObservation(o) != nil {
		return ErrRejected
	}
	if c.Operation == "host.artifact.inventory" && o.Terminal() && validateArtifactInventoryObservation(o) != nil {
		return ErrRejected
	}
	if c.Operation == "host.cache.inventory" && o.Terminal() && validateCacheObservation(o) != nil {
		return ErrRejected
	}
	if o.Terminal() && (o.StartedAt.IsZero() || o.FinishedAt.Before(o.StartedAt)) {
		return ErrRejected
	}
	return nil
}

// Binding is safe to freeze in the execution snapshot. Credentials and network
// coordinates remain exclusively in operator configuration.
type Binding struct {
	HostKey    string `json:"host_key"`
	ConfigHash string `json:"config_hash"`
}

type Runner interface {
	Binding(project, machine string) (Binding, error)
	Do(ctx context.Context, request Request) (Observation, error)
}
