package provisionboot

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"github.com/enzoOvOyu/keel/internal/provisionpreflight"
	"time"
	"unicode/utf8"
)

const BootProofSchema = "keel.provision-boot-proof/v1"
const bootProofDomain = "Keel provision boot proof v1\x00"

// BootProof is authenticated separately from public Grant IDs, DHCP identity
// and self-reported hardware. A private per-boot key signs the complete payload
// digest and exact installation/boot/sequence/operation binding.
type BootProof struct {
	Schema         string `json:"schema"`
	InstallationID string `json:"installation_id"`
	BootID         string `json:"boot_id"`
	Sequence       int64  `json:"sequence"`
	Operation      string `json:"operation"`
	PayloadSHA256  string `json:"payload_sha256"`
	Signature      string `json:"signature"`
}
type BootEnrollment struct {
	InstallationID string `json:"installation_id"`
	BootID         string `json:"boot_id"`
	PublicKey      string `json:"public_key"`
}

type BootAcknowledgment struct {
	InstallationID string    `json:"installation_id"`
	BootID         string    `json:"boot_id"`
	Sequence       int64     `json:"sequence"`
	State          string    `json:"state"`
	ExpiresAt      time.Time `json:"expires_at"`
}
type EnrollmentSubmission struct {
	Enrollment BootEnrollment `json:"enrollment"`
	Proof      BootProof      `json:"proof"`
}

// Report is base64 in the envelope so the signed helper stdout bytes survive
// JSON serialization exactly; it is decoded and recomputed by the authority.
type PreflightSubmission struct {
	Report    []byte    `json:"report"`
	Proof     BootProof `json:"proof"`
	RefreshID string    `json:"refresh_id,omitempty"`
}

// Review uses the last acknowledged sequence without consuming it. This is a
// read-only, nonce-bound query; it cannot authorize a write or mutate versions.
type ReviewQuery struct {
	Nonce string `json:"nonce"`
}
type ReviewSubmission struct {
	Query ReviewQuery `json:"query"`
	Proof BootProof   `json:"proof"`
}
type ReviewStatus struct {
	InstallationID    string    `json:"installation_id"`
	BootID            string    `json:"boot_id"`
	Sequence          int64     `json:"sequence"`
	Nonce             string    `json:"nonce"`
	State             string    `json:"state"`
	ExpiresAt         time.Time `json:"expires_at"`
	ReportSHA256      string    `json:"report_sha256"`
	RefreshID         string    `json:"refresh_id,omitempty"`
	StoragePlanSHA256 string    `json:"storage_plan_sha256,omitempty"`
}

// PreflightProofPayload binds refresh authority to the exact raw helper bytes.
// Initial reporting keeps the original raw-byte transcript.
func PreflightProofPayload(in PreflightSubmission) ([]byte, string, error) {
	if len(in.Report) == 0 || len(in.Report) > 2<<20 {
		return nil, "", ErrDenied
	}
	if in.RefreshID == "" {
		return in.Report, "preflight", nil
	}
	if !opaqueID.MatchString(in.RefreshID) {
		return nil, "", ErrDenied
	}
	payload := append([]byte("Keel preflight refresh v1\x00"+in.RefreshID+"\x00"), in.Report...)
	return payload, "preflight-refresh", nil
}

const MaximumCallbackBytes = (2<<20)*4/3 + 8192

const BootstrapSchema = "keel.provision-boot-bootstrap/v1"

type BootstrapBinding struct {
	ID             string                  `json:"id"`
	InstallationID string                  `json:"installation_id"`
	ProjectID      string                  `json:"project_id"`
	GrantID        string                  `json:"grant_id"`
	Spec           provisionpreflight.Spec `json:"spec"`
	ExpiresAt      time.Time               `json:"expires_at"`
}
type Bootstrap struct {
	Schema  string           `json:"schema"`
	Token   string           `json:"token"`
	Binding BootstrapBinding `json:"binding"`
}

// The machine protocol emits canonical JSON, like public boot manifests.
// Exact re-encoding rejects unknown, duplicate and case-aliased fields without
// rewriting any signed report bytes inside the base64 field.
func DecodeCallback(raw []byte, value any) error {
	if len(raw) == 0 || len(raw) > MaximumCallbackBytes || !utf8.Valid(raw) || json.Unmarshal(raw, value) != nil {
		return ErrDenied
	}
	canonical, err := json.Marshal(value)
	if err != nil || !bytes.Equal(canonical, raw) {
		return ErrDenied
	}
	return nil
}

func proofOperation(value string) bool {
	switch value {
	case "enroll", "preflight", "preflight-refresh", "review", "write-permit", "install-stopped", "first-boot":
		return true
	}
	return false
}
func (p BootProof) message() ([]byte, error) {
	if p.Schema != BootProofSchema || !opaqueID.MatchString(p.InstallationID) || !opaqueID.MatchString(p.BootID) || p.Sequence < 1 || p.Sequence > 4096 || !proofOperation(p.Operation) || !sha256ID.MatchString(p.PayloadSHA256) {
		return nil, ErrDenied
	}
	p.Signature = ""
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, ErrDenied
	}
	return append([]byte(bootProofDomain), raw...), nil
}
func maximumProofBytes(operation string) int {
	if operation == "preflight-refresh" {
		return (2 << 20) + 8192
	}
	return 2 << 20
}
func SignBootProof(key ed25519.PrivateKey, installation, boot string, sequence int64, operation string, payload []byte) (BootProof, error) {
	p := BootProof{Schema: BootProofSchema, InstallationID: installation, BootID: boot, Sequence: sequence, Operation: operation}
	if len(key) != ed25519.PrivateKeySize || len(payload) == 0 || len(payload) > maximumProofBytes(operation) {
		return BootProof{}, ErrDenied
	}
	hash := sha256.Sum256(payload)
	p.PayloadSHA256 = hex.EncodeToString(hash[:])
	message, err := p.message()
	if err != nil {
		return BootProof{}, err
	}
	p.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(key, message))
	return p, nil
}
func VerifyBootProof(key ed25519.PublicKey, installation, boot string, next int64, operation string, payload []byte, p BootProof) error {
	if len(key) != ed25519.PublicKeySize || len(payload) == 0 || len(payload) > maximumProofBytes(operation) || p.InstallationID != installation || p.BootID != boot || p.Sequence != next || p.Operation != operation {
		return ErrDenied
	}
	hash := sha256.Sum256(payload)
	if p.PayloadSHA256 != hex.EncodeToString(hash[:]) {
		return ErrDenied
	}
	message, err := p.message()
	if err != nil {
		return ErrDenied
	}
	signature, err := base64.StdEncoding.Strict().DecodeString(p.Signature)
	if err != nil || base64.StdEncoding.EncodeToString(signature) != p.Signature || len(signature) != ed25519.SignatureSize || !ed25519.Verify(key, message, signature) {
		return ErrDenied
	}
	return nil
}
func (e BootEnrollment) Key() (ed25519.PublicKey, error) {
	if !opaqueID.MatchString(e.InstallationID) || !opaqueID.MatchString(e.BootID) {
		return nil, ErrDenied
	}
	key, err := base64.StdEncoding.Strict().DecodeString(e.PublicKey)
	if err != nil || len(key) != ed25519.PublicKeySize || base64.StdEncoding.EncodeToString(key) != e.PublicKey {
		return nil, ErrDenied
	}
	return ed25519.PublicKey(key), nil
}
