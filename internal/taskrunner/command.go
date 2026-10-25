// Package taskrunner defines the transport-independent, bounded task protocol.
// Operations are an allowlist; a request never contains a shell command.
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
	APIVersion   string `json:"api_version"`
	ProjectID    string `json:"project_id"`
	MachineID    string `json:"machine_id"`
	CommandID    string `json:"command_id"`
	AttemptID    string `json:"attempt_id"`
	FencingToken int64  `json:"fencing_token"`
	Operation    string `json:"operation"`
	TimeoutMS    int    `json:"timeout_ms"`
	WaitMS       int    `json:"wait_ms"`
}

func (c TaskCommand) Validate() error {
	if c.APIVersion != APIVersion || !idPattern.MatchString(c.ProjectID) || !idPattern.MatchString(c.MachineID) || !idPattern.MatchString(c.CommandID) || !idPattern.MatchString(c.AttemptID) || c.FencingToken < 1 || c.TimeoutMS < 100 || c.TimeoutMS > 30000 {
		return ErrRejected
	}
	if c.Operation != "host.identity" && c.Operation != "host.wait" {
		return ErrRejected
	}
	if (c.Operation == "host.identity" && c.WaitMS != 0) || c.WaitMS < 0 || c.WaitMS > 30000 {
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
