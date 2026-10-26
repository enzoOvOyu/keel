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
	"regexp"
	"time"
)

var ErrRejected = errors.New("task runner request rejected")
var ErrUnknown = errors.New("task runner outcome requires reconciliation")
var idPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

const APIVersion = "keel.task/v1"

type TaskCommand struct {
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
	if c.Operation != "host.identity" && c.Operation != "host.wait" && c.Operation != "trust.verify" && c.Operation != "trust.install" && c.Operation != "script.run" {
		return ErrRejected
	}
	if c.Operation == "script.run" {
		if !idPattern.MatchString(c.ScriptMediaID) || !scriptDigestPattern.MatchString(c.ScriptSHA256) || c.ScriptSize < 1 || c.ScriptSize > MaxScriptBytes || c.AuthorizationExpiresAt < 1 || !validScriptVersion(c.ScriptObjectVersion) || (c.ScriptInterpreter != "shell" && c.ScriptInterpreter != "python") {
			return ErrRejected
		}
	} else if c.ScriptMediaID != "" || c.ScriptObjectVersion != "" || c.ScriptSHA256 != "" || c.ScriptSize != 0 || c.ScriptInterpreter != "" {
		return ErrRejected
	}
	if c.Operation == "trust.install" {
		if _, err := installPublicIdentity(c.PublicKey, c.ProjectID); err != nil || c.AuthorizationExpiresAt < 1 {
			return ErrRejected
		}
	} else if c.PublicKey != "" || c.Operation != "script.run" && c.AuthorizationExpiresAt != 0 {
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
	Action  string      `json:"action"`
	Command TaskCommand `json:"command"`
	Script  []byte      `json:"script,omitempty"`
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
	if c.Operation == "script.run" && o.Terminal() && validateScriptObservation(o) != nil {
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
