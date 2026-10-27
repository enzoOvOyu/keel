// Package bmc implements read-only BMC inventory boundaries. It does not grant
// resource leases, admit executions or perform configuration/power operations.
package bmc

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"time"
)

const ParserVersion = "redfish-inventory/v1"
const MaxResourceBytes = 2 << 20
const MaxTotalBytes = 32 << 20

var stableID = regexp.MustCompile(`^[a-f0-9]{32}$`)
var sha256Pattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type Target struct {
	BOMPackageID       string `json:"bom_package_id,omitempty"`
	AdapterBindingHash string `json:"adapter_binding_hash,omitempty"`
	RequestID          string `json:"request_id"`
	ProjectID          string `json:"project_id"`
	AssetID            string `json:"asset_id"`
	MachineID          string `json:"machine_id"`
	AssetVersion       int64  `json:"asset_version"`
	Serial             string `json:"serial"`
	CredentialBatchRef string `json:"credential_batch_ref"`
	CredentialVersion  string `json:"credential_version"`
}

func (t Target) Validate() error {
	if t.BOMPackageID != "" && !stableID.MatchString(t.BOMPackageID) {
		return errors.New("invalid frozen BOM package identity")
	}
	if t.AdapterBindingHash != "" && !sha256Pattern.MatchString(t.AdapterBindingHash) {
		return errors.New("invalid BMC adapter binding hash")
	}
	if !stableID.MatchString(t.RequestID) || !stableID.MatchString(t.ProjectID) || !stableID.MatchString(t.AssetID) || !stableID.MatchString(t.MachineID) || t.AssetVersion < 1 || len(t.Serial) < 1 || len(t.Serial) > 120 || len(t.CredentialBatchRef) < 1 || len(t.CredentialBatchRef) > 120 || len(t.CredentialVersion) < 1 || len(t.CredentialVersion) > 120 {
		return errors.New("invalid BMC target or frozen credential identity")
	}
	return nil
}

type RawReference struct {
	Key     string `json:"key"`
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size"`
}
type RawSink interface {
	StoreRaw(context.Context, Target, string, []byte) (RawReference, error)
}

// Reader is implemented by the HTTPS protocol client and deterministic fixture
// reader. Origins must never be inferred from whether requests happen locally.
type Reader interface {
	Read(context.Context, string) (Document, error)
	Resolve(string, string) (string, error)
	Origin() string
}
type Document struct {
	Path       string
	Body       []byte
	ReceivedAt time.Time
}
type Failure struct {
	Path       string `json:"path"`
	Class      string `json:"class"`
	Code       string `json:"code"`
	HTTPStatus int    `json:"http_status,omitempty"`
	Retryable  bool   `json:"retryable"`
}

func (f *Failure) Error() string { return f.Class + ":" + f.Code }
func failure(path, class, code string, retry bool) *Failure {
	return &Failure{Path: path, Class: class, Code: code, Retryable: retry}
}

type Resource struct {
	Path       string          `json:"path"`
	Type       string          `json:"type"`
	DataSHA256 string          `json:"data_sha256"`
	Source     string          `json:"source"`
	ReceivedAt time.Time       `json:"received_at"`
	Raw        RawReference    `json:"raw"`
	Data       json.RawMessage `json:"data"`
}
type Snapshot struct {
	Fallback        *FallbackInfo     `json:"fallback,omitempty"`
	Version         string            `json:"version"`
	Target          Target            `json:"target"`
	Level           string            `json:"level"`
	Origin          string            `json:"origin"`
	Source          string            `json:"source"`
	ParserVersion   string            `json:"parser_version"`
	State           string            `json:"state"`
	IdentityMatched bool              `json:"identity_matched"`
	StartedAt       time.Time         `json:"started_at"`
	CollectedAt     time.Time         `json:"collected_at"`
	Resources       []Resource        `json:"resources"`
	Failures        []Failure         `json:"failures"`
	Capabilities    map[string]string `json:"capabilities"`
	Degraded        bool              `json:"degraded"`
}
type Config struct {
	MaxResources   int
	MaxDepth       int
	MaxBytes       int64
	MinimumVersion string
}

func DefaultConfig() Config {
	return Config{MaxResources: 1024, MaxDepth: 8, MaxBytes: MaxTotalBytes, MinimumVersion: "1.0.0"}
}
